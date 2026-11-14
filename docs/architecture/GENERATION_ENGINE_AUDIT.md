# Generation Engine Audit

## Scope and baseline

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

### P1 — Provider artifact repository can return bytes from a different generation

`provider/internal/adapters/filesystem/artifacts.go` calls `Catalog`, then
opens an artifact after the catalog's reader lock has been released. A writer
can publish between those operations. This path does not hold a read lock
through open/read or re-check the returned bytes against the catalog hash.
`internal/adapters/generation.ReadArtifact` provides a safer locked and
rehashed pattern.

**Impact:** a concurrent caller can receive bytes that do not match the
metadata it just cataloged.

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
multisite site isolation. The audit's focused generation/safefs/protocol test
command passed. Missing proof includes crash-point recovery, a consistent
read through Provider `ArtifactRepository.Open`, transaction semantics for
standalone generators, and race-resistant filesystem operations.

The full-generation tests do not establish device syntax, delivery, execution,
or hardware compatibility.

## Recommended implementation order

1. Define a durable, versioned publication journal and a bounded recovery
   procedure; retain fail-closed behavior for malformed or untrusted recovery
   data. Test process-interruption points, not only returned errors.
2. Make Provider artifact reads hold a coherent snapshot or use the shared
   locked and rehashing reader path.
3. Decide and document the contract for standalone and declarative generators;
   make their publication/catalog semantics explicit and consistent.
4. Add crash, cancellation, concurrent read/write, and preserved-user-file
   regression coverage.
5. Treat race-resistant path operations as a separate security improvement
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
