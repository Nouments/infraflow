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

The same API exposes a separate admin-only execution lifecycle at
`/api/v1/executions`. It uses an idempotency key, persists task attempts and
results, and is connected to the existing dependency scheduler. The default
Provider registers no operational executor, so task types are rejected until
an explicitly authorized executor is added. Execution is never inferred from
planning or artifact generation; the dashboard and TUI do not yet display
execution records.

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

## GNS3 Docker deployment

Build both service images from the repository root:

```sh
docker build -f docker/infraflow-provider/Dockerfile -t infraflow-provider:gns3 .
docker build -f docker/infraflow-agent/Dockerfile -t infraflow-agent:gns3 .
```

The images use the existing strict YAML files and override their runtime values
through environment variables. Their defaults bind the provider to
`0.0.0.0:8443` and `0.0.0.0:8080`; the agent connects to
`192.168.0.3:8443` and `https://192.168.0.3:8080`. Configure the GNS3 node
interfaces on the same isolated Ethernet segment as `192.168.0.2/24` (agent)
and `192.168.0.3/24` (provider). Docker does not assign these addresses from
the InfraFlow environment variables. Verify them inside each node with
`ip -brief address` before starting the services. No host port publishing is
needed when the nodes share the GNS3 segment.

Create the lab server certificate outside the repository and outside the
Docker build context. This self-signed certificate is for an isolated lab; use
a certificate issued by your deployment CA outside the lab:

```sh
install -d -m 0700 "$HOME/.config/infraflow/gns3"
openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 365 \
  -keyout "$HOME/.config/infraflow/gns3/provider.key" \
  -out "$HOME/.config/infraflow/gns3/provider.crt" \
  -subj "/CN=192.168.0.3" \
  -addext "subjectAltName=IP:192.168.0.3" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,digitalSignature,keyCertSign" \
  -addext "extendedKeyUsage=serverAuth"
cp "$HOME/.config/infraflow/gns3/provider.crt" \
  "$HOME/.config/infraflow/gns3/provider-ca.pem"
chmod 0600 "$HOME/.config/infraflow/gns3/provider.key"
```

In the GNS3 Docker node settings, mount the provider certificate and private
key read-only at `/run/secrets/provider.crt` and
`/run/secrets/provider.key`; mount only `provider-ca.pem` read-only in the
agent at `/run/secrets/provider-ca.pem`. Persist `/var/lib/infraflow-provider`
and `/var/lib/infraflow-agent` as node volumes. Inject the same
`INFRAFLOW_AGENT_TOKEN` into both nodes using GNS3's environment settings or a
secret manager; it must contain at least 32 bytes. Do not put the token or
private key in YAML, source control, image build arguments, or the image.

Start the provider first, then the agent. The provider console must report its
TLS gRPC and HTTPS listeners; the agent must report successful registration
and artifact-run completion. A TLS, connection, or authentication failure is
reported by the respective entrypoint or service. The Docker images expose
ports 8443 and 8080; allow those ports between the two GNS3 nodes.

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
