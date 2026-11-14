# Generation Engine Audit

## M1 follow-up status — `develop` HEAD `23f3325ad24651d506f96d962f1b5de25c03ebbc`

The branch was fetched and verified against `origin/develop`; HEAD matches the
supplied starting reference, with no newer remote commits. The initial
worktree was clean. M1 changes are local and uncommitted.

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

### M1 evidence

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
  on the starting commit `23f3325` passed every Linux and Windows step,
  including checkout, full tests, build, vet, formatting, and diff checks.
  This run predates the M1 working-tree changes; no CI result exists for them.

Remaining M1 scope includes the separate transactional-publication,
crash-recovery, standalone-generator, and declarative artifact-contract
findings. M2 and later milestones have not started.

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

### P1 — Provider artifact repository could return bytes from a different generation — fixed locally

`provider/internal/adapters/filesystem/artifacts.go` calls `Catalog`, then
opened an artifact after the catalog's reader lock had been released. A writer
could publish between those operations, and the opened bytes were not checked
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
