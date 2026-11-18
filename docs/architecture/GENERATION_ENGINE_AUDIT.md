# Generation Engine Audit

## Current review — 2026-11-18

The current local revision is based on `develop` HEAD
`9707df0f009662c2dbd39d63fc138389bb884675`. The Agent-only native artifact
staging fix does not change Provider generation or publication code. The full
and race Go suites passed locally. A real Provider–Agent run in local GNS3
confirmed transfer and byte equality for the rendered Cisco IOS-XE profile
artifacts; no vendor parser/device validation or application occurred.

Source review confirmed that `GenerateAllContext` already stages site outputs,
validates hashes and catalogs, preserves uncatalogued files, and publishes
through the bounded recovery journal. It also confirmed that
`generate-device-config` used a separate backup/replace path and would replace
an arbitrary regular file at a generated path.

Declarative publication now verifies the previous generation report and every
prior configuration hash before treating those files as managed. Unmanaged
files and edited prior configurations fail closed. Verified regeneration and
stale-output removal use the shared journaled publisher. Regression tests cover
those cases. The independent source review found no blocking issue; it notes
that the report is unsigned ownership metadata, so the output root is trusted.

After implementation and dependency updates, the full and race Go suites,
generation tests, the Provider–Agent multisite loopback integration, and the
fresh-output demo/verifier passed. Linux and Windows Actions did not run on this
uncommitted revision. Baseline Actions
[run 44249869562](https://github.com/Nouments/infraflow/actions/runs/44249869562)
failed on Linux at `govulncheck` before tests; Windows tests and static checks
passed, as did the separate demo and Provider–Agent integration job. Those
results apply to the baseline commit. Exact commands and outcomes are in the
[completion report](../COMPLETION_AND_TEST_REPORT.md).

Remaining generation limits are unchanged: report ownership is unsigned,
filesystem-wide atomic directory replacement and parent-directory durability
are not guaranteed, and hostile concurrent filesystem path replacement is
outside the current protections. Native vendor output remains render-only.

## Historical mission 85+ local follow-up — `develop` baseline `bbffd4153857547204a457fa0da2c5f46e542d06`

The starting worktree was clean and `develop` matched `origin/develop` at
`stabilization6`. CI
[run 43634679961](https://github.com/Nouments/infraflow/actions/runs/43634679961)
passed Linux tests/race/vet/build/format/diff and Windows tests/vet/build/
format/diff on that SHA. No CI run covers the current local changes.

The full-generation publisher now writes a strict, bounded journal and
SHA-256-verified backups before changing published files. The journal stores
the original/new hashes, original modes, backup location, and directories
created for publication. Reads fail closed while a journal or backup state is
present. Under the next writer lock, an uncommitted transaction is restored
and a committed transaction is cleaned. Publication-phase failures roll back
using this same journal; a simulated abrupt interruption after an artifact
write is covered. Tests also cover unsafe recovery metadata and committed
cleanup after backup removal.

Recovery is not a global atomic directory swap. File contents are synced
before rename, but parent-directory sync and power-loss durability are not
guaranteed across filesystems. A stale writer lock is not auto-removed; an
operator must verify the owner is no longer active and remove only the lock
file before retrying. Reader markers are also persistent and writers fail
closed while any are present. After stopping all processes that may read the
root and confirming no reader is active, an operator may remove only the
`.infraflow-generation-reader-*.lock` files. Invalid journal or backup
metadata remains fail-closed. An initial journal-write failure removes its
unused backup directory rather than leaving an orphan that blocks reads and
future recovery.
Standalone and declarative generation remain outside the shared full-generation
transaction/catalog contract.

The new Bash launcher/Python verifier and Linux CI job are configured to check
fresh-output generation, manifests, paths, sizes, hashes, bootstrap
opt-in/opt-out, native rendered content, unsupported OSPF, and the real Agent
multisite integration test. The local demo and new job were not executed; no
success is claimed.
The local full test suite failed under Windows Application Control and
`CGO_ENABLED=0` SQLite limitations. The latest generation-package test
invocation was blocked by Application Control; earlier focused runs passed.
Race tests are not available locally with `CGO_ENABLED=0`. Exact chronology is
in the [completion report](../COMPLETION_AND_TEST_REPORT.md).

## Historical stabilization5 follow-up — `develop` HEAD `093ed33944fc3f35ac477079cd6b0cc63f575f01`

`develop` and `origin/develop` were fetched and confirmed at this starting
commit; no newer remote commit was found. The initial worktree was clean.
Latest CI [run 43632705446](https://github.com/Nouments/infraflow/actions/runs/43632705446)
passed on this exact commit: Linux full tests/race/vet/build/format/diff and
Windows full tests/vet/build/format/diff. Windows CI does not run race tests.

`ArtifactRepository.Open` now delegates to `generation.ReadArtifact`, which
holds the shared reader marker through catalog validation, file reading, and
hash verification. It returns a bounded in-memory snapshot (`MaxArtifactBytes`
is 32 MiB), so metadata and bytes belong to one generation even if publication
finishes while the caller consumes the returned reader. This uses memory
proportional to artifact size. A streaming descriptor was tested but rejected:
Windows denied atomic replacement while the file remained open.

Concurrent testing also exposed a narrow catalog-enumeration race: `ReadDir`
could return a transient reader-lock filename which disappeared before
`Lstat`. `catalogArtifacts` now skips the reserved generation-lock filenames;
read/write exclusion remains enforced by lock acquisition. The repeated
concurrent-reader test passed ten runs.

Windows stress testing also found that inspection of a writer lock during
creation can temporarily return access denied. Read-lock acquisition now
classifies that as generation-in-progress and releases its reader marker.

The current local follow-up also reproduced `inspectManagedOutput` failing on
Windows when a reader marker disappeared between `ReadDir` and `Lstat`.
Publication inspection now skips the reserved generation lock and reader
marker names. The existing concurrent reader/generator regression passed ten
repetitions after this fix. The fix is uncommitted and has no post-change CI.

### Historical M1 evidence — stabilization4 (`23f3325`)

- `go test -count=1 ./provider/internal/adapters/filesystem ./provider/internal/adapters/executors ./provider/internal/delivery/grpcapi ./internal/adapters/generation` — Earlier run: PASS. Latest revalidation had a transient timeout in unrelated `TestSSHSessionOutputLimitFailsAndCloses`; isolated rerun passed, and filesystem/generation passed on rerun.
- `go test -count=10 -run '^TestArtifactRepositoryOpen' ./provider/internal/adapters/filesystem` — PASS.
- `go test -run '^$' -bench '^BenchmarkArtifactRepositoryOpen$' -benchmem ./provider/internal/adapters/filesystem` — PASS; 12,853,962 ns/op, 478,601 B/op, 533 allocs/op on Windows/amd64, Intel Core i5-6500T.
- `go test -count=1 ./...` — Latest run FAIL: Windows Application Control
  blocked the temporary Provider executable in the multisite Agent integration;
  `CGO_ENABLED=0` prevents SQLite-dependent tests. Changed filesystem and
  generation packages passed; no full-suite pass is claimed.
- `go test -race -count=1 ./provider/internal/adapters/filesystem ./internal/adapters/generation` — NOT RUN: Go reported that `-race` requires CGO; local `CGO_ENABLED=0`.
- `go vet ./...`, `go build ./provider/cmd ./agent/cmd`, and
  `git diff --check` — PASS locally.
- GitHub Actions [run 43624108997](https://github.com/Nouments/infraflow/actions/runs/43624108997)
  on `23f3325` passed Linux and Windows steps. This evidence predates the later
  `stabilization5` commit and is retained only as historical context.

The former M1 status and evidence above are historical; the read-consistency
fix was included in `stabilization5`. Remaining findings include
transactional-publication crash recovery, standalone-generator consistency,
and the declarative artifact contract. The current evidence and the later
lock-marker race fix are recorded in the completion report and the
presentation-readiness section above.

## Historical audit scope and baseline

Reviewed the Provider configuration, generation, publication, catalog,
filesystem safety, and shared protocol implementation at `develop`
`9f60e0e51104e59875a5156bc13c9ac31c388baf`. The initial worktree was clean.
The focused command
`go test ./internal/adapters/generation ./internal/infrastructure/safefs ./pkg/protocol`
passed during the audit. This does not cover every failure mode described
below.

## Implemented behavior

### Input and planning

- Generic YAML and versioned `NetworkDeployment` are strict, bounded inputs.
- Unsupported intent such as OSPF is rejected rather than silently omitted.
- Planning is descriptive and does not apply infrastructure changes.

### Full generation

`GenerateAllContext` stages per-site inventory, topology, native device
outputs, data-only Terraform, and explicitly opted-in bootstrap artifacts.
Site workers are bounded. The full publisher validates the existing catalogs,
uses their manifests as the ownership source, removes only obsolete managed
artifacts, and preserves uncataloged user files. It publishes global and site
reports/manifests with sizes and SHA-256 metadata.

Writers targeting the same root use an exclusive lock. `Catalog` and
`ReadArtifact` coordinate through reader markers; `ReadArtifact` checks the
returned content hash. Internal staging renderers avoid recursively acquiring
the final output-root lock.

### Other generation entry points

Standalone `Generate`, `GenerateNative`, `GenerateTerraform`, and
`GenerateBootstrap` serialize their writes and atomically replace individual
files. They do not use the full-generation snapshot/rollback and stale-file
publication lifecycle. The declarative `generate-device-config` path has a
separate publication/report implementation.

## Findings, prioritized

### P1 — Interrupted full publication is fail-closed but not recoverable

The publisher invalidates prior catalog entry points, snapshots affected
managed files, publishes the new files, and cleans the backup after success.
If the process or machine stops between these operations, a recovery directory
and partially published output can remain. Catalog rejects the recovery
directory, and a later generation does not automatically restore or complete
the transaction. The current regression test verifies safe rejection of an
interrupted recovery directory, not recovery to the previous valid generation.

**Impact:** operator intervention may be required after a crash; output can be
unavailable until recovered. This is not a global atomic directory swap.

### P1 — Provider artifact repository could return bytes from a different generation — fixed

Historical source at the audit baseline called `Catalog`, then opened an
artifact after the catalog's reader lock had been released. A writer could
publish between those operations, and the opened bytes were not checked
against the catalog hash. `ArtifactRepository.Open` now uses
`generation.ReadArtifact` to look up metadata and read/hash the target under
one reader lock. The returned reader wraps that verified byte snapshot.

**Regression coverage:** tests verify hash/metadata consistency before and
after a changed generation, safe rejection while a writer marker is present,
missing artifacts after removal, concurrent readers, and concurrent readers
with generation. The tests use start barriers rather than sleeps.

### P1 — Concurrent readers could race catalog lock-file enumeration — fixed locally

The catalog scanner enumerated every output-root entry and then `Lstat`ed it.
Another reader could remove its marker between those operations, causing a
spurious catalog error. The scanner now ignores reserved lock filenames;
`acquireGenerationReadLock` still rejects an active writer, and writers still
check active reader markers. The eight-reader test passed ten repeated runs.

### P1 — Standalone generators are serialized but not transactionally published

The public writer lock prevents concurrent InfraFlow writers from interleaving,
but a later per-file write failure can leave earlier files updated. These
entry points do not share the full generation rollback/stale-artifact handling
and do not add their output to the full shared catalog in the same way.

**Impact:** callers must not assume full-generation consistency or
catalog-discoverability for standalone output.

### P2 — Declarative output does not conform to the shared catalog contract

The declarative generator returns a `device_configuration` artifact under
`<site>/<device>/config.cfg`, while the shared protocol path/type validation
does not define that combination. The generator also does not publish a site
catalog manifest. The common catalog therefore cannot discover or validate
those returned artifacts as normal catalog entries.

**Impact:** declarative output has a distinct lifecycle and should either
receive an explicit separate contract or be aligned with the shared catalog.

### P2 — Path defenses are check-then-use

`safefs` rejects traversal and detected symlinks, but uses path-based `Lstat`
checks before later `MkdirAll`, temp-file creation, rename, or open operations.
A concurrent local actor able to mutate the output tree may change a checked
component between validation and use. Existing tests establish rejection of
static symlink/traversal cases, not hostile concurrent filesystem races.

**Impact:** document the trusted output-root assumption; use directory-relative
no-follow operations if hostile local mutation must be in scope.

## Test coverage observed

Existing generation tests exercise successful generation, stale managed-file
cleanup, previous-manifest validation, rollback on returned publication
errors, hashes, idempotency, symlink/traversal rejection, locking, and
multisite site isolation. The M1 focused tests listed above passed. Missing
proof includes crash-point recovery, transaction semantics for standalone
generators, and race-resistant filesystem operations. Local race testing was
not available because CGO is disabled.

The full-generation tests do not establish device syntax, delivery, execution,
or hardware compatibility.

## Recommended implementation order

1. Define a durable, versioned publication journal and a bounded recovery
   procedure; retain fail-closed behavior for malformed or untrusted recovery
   data. Test process-interruption points, not only returned errors.
2. Decide and document the contract for standalone and declarative generators;
   make their publication/catalog semantics explicit and consistent.
3. Add crash, cancellation, concurrent read/write, and preserved-user-file
   regression coverage.
4. Treat race-resistant path operations as a separate security improvement
   unless output roots can be writable by an adversarial local actor.

No new vendor capability, deployment behavior, or generation architecture is
introduced by this audit.

## Primary source files

- [Full generation orchestration](../../internal/adapters/generation/generator.go)
- [Publication and rollback](../../internal/adapters/generation/publication.go)
- [Catalog and artifact reads](../../internal/adapters/generation/catalog.go)
- [Shared artifact path/type contract](../../pkg/protocol/artifact.go)
- [Declarative configuration renderer](../../internal/adapters/generation/declarative.go)
- [Provider artifact repository](../../provider/internal/adapters/filesystem/artifacts.go)
- [Filesystem safety helpers](../../internal/infrastructure/safefs/atomic.go)
