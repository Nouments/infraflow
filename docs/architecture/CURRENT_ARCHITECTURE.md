# Current Architecture Snapshot

## Audit baseline

- Repository: `Nouments/infraflow`
- Branch: `develop`
- Inspected HEAD: `093ed33944fc3f35ac477079cd6b0cc63f575f01` (`stabilization5`)
- The branch was fetched; `develop` matched `origin/develop` and the supplied
  starting reference. No newer remote commit was found; the initial worktree
  was clean.
- Evidence: checked-in source/tests and GitHub Actions run
  [43632705446](https://github.com/Nouments/infraflow/actions/runs/43632705446).
  Linux passed full and race tests, vet, builds, gofmt, and diff checks;
  Windows passed full tests, vet, builds, gofmt, and diff checks. Windows
  does not run race tests. A follow-up local race fix is not included in CI.

This is a current-state snapshot, not an architecture proposal. Statements
about device compatibility, deployment, and verification are limited to
observed evidence.

## System boundaries

```text
Provider CLI / HTTP
  YAML -> validation -> planning -> generation -> manifests/catalog
  authenticated HTTP and gRPC -> user, execution, and Agent operations

Agent
  authenticated gRPC -> catalog/download -> hash validation -> local staging
  supported processors -> report submission
```

Provider and Agent are separate Go services. Their shared protocol is versioned
in `api/proto/infraflow/v1` and `pkg/protocol`; Agent does not import Provider
packages. The Provider uses adapters for configuration, planning, generation,
filesystem access, authentication, and SQLite-backed user/session storage.

The Agent's ordinary run downloads and verifies catalog artifacts and invokes
its generic processor. It is not a general command runner and does not apply
network configurations. Its `validate-terraform` operation is separate and
explicit. The TUI manages/inspects remote executions; it is not a local
Developer Agent launcher.

## Provider capabilities observed

- Strict, size-limited YAML loading and semantic network validation.
- Deterministic plan generation.
- `GenerateAllContext` stages site results, publishes cataloged artifacts,
  manifests, and reports; bootstrap files are emitted only for explicitly
  enabled services.
- Shared catalog validation checks manifest metadata, allowed paths/types,
  file sizes, and SHA-256 values. `ReadArtifact` rechecks bytes while holding
  the generation reader lock.
- Provider `ArtifactRepository.Open` now obtains metadata and content through
  `ReadArtifact`, returning a verified in-memory snapshot rather than opening
  the path after cataloging. The snapshot is bounded by the 32 MiB artifact
  limit; simultaneous opens can retain one snapshot each until callers close
  or release their readers. A streaming descriptor was rejected after
  Windows testing showed an open file can block atomic replacement.
- Authenticated HTTP and gRPC APIs; Agent catalog access can be site-scoped.
- Persistent execution lifecycle with registered typed executors:
  `artifact.verify` and `cisco.ios.configure`. Cisco configuration uses SSH
  with typed operations and state read-back. The API does not expose a generic
  shell executor.
- The CLI `apply` entry point creates a planning job; it is not the executor.
  Execution is a separate, admin-authorized API operation restricted to
  registered task types.

These are code-level capabilities, not proof of live network operation.
Provider tests use mocks for Cisco executor interactions; no authorized
hardware or lab validation was observed.

## Generation and artifact lifecycle

The full generation path stages artifacts before publication, uses an
exclusive output-root writer lock, coordinates `Catalog` and `ReadArtifact`
with reader markers, and uses snapshots/rollback to preserve the previous
cataloged state on handled publication errors. It derives managed-file
ownership from validated previous manifests and does not recursively delete
the output directory. Concurrent catalog scans now ignore reserved lock
filenames, avoiding a race when another reader removes a marker between
`ReadDir` and `Lstat`. Full-generation inspection now also skips those
transient lock names; otherwise a Windows `Lstat` could fail after a reader
removed its marker. This follow-up fix is local and not covered by the cited
CI. External processes that ignore InfraFlow locks are not coordinated.

This is rollback-capable publication, not a filesystem-wide atomic swap. A
crash can leave a recovery directory and partial files; catalog access fails
closed and there is no automatic recovery procedure. Standalone generators
have per-file atomic writes and writer locking, but do not share the full
generation transaction and stale-file/catalog lifecycle. The declarative
device-config generator has a separate report/rollback path and does not
publish artifacts through the shared site catalog contract.

Filesystem safety rejects traversal and statically detected symlinks, but
path-based check-then-use operations are not race-proof against another local
actor that can concurrently modify the output tree.

Artifact reads materialize a snapshot up to the 32 MiB protocol limit. There
is no global limit on simultaneous readers; concurrent large reads can
therefore consume substantial memory.

## Execution and device capability limits

- Native Cisco `.cfg`, MikroTik RouterOS `.auto.rsc`, and FortiOS `.cfg`
  outputs are generated text.
- Cisco `.cfg` generation is not Cisco ZTP or AutoInstall.
- MikroTik `.auto.rsc` has not been proven on CHR or hardware.
- FortiOS `.cfg` is render-only (the current native renderer uses `.cfg`, not
  `.conf`).
- No vendor is hardware-validated based on this repository's current evidence.
- Agent processing/staging does not upload or execute generated configs.
- Planning, generation, delivery, execution, and verification are distinct
  lifecycle stages.
- Bootstrap data generation does not start DHCP/TFTP/HTTP services.
- Terraform validation invokes an external Terraform binary, but no Terraform
  apply is part of ordinary Agent processing.

## Coding-agent status

There is no integrated Developer Agent, independent Reviewer Agent, or task
Orchestrator in the application. `.github/agents/*.agent.md` files provide
repository-specific Copilot guidance; they are not InfraFlow runtime agents.
No model runtime/provider or OS sandbox was found in the application
architecture. The Terraform toolrunner uses a temporary working directory,
isolated `HOME`, command timeout, and input restrictions, but runs as the
Agent's OS identity, inherits `PATH`, and is not an OS-level sandbox.

Agent reports may describe artifacts as `completed` when they have only been
validated or staged; they must not be interpreted as device application or
service startup.

## Test and CI evidence

On the inspected HEAD, Linux and Windows CI passed the steps listed above.
After the uncommitted inspection-race fix, focused config, generation,
protocol, and filesystem tests passed; the targeted reader/generator
concurrency regression passed ten runs. Local `go test -count=1 ./...` failed
because `CGO_ENABLED=0` prevents SQLite-dependent tests and the temporary
Provider process from starting; a concurrent lock-marker race in that run was
then corrected and stress-tested. Local race tests cannot run without CGO.
Application Control blocked a targeted CLI test binary and `go run`; PowerShell
refused to load the demo script under the active policy. Exact results are in
[`../COMPLETION_AND_TEST_REPORT.md`](../COMPLETION_AND_TEST_REPORT.md).

## Architecture sources

- [ARCHITECTURE.md](../../ARCHITECTURE.md)
- [INFRAFLOW_SPEC.md](../../INFRAFLOW_SPEC.md)
- [Provider CLI](../../provider/internal/delivery/cli/cli.go)
- [Agent runner](../../agent/internal/application/runner.go)
- [Agent generic processor](../../agent/internal/adapters/processor/generic.go)
- [Provider execution API](../../provider/internal/delivery/httpapi/jobs.go)
- [CI workflow](../../.github/workflows/go-ci.yml)
