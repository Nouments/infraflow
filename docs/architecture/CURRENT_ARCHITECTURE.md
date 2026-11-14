# Current Architecture Snapshot

## Audit baseline

- Repository: `Nouments/infraflow`
- Branch: `develop`
- Inspected HEAD: `9f60e0e51104e59875a5156bc13c9ac31c388baf` (`stabilization3`)
- Initial worktree: clean
- Evidence: checked-in source/tests and GitHub Actions run
  [43622305020](https://github.com/Nouments/infraflow/actions/runs/43622305020).
  Linux passed the defined checks on this commit. Windows stopped before
  checkout at a pre-checkout Git command; it did not test the code.

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
the output directory. External processes that ignore InfraFlow locks are not
coordinated.

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

## Execution and device capability limits

- Native Cisco `.cfg`, MikroTik RouterOS `.auto.rsc`, and FortiOS `.conf`
  outputs are generated text.
- Cisco `.cfg` generation is not Cisco ZTP or AutoInstall.
- MikroTik `.auto.rsc` has not been proven on CHR or hardware.
- FortiOS `.conf` is render-only.
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

On the inspected HEAD, Linux CI passed full tests, race tests, vet, command
builds, gofmt, and diff checks. Windows CI failed before checkout because
`git config core.autocrlf false` was run without a repository; no Windows test
result exists for that run. A local `go test -count=1 ./...` on Windows failed
because Application Control blocked multiple test binaries and the local
`CGO_ENABLED=0` SQLite stub cannot run. Focused generation, safe-filesystem,
and protocol tests passed locally. Exact results are in
[`../COMPLETION_AND_TEST_REPORT.md`](../COMPLETION_AND_TEST_REPORT.md).

## Architecture sources

- [ARCHITECTURE.md](../../ARCHITECTURE.md)
- [INFRAFLOW_SPEC.md](../../INFRAFLOW_SPEC.md)
- [Provider CLI](../../provider/internal/delivery/cli/cli.go)
- [Agent runner](../../agent/internal/application/runner.go)
- [Agent generic processor](../../agent/internal/adapters/processor/generic.go)
- [Provider execution API](../../provider/internal/delivery/httpapi/jobs.go)
- [CI workflow](../../.github/workflows/go-ci.yml)
