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
go run ./provider/cmd validate -f examples/network-deployment.yaml
```

`validate` is read-only. `plan` only describes deterministic work. `generate-all` creates and publishes the complete agent catalog (inventory, topology, Ansible, Terraform, DHCP/DNS/TFTP/PXE/iPXE) with one combined manifest; it does not contact infrastructure or apply a plan.

### Reproducible declarative device-configuration demo

The versioned `NetworkDeployment` schema is supported by the existing
`validate` command and the `generate-device-config` command. Run these commands
from the repository root. Outputs go to a fresh temporary directory rather
than into the checkout:

```sh
demo_dir="$(mktemp -d "${TMPDIR:-/tmp}/infraflow-device-demo.XXXXXX")"
go run ./provider/cmd validate -f examples/network-deployment.yaml
go run ./provider/cmd plan -f examples/network-deployment.yaml
go run ./provider/cmd generate-device-config \
  -f examples/network-deployment.yaml -out "$demo_dir/generated"
cat "$demo_dir/generated/demo/R1/config.cfg"
cat "$demo_dir/generated/demo/generation-report.json"
sha256sum "$demo_dir/generated/demo/R1/config.cfg" \
  "$demo_dir/generated/demo/generation-report.json"
```

Expected validation output is `Valid: 1 site(s), 1 device(s); no
infrastructure changes made.` The plan is `PLANNED`; its `generated`,
`executed`, and `verified` lifecycle fields remain false. Generation reports
`rendered_unverified`, prints each output SHA-256, and writes the config plus a
JSON report. The generated `config_hash` must match the first `sha256sum`
result; the reported SHA-256 for `generation-report.json` must match the
second. These checks establish file integrity only.

The example uses the documentation-only `192.0.2.0/24` range; it is not a
device address or a claim of lab validation. The embedded catalogue contains a
partial Cisco ISR 1100 record with an official product-family reference, but
does not claim verified SKU, interface, or IOS XE version support. The
`infraflow.cisco.iosxe.isr-1100.interfaces.v1` template only renders static
IPv4 interface addressing and administrative state. The report includes the
candidate score (93/100), its criterion-by-criterion rationale, resolved
variables, template hash, and explicit hardware-validation limits. Generated
configuration is render-only; it is not applied to equipment.
The report labels document validation as `passed`, declaration-to-template
compatibility as render-only, command syntax validation as `not_performed`,
and physical-device validation as `not_performed`. No IOS parser or device
connection is used by this demo.

The deterministic score is: exact manufacturer/model 30 points; OS family
20 points plus 5 only for an explicitly supported OS version; requested
feature coverage 25; required variables 10; valid template metadata and
render-only status 8 of 10. A score is calculated only after mandatory
catalog, identity, OS-family, feature, variable, and template checks pass;
points cannot compensate for an incompatibility. Because this example omits
an OS version, its score is 93/100. The score describes declaration-to-template
matching only, not physical-device compatibility.

Planning, generation, execution, and verification are separate lifecycle
stages. `plan` describes intended work without applying it. The
`generate-device-config` command renders a local artifact and report only. This
demo does not execute the planned task, contact a device, or verify resulting
device state. The separate Provider execution API is an explicit action and
requires an authorized target; no execution command is included here.

To verify fail-closed behavior for an unsupported feature and the absence of
partial output, use a separate temporary destination:

```sh
if go run ./provider/cmd generate-device-config \
  -f examples/network-deployment-unsupported-ospf.yaml -out "$demo_dir/rejected"; then
  echo "ERROR: unsupported declaration unexpectedly succeeded" >&2
  exit 1
fi
test ! -e "$demo_dir/rejected/demo/R1/config.cfg"
test ! -e "$demo_dir/rejected/demo/generation-report.json"
```

The command must fail with the field path `spec.devices[0].routing.ospf`; the
two `test` commands must succeed because neither artifact is published.
OSPF, VLANs, static routes, other hardware models, and OS versions are not
implemented by this template. No datasheet or image is bundled: catalog
entries distinguish official URL references from files actually available
locally. See
[`internal/adapters/catalog/README.md`](internal/adapters/catalog/README.md).

Generic Ansible generation produces an inventory and `site.yml` that only prints declared device metadata. Its catalog type is `ansible_inspection_playbook`; it is not a vendor configuration or evidence of device execution. Vendor-specific playbooks are separate `ansible_playbook` artifacts named `vendor-playbook.yml`, marked `UNVERIFIED` and manual-only. They require an explicit `--check` preview or `INFRAFLOW_APPLY=true` for mutation and have not been validated on a lab device.

The provider can also persist validated planning jobs and expose them through its authenticated HTTP API when `api_listen_address` is set in the provider configuration. Plain HTTP is loopback-only; a remote API requires the configured TLS certificate/key pair. Job creation validates and plans only; it does not provision devices.

The same API exposes a separate admin-only execution lifecycle at
`/api/v1/executions`. It uses an idempotency key, persists task attempts and
results, and is connected to the existing dependency scheduler. The Provider
registers two operational task executors: `artifact.verify` for read-only
cryptographic artifact verification, and `cisco.ios.configure` for strictly
validated Cisco IOS hostname and Ethernet interface configuration over SSH with
running-config read-back verification. Unsupported task types are rejected. Execution
is never inferred from planning or artifact generation; execution records can be
inspected and cancelled by administrators via the web console and the Linux TUI.
Live Cisco/GNS3 validation remains **NON EXÉCUTÉ** until an authorized, reachable
IOS node and independently verified SSH host key are available. See the
[Cisco IOS GNS3 lab runbook](docs/lab/cisco-ios-gns3.md); its preflight checks
connectivity, pinned host-key fingerprint, and IOS version without configuration.

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
- [x] Admin execution lifecycle views with pagination, details, and audited cancellation in the web console and Linux TUI
- [ ] Device provisioning adapters, registered operational executors, and remaining bootstrap services

Device provisioning tasks are reported as blocked because no verified adapters are registered. The later items are intentionally not represented as supported capabilities yet. See [INFRAFLOW_SPEC.md](INFRAFLOW_SPEC.md) for the current phased roadmap and acceptance criteria.
