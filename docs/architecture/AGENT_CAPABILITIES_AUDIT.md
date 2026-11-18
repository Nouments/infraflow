# Agent Capabilities Audit

## Current status — 2026-11-18

The current local revision is based on `develop` at
`9707df0f009662c2dbd39d63fc138389bb884675`. This pass fixed the Agent's stored
artifact allowlist for verified `native_device_config` and `native_manifest`
files and added regression tests. The full and race Go suites passed, as did a
real Provider–Agent appliance synchronization run in local GNS3. No GitHub
Actions run covers the local revision. Baseline Actions
[run 44249869562](https://github.com/Nouments/infraflow/actions/runs/44249869562)
passed the Windows suite and the separate demo/integration job; Linux stopped
at `govulncheck` before tests. The GNS3 run tested appliance transport and
staging only. No device was contacted, and staged native configuration was not
applied. Baseline CI evidence is not presented as CI evidence for this
revision.

## Historical status — 2026-11-14

The current review baseline is `develop` HEAD
`093ed33944fc3f35ac477079cd6b0cc63f575f01`; the latest CI
[run 43632705446](https://github.com/Nouments/infraflow/actions/runs/43632705446)
passed Linux tests/race/vet/build/format/diff and Windows
tests/vet/build/format/diff on that commit. The audit below describes the
checked-in Agent boundaries, which remain unchanged by the local generation
lock-marker fix. That local fix has not been CI-verified.

## Scope and baseline

Reviewed the Agent runtime, CLI, generic processor, filesystem state handling,
Terraform toolrunner, TUI, Provider execution registration, and authorization
boundaries at the earlier `develop` HEAD
`9f60e0e51104e59875a5156bc13c9ac31c388baf`.

## What the Agent does today

The ordinary Agent run fetches a catalog and artifacts from the Provider,
validates catalog metadata and hashes, stores files locally, invokes supported
processors, saves a report, and submits it. The generic processor validates
inventory/topology and treats native, Terraform, and bootstrap artifacts as
staged/validated data; it does not apply native configurations or start
services. Unsupported artifact types are blocked.

The separate `validate-terraform` command stages a constrained file set and
runs `terraform init` and `terraform validate`; it does not run `terraform
apply`. The toolrunner has bounded command execution, a temporary working
directory, isolated `HOME`, and input restrictions intended to reject
provisioning or external execution blocks. It invokes the Terraform executable
found through inherited `PATH` as the Agent's OS identity. No container, VM,
restricted token, namespace, or equivalent OS sandbox was found.

The Agent TUI is a management and inspection client for existing remote
executions. It is not an Agent process supervisor or a coding interface.
Local tool inspection reports whether Terraform is on `PATH`.

## Provider execution boundary

The Provider registers typed `artifact.verify` and
`cisco.ios.configure` executors. Cisco tasks resolve an authorized target,
connect over SSH, apply typed commands, and read back state. The API accepts
only registered task types and is admin-gated; it is not a general shell
endpoint. Cisco executor tests use mock sessions and do not prove operation
against physical or virtual IOS hardware.

Agent credentials can be site-scoped for catalog, download, report, and log
access. Admin authorization is required to submit Provider execution tasks.
These controls do not establish device compatibility or grant the ordinary
Agent a device-configuration capability.

## Status semantics and risks

- A generic artifact report can say `completed` when the work means that files
  were validated or staged. Consumers must not infer device mutation or
  service startup from that status.
- Filesystem writes verify hashes and reject traversal/static symlinks, but
  checks are path-based and not race-proof against a hostile concurrent local
  actor.
- Artifact files use ordinary readable modes; private report/log data has
  separate protection. Do not treat the Agent state directory as a security
  boundary against another process with the same OS identity.
- Terraform's temporary directory and timeout are containment conveniences,
  not process isolation. The external binary and host account remain trusted.
- No automatic Cisco ZTP, Cisco AutoInstall, Fortinet ZTP, MikroTik API,
  FortiOS API, device upload, or Netinstall path is provided by the Agent.

## Coding-agent/orchestration capability

No in-product Developer Agent, independent Reviewer Agent, or task Orchestrator
was found. `.github/agents/infraflow-engineer.agent.md` and
`.github/agents/infraflow-generation-sprint.agent.md` are repository
customization/prompt files, not runtime worker implementations. There is no
integrated model provider/runtime, isolated coding workspace, review
independence mechanism, persisted coding-task state machine, or approval-gated
patch workflow. Do not describe the existing Provider scheduler as that
system: it schedules typed InfraFlow executions, not model-driven code tasks.

## Evidence from tests and CI

Tests cover Agent transfer/hash validation, site scoping, generic processing,
Terraform input rejection, path/symlink handling, and mocked Cisco execution.
The focused generation, safefs, and protocol test command passed locally; the
full Windows suite was blocked in part by Application Control and disabled
CGO. Linux CI passed the full test and race suites on the inspected HEAD.
Windows CI on that HEAD failed before checkout, so no Windows CI test evidence
is available. See the
[completion report](../COMPLETION_AND_TEST_REPORT.md) for exact evidence.

## Capability decision

Current product roles are Provider and artifact-processing Agent. A safe
Developer/Reviewer/Orchestrator system is a future capability and requires,
at minimum, an explicit model/runtime adapter, an OS-enforced isolated
workspace and command boundary, durable task state, independently enforced
review, cancellation, resource limits, and human approval before any
externalized change. Prompt files and temporary directories alone are not
adequate isolation.

## Primary source files

- [Agent runtime runner](../../agent/internal/application/runner.go)
- [Generic artifact processor](../../agent/internal/adapters/processor/generic.go)
- [Terraform toolrunner](../../agent/internal/application/toolrunner/runner.go)
- [Agent CLI](../../agent/internal/delivery/cli/cli.go)
- [Agent TUI](../../agent/internal/delivery/tui/model.go)
- [Provider execution API](../../provider/internal/delivery/httpapi/jobs.go)
- [Cisco IOS executor](../../provider/internal/adapters/executors/cisco_ios_configure.go)
- [Agent filesystem state](../../agent/internal/adapters/filesystem/state.go)
