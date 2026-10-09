# InfraFlow

InfraFlow is a declarative infrastructure orchestration project. Its design follows the repository roadmap in [INFRAFLOW_SPEC.md](INFRAFLOW_SPEC.md): a generic orchestration core, explicit adapter capabilities, and a strict separation between validation, planning, generation, and execution.

## Services

This monorepo contains two independently buildable services:

```text
provider/  user-facing validation, planning, generation, and authenticated gRPC service
agent/     independent gRPC client that downloads streams, processes supported artifacts, and reports state
```

The provider owns the desired infrastructure input and generated files. The agent receives only files published in the provider's hash-verified manifest. A generic scheduler core now validates dependency graphs and provides bounded execution primitives, but provider-specific provisioning remains unsupported until it has verified references, an isolated adapter, fixtures or a lab, automated tests, and observable results.

## Requirements

- Go 1.25 or newer

## Quick start

```sh
go test ./...
go run ./provider/cmd validate -f examples/infra.yaml
go run ./provider/cmd plan -f examples/infra.yaml
go run ./provider/cmd generate -f examples/infra.yaml -out ./provider-data
go run ./provider/cmd generate-ansible -f examples/infra.yaml -out ./ansible-output
go run ./provider/cmd generate-terraform -f examples/infra.yaml -out ./terraform-output
go run ./provider/cmd generate-bootstrap -f examples/infra.yaml -out ./bootstrap-output
go run ./provider/cmd generate-all -f examples/infra.yaml -out ./provider-data
```

`validate` is read-only. `plan` only describes deterministic work. `generate-all` creates and publishes the complete agent catalog (inventory, topology, Ansible, Terraform, DHCP/DNS/TFTP/PXE/iPXE) with one combined manifest; it does not contact infrastructure or apply a plan.

The provider can also persist validated planning jobs and expose them through its authenticated HTTP API when `api_listen_address` is set in the provider configuration. Plain HTTP is loopback-only; a remote API requires the configured TLS certificate/key pair. Job creation validates and plans only; it does not provision devices.

The provider HTTP API has two separate authentication domains. Agents continue
to use the configured machine bearer token. Platform users use SQLite-backed
accounts with bcrypt password hashes, expiring opaque sessions, and `admin` or
`user` roles. Administrators manage accounts under `/api/v1/users`; regular
users can create and inspect planning jobs but cannot manage agents, events, or
users. On first startup, the backend generates the administrator password and
writes a protected local shell retrieval script.

The Linux TUI connects directly to the provider API with an editable
`https://server:port` address. A provider web console is also available when
`web_ui_enabled: true` is configured with `api_listen_address`; it shares the
same SQLite-backed sessions and RBAC instead of creating separate credentials.

The agent has experimental, explicit-start DHCPv4, read-only TFTP, and bootstrap HTTP services. They bind to a selected interface, use provider-generated allowlists/configuration, and require an isolated provisioning network. DHCP leases remain in memory and firmware images are not bundled. DNS service and vendor-specific ZTP/AutoInstall are not implemented.

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
internal/ports/      Application-owned repository and service contracts
internal/adapters/   YAML, planning, generation, and shared adapters
internal/application/  Planning, scheduling, reconciliation, and use cases
internal/infrastructure/  Filesystem safety and security helpers
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
- [x] Generic Ansible inventory and inspection playbook generation (without execution)
- [x] Generic data-only Terraform configuration generation (without provider or apply)
- [x] Generic DHCP/DNS/TFTP/PXE/iPXE bootstrap artifact generation (without agent execution)
- [x] Provider SQLite user accounts, bcrypt passwords, expiring sessions, and admin/user authorization
- [x] Linux TUI client for authenticated planning jobs and agent state
- [x] Provider Fiber web console for planning jobs, agents, audit events, technical logs, and admin user access
- [x] Admin technical-log dashboard with query filters, cursor pagination, SSE resume/deduplication, and explicit stream errors
- [x] Agent TUI point-in-time technical log query (`l` / `logs`), restricted to administrators
- [x] Incremental bounded stdout/stderr capture and optional process log synchronization through the existing agent gRPC contract
- [x] Experimental agent DHCP/TFTP/bootstrap HTTP services and fake end-to-end artifact transfer/report test
- [ ] Device provisioning adapters, execution job API, and remaining bootstrap services

Device provisioning tasks are reported as blocked because no verified adapters are registered. The later items are intentionally not represented as supported capabilities yet. See [INFRAFLOW_SPEC.md](INFRAFLOW_SPEC.md) for the current phased roadmap and acceptance criteria.
