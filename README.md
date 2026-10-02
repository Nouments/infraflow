# InfraFlow

InfraFlow is a declarative infrastructure orchestration project. Its design follows `INFRAFLOW_SPEC.md`: generic orchestration core, explicit adapter capabilities, and a strict separation between validation, planning, generation, and execution.

## Services

This monorepo contains two independently buildable services:

```text
provider/  user-facing validation, planning, generation, and authenticated gRPC service
agent/     independent gRPC client that downloads streams, processes supported artifacts, and reports state
```

The provider owns the desired infrastructure input and generated files. The agent receives only files published in the provider's hash-verified manifest. A generic scheduler core now validates dependency graphs and provides bounded execution primitives, but provider-specific provisioning remains unsupported until it has verified references, an isolated adapter, fixtures or a lab, automated tests, and observable results.

## Requirements

- Go 1.23 or newer

## Quick start

```sh
go test ./...
go run ./provider/cmd validate -f examples/infra.yaml
go run ./provider/cmd plan -f examples/infra.yaml
go run ./provider/cmd generate -f examples/infra.yaml -out ./provider-data
```

`validate` is read-only. `plan` only describes deterministic work. `generate` writes local provider artifacts; it does not contact infrastructure.

The provider can also persist validated planning jobs and expose them through its authenticated loopback HTTP API when `api_listen_address` is set in the provider configuration. Job creation validates and plans only; it does not provision devices.

The agent does not currently run DHCP, DNS, TFTP, HTTP, or iPXE services. These are planned local bootstrap services, distinct from generating static configuration files, and will be added only with isolated adapters, fixtures, tests, and observable results.

To serve generated files to an agent, set a strong token through a secret manager or environment variable. The provider uses gRPC streaming; plaintext is restricted to loopback. Remote deployments require TLS.

```sh
export INFRAFLOW_AGENT_TOKEN='<secret of at least 32 bytes>'
go run ./provider/cmd serve -config examples/provider-config.yaml
```

On the agent host, provide the same token securely and configure the provider address/TLS in the agent YAML:

```sh
INFRAFLOW_AGENT_TOKEN='<same secret>' go run ./agent/cmd run \
  -config examples/agent-config.yaml
```

## Project layout

```text
provider/            User-facing provider service and executable
agent/               Agent config, application, adapters, delivery, and executable
pkg/protocol/        Shared artifact and state contract
api/proto/           Versioned gRPC protocol source
internal/domain/     Core infrastructure and plan types
internal/config/     YAML loading and validation
internal/planner/    Deterministic dependency planning
internal/reconcile/  Side-effect-free desired/observed drift comparison
internal/generator/  Deterministic artifact generation and catalog validation
examples/            Sample infrastructure declarations
ARCHITECTURE.md      Service boundaries and safety rules
```

## Development

```sh
make fmt
go vet ./...
go test -race ./...
make build-provider
make build-agent
```

Focused service checks are available as `make test-provider` and `make test-agent`.

## Implementation status

- [x] Go module and separated provider/agent service folders
- [x] README, architecture notes, and example input
- [x] Strict YAML parsing and semantic validation
- [x] Deterministic dependency plan generation
- [x] Deterministic inventory and topology artifacts with hash manifest
- [x] Provider gRPC artifact streaming/state API, planning-job HTTP API, authenticated agent registration/heartbeat, and independent configured agent client
- [x] Generic dependency-aware scheduler primitives (retry, timeout, cancellation, concurrency, and locks)
- [x] Side-effect-free desired/observed reconciliation with deterministic drift paths and state hashes
- [ ] Agent bootstrap services, device provisioning adapters, execution job API, and web UI

Device provisioning tasks are reported as blocked because no verified adapters are registered. The later items are intentionally not represented as supported capabilities yet. See `INFRAFLOW_SPEC.md` for the full phased roadmap and acceptance criteria.
