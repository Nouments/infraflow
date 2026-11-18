# InfraFlow

InfraFlow is a declarative infrastructure orchestration project. Its design follows the repository roadmap in [INFRAFLOW_SPEC.md](INFRAFLOW_SPEC.md): a generic orchestration core, explicit adapter capabilities, and a strict separation between validation, planning, generation, and execution.

## Services

This monorepo contains two independently buildable services:

```text
provider/  user-facing validation, planning, generation, and authenticated gRPC service
agent/     independent gRPC client that downloads streams, processes supported artifacts, and reports state
```

The provider owns the desired infrastructure input and generated files. The agent receives only files published in the provider's hash-verified manifest. A generic scheduler core validates dependency graphs and provides bounded execution primitives. Only explicitly registered task types are executable; vendor-specific ZTP/AutoInstall and unverified device workflows remain unsupported until their evidence requirements are met.

## Requirements

- Go 1.26 or newer

## Quick start

```sh
go test ./...
go run ./provider/cmd validate -f examples/infra.yaml
go run ./provider/cmd plan -f examples/infra.yaml
go run ./provider/cmd generate -f examples/infra.yaml -out ./provider-data
go run ./provider/cmd generate-terraform -f examples/infra.yaml -out ./terraform-output
go run ./provider/cmd generate-bootstrap -f examples/infra.yaml -out ./bootstrap-output
go run ./provider/cmd generate-all -f examples/infra.yaml -out ./provider-data
go run ./provider/cmd validate -f examples/network-deployment.yaml
```

### Current repository and local verification — 2026-11-18

The fetched `develop` and `origin/develop` baseline is
`9ec97bc8fd16f02b60f4e65eb0d6912b4e3fb444`; the worktree was clean before
this local follow-up. Declarative `generate-device-config` publication now
uses the shared recovery journal. It replaces prior outputs only after
verifying the previous report and configuration hashes, rejects unmanaged or
edited files, and removes stale outputs from a verified prior generation.

A reachable-vulnerability scan prompted upgrades to Brotli, `x/net`, gRPC,
and SSH dependencies. The module and Docker builder images now require Go
1.26. The final CI-pinned `govulncheck@v1.9.0` scan reported zero reachable
vulnerabilities.

After the code and dependency changes, the full Go suite, race suite, vet,
Provider/Agent builds, formatting check, and `git diff --check` passed. The
fresh-output generation demo and manifest/hash verifier also passed, as did
the real Provider–Agent multisite loopback integration test. No device was
contacted; native vendor output remains render-only. GitHub Actions run
[44249869562](https://github.com/Nouments/infraflow/actions/runs/44249869562)
on the fetched baseline failed in Linux at `govulncheck`, before Go tests; its
Windows full test/static checks and demo/integration jobs passed. No Linux or
Windows GitHub Actions run covers this uncommitted revision. See the
[completion report](docs/COMPLETION_AND_TEST_REPORT.md) and
[generation audit](docs/architecture/GENERATION_ENGINE_AUDIT.md) for exact
commands and remaining limits.

### Historical stabilization evidence (2026-11-12)

The local stabilization run started on `develop` at
`986d2a7b15fe9aee46f4579688b694862dd64485`; its fixes are uncommitted. The
three generation fixtures listed below validated and generated successfully
into fresh temporary directories. Manifests/reports parsed as JSON and artifact
paths, sizes, and SHA-256 values matched. Detailed counts and exact results are
in the [completion and test report](docs/COMPLETION_AND_TEST_REPORT.md).

This is not a green test baseline: focused/provider/agent/full `go test` runs
did not complete cleanly on this Windows machine (`CGO_ENABLED=0`, Application
Control restrictions, and other Windows integration limitations). `go vet`,
the Provider/Agent build, and `git diff --check` passed. The local `gofmt -l`
check listed all files because the Windows working tree uses CRLF while Git
stores Go sources with LF; the LF-normalized temporary-copy check passed, and
the Windows CI checkout now disables CRLF conversion. The latest available CI
run on the inspected HEAD failed on Linux (SQLite driver not registered) and
Windows (`go` missing from the MSYS2 shell); local uncommitted fixes are
awaiting CI verification. Release readiness therefore remains **not ready**.

`validate` is read-only. `plan` only describes deterministic work.
`generate-all` creates and publishes the supported artifact catalog
(inventory, topology, Terraform data, native device configuration, and
bootstrap DHCP/DNS/TFTP/PXE/iPXE data for explicitly enabled services) with
one combined manifest; it does not contact infrastructure or apply a plan.
Bootstrap service artifacts are omitted unless the corresponding service is
explicitly enabled.

`generate-device-config` parses, resolves templates, and renders before
acquiring the output-root lock; generic standalone generators validate before
creating the root. Declarative publication uses the shared recovery journal
and only replaces prior outputs when its previous report and configuration
hashes verify. It rejects unmanaged files and locally edited generated
configurations without changing them. Those rejected requests leave a
previously absent `-out` path absent and do not change a pre-existing
directory. `generate-all`
intentionally may publish a `partial_failure` report when an individual site
fails. Existing output directories are never recursively deleted.

Full-generation publication uses a bounded journal and verified rollback
copies. If a reader process is interrupted, its
`.infraflow-generation-reader-*.lock` marker may remain and writers fail
closed. Only after stopping all InfraFlow processes that may read the output
root and confirming no reader is active, an operator may remove those exact
reader marker files. Do not remove active reader markers. A stale
`.infraflow-generation.lock` similarly requires confirming no writer is active
before removing only that lock file; the next generation then attempts journal
recovery. If rollback cannot be verified, generation and artifact reads expose
`ErrRecoveryRequired` and retain the journal/backup data instead of presenting
a partial catalog as valid. This rollback mechanism is not a filesystem-wide
atomic swap, and parent-directory durability is not guaranteed across
filesystems.

Run the scripted presentation demo from a Linux shell or Git Bash at the
repository root:

```sh
bash scripts/presentation-demo-launch.sh
python3 scripts/presentation-demo-verify.py <fresh-root-printed-by-launcher>
```

The launcher uses a fresh temporary root and existing Provider CLI commands to
validate, plan, and generate the Cisco/MikroTik, opt-in bootstrap, and
bootstrap-disabled multisite fixtures. It rejects unsupported OSPF and checks
that no output was published. The separate verifier checks generated JSON,
site isolation, manifests/reports, on-disk files, sizes, and SHA-256 values,
then displays the native Cisco `.cfg` and RouterOS `.auto.rsc`. Expected
completion ends with `PASS: demo verification complete; artifacts were not
applied or device-verified.` This follow-up demo and its new CI job have not
yet been executed on the local diff; no successful run is claimed. On Windows,
the existing `scripts/demo-provisioning.ps1` remains available, subject to the
machine's PowerShell execution policy. The completion report records the
observed local environment limitations.

The YAML parser accepts either `site` or `sites`, based on which top-level key
is present, and rejects documents containing both. Network validation rejects
overlapping static prefixes on interfaces of the same device, requires each
declared link endpoint's configured prefix to match the link network, and
rejects repeated interfaces within a link or interfaces assigned to multiple
links. It checks a static route's next hop against the selected (or any) directly
connected static interface prefix, and requires route destinations to use
canonical IPv4 network CIDRs. The validator does not resolve recursive routes,
DHCP-assigned gateways, VLANs, or dynamic routing; such reachability cannot be
proven from the current input model.

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

`generate-all` also emits text-only native configurations for the currently
implemented Cisco IOS-XE, MikroTik RouterOS 7, and FortiOS family profiles.
Native filenames use the declared device ID (or device name when no ID is
provided), so adding another device does not renumber existing artifacts. The
native manifest records model and software-version metadata when supplied but
marks compatibility `unknown`; family-level rendering is not a supported
model/version matrix or evidence of syntax validation. Unsupported intent is
rejected rather than silently omitted. Generated files are never applied
automatically.
The Cisco `.cfg` is generated text, not Cisco ZTP. The MikroTik `.auto.rsc`
is rendered but has not been proven on CHR or hardware. The current FortiOS
native renderer writes `.cfg` text and is render-only. No vendor is
hardware-validated, and generation is not delivery, execution, or verification.
The report labels document validation as `passed`, declaration-to-template
compatibility as render-only, command syntax validation as `not_performed`,
and physical-device validation as `not_performed`. A separate
`config_syntax_validation` status is `not_available`: no offline vendor syntax
parser is registered for Cisco IOS-XE, RouterOS 7, or FortiOS. Exact-output
tests verify the renderer's output, not vendor syntax acceptance.
`examples/fortios-native.yaml` is a render-only FortiOS example. No device
connection is used by this demo.

### RouterOS `.auto.rsc` render-only demo

`examples/provisioning-generation.yaml` exercises the existing Cisco native
`.cfg` renderer and the versioned RouterOS `auto-rsc` template through the
same `generate-all` pipeline. The RouterOS profile is limited to declared
model `RB750Gr3`, OS train `7.x`, and template version `1`; exact RouterOS
build and device compatibility remain `unknown`. It renders identity, IPv4
static/DHCP interfaces, and static routes only when those fields are present.
VLAN, interface admin-state, administration-service settings, upload, RouterOS
execution, and `.auto.log` retrieval are not implemented. Cisco ZTP remains
blocked because the repository evidence does not establish a model/release
profile compatible with the project's TLS-only requirement. Cisco AutoInstall
and Fortinet ZTP profiles are also unsupported.

Run the demo in PowerShell from a fresh temporary directory:

```powershell
go run ./provider/cmd validate -f examples/provisioning-generation.yaml
$demo = Join-Path $env:TEMP ("infraflow-provisioning-" + [guid]::NewGuid())
go run ./provider/cmd generate-all -f examples/provisioning-generation.yaml -out $demo
Get-Content (Join-Path $demo "routeros-auto-rsc-demo/native/device-mikrotik-r1.auto.rsc")
Get-Content (Join-Path $demo "routeros-auto-rsc-demo/native/native-manifest.json")
Get-Content (Join-Path $demo "routeros-auto-rsc-demo/site-manifest.json")
Get-FileHash (Join-Path $demo "routeros-auto-rsc-demo/native/device-mikrotik-r1.auto.rsc") -Algorithm SHA256
Get-Content (Join-Path $demo "generation-report.json")
```

The generated `.auto.rsc` is an inert catalog artifact until an operator
separately chooses an upload path. It uses stable `infraflow:` comments and
RouterOS `find`/conditional `set` commands for its own address, DHCP-client,
and route entries, avoiding unconditional duplicate `add` operations on
repeat import. This is template-level repeat-safety, not CHR validation; do
not treat it as proven until the exact RouterOS build has been tested. Its
hashes establish file integrity only, not device acceptance or execution.
Because upload automatically executes `.auto.rsc`, only upload it through an
authorized isolated lab workflow. After upload, retrieve and inspect the
corresponding `.auto.log` separately. See the [RouterOS CHR runbook](docs/lab/routeros-chr.md).

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

Ansible is not part of the InfraFlow architecture: there is no Ansible generator, artifact contract, runner, or CLI command. The project focus is device-specific ZTP and AutoInstall workflows. Today, the provider emits generic static DHCP/DNS/TFTP/PXE/iPXE bootstrap contracts and native text configurations; these are not yet complete vendor ZTP/AutoInstall flows, are not automatically applied, and remain unverified against physical devices. See the [vendor provisioning audit](docs/vendor-provisioning-audit.md) for the current capability matrix, architecture boundaries, official references consulted, and evidence limits.

### Four-site Agent synchronization demo

`examples/multisite-preparation.yaml` is a documentation-only preparation
scenario for Cisco IOS-XE, MikroTik RouterOS, and FortiOS family labels across
four sites. It declares RouterOS `7.x` as scenario metadata but does not claim
that a particular device was observed running that version. It omits exact
device models and interfaces. Its shared `192.0.2.0/24` bootstrap range is
TEST-NET documentation space; bootstrap artifacts are omitted because no
bootstrap service is explicitly enabled. Vendor labels and OS metadata are
not evidence that the devices or generated vendor-specific configuration are
compatible.

`generate-all` runs independent site generation in parallel, capped at four
workers. Each site is generated and hash-checked in a private staging
directory. Successful sites publish only after their files validate; a site
failure is recorded without discarding other completed sites, and the command
still returns an error/global `partial_failure` status. Output includes a root
`manifest.json` and `generation-report.json`; each successful site contains
the existing Agent catalog `manifest.json`, a `site-manifest.json` with
artifact sizes and SHA-256 values, and `generation-report.json`. The catalog
manifest is published last for that site, so incomplete files are not offered
to Agents. Sites with errors have a failed site manifest/report but no
Agent-visible catalog manifest.

Validate, plan, and generate the versioned catalog into a fresh temporary
directory:

```sh
go run ./provider/cmd validate -f examples/multisite-preparation.yaml
go run ./provider/cmd plan -f examples/multisite-preparation.yaml
demo_dir="$(mktemp -d "${TMPDIR:-/tmp}/infraflow-multisite.XXXXXX")"
go run ./provider/cmd generate-all \
  -f examples/multisite-preparation.yaml -out "$demo_dir/catalog"
cat "$demo_dir/catalog/site-a/inventory.json"
cat "$demo_dir/catalog/site-a/manifest.json"
cat "$demo_dir/catalog/manifest.json"
cat "$demo_dir/catalog/site-a/site-manifest.json"
sha256sum "$demo_dir/catalog/site-a/inventory.json"
```

For an actual loopback transfer/report run, execute the end-to-end test:

```sh
go test -count=1 ./agent/internal/application \
  -run '^TestMultiSiteAgentCLITransfersOnlyAuthorizedArtifacts$'
```

The test builds and launches the real Provider and Agent executables, starts
four Agents with distinct site-scoped gRPC tokens, and checks the downloaded
file hashes, per-site state directories, absence of disabled bootstrap
artifacts, and the four reports persisted by the Provider. It also checks that
the unsupported OSPF
example is rejected without publishing a config or generation report. All
runtime state and generated files live under Go's temporary test directory.
`TestRunSitesInParallelStartsIndependentWorkersConcurrently` verifies that the
production site worker pool starts two independent site jobs at the same time;
`TestGenerateAllKeepsSuccessfulSitesWhenAnotherSiteFails` verifies that a
failed site does not remove a successful site's catalog. The Agent downloads
the site manifest first and compares its declared paths, types, and hashes
against the Provider catalog before accepting the rest; it also checks each
downloaded file's declared byte size and SHA-256 before publication.
`agent_credentials` binds each gRPC token
to one `agent_id` and `site_id`; in this mode the legacy global machine token
cannot access the gRPC artifact catalog. Keep the global token in a secret
manager/environment for the existing HTTP registration endpoint, and provide
each Agent's scoped token separately using `provider.artifact_token_env`.
When site manifests are published, Provider startup verifies that every
published site has exactly one configured Agent credential and rejects
credentials for sites absent from the published catalog.
The shared-token legacy gRPC mode remains limited to a single-site catalog.
Each scoped Agent credential requires a future `expires_at` timestamp and is
checked on every gRPC request. A remote single-site deployment, or a remote
HTTP registration listener using the legacy global token, must set
`token_expires_at`; the global token is checked on both HTTP and gRPC requests.
Rotation and revocation are
configuration-driven: update the secret environment value or remove the
scoped credential, then restart the Provider; the old token is no longer
accepted by the restarted service. The Provider does not hot-reload tokens.

For example, the Provider's site credentials and one Agent's token references
have this shape (all referenced environment values must be secret and at least
32 bytes; every scoped Agent token must be unique and different from the global
machine token):

```yaml
# Provider configuration
token_env: INFRAFLOW_AGENT_TOKEN
token_expires_at: "2099-12-31T23:59:59Z" # replace with the approved rotation deadline
tls:
  certificate_file: /run/secrets/provider.crt
  key_file: /run/secrets/provider.key
  client_ca_file: /run/secrets/agent-ca.crt
agent_credentials:
  - agent_id: agent-site-a
    site_id: site-a
    token_env: INFRAFLOW_SITE_A_GRPC_TOKEN
    expires_at: "2099-12-31T23:59:59Z" # replace with the approved rotation deadline
```

```yaml
# Agent configuration for site-a
agent:
  id: agent-site-a
  site_id: site-a
  state_directory: /var/lib/infraflow-agent
provider:
  address: provider.example:8443
  api_address: https://provider.example:8080
  token_env: INFRAFLOW_AGENT_TOKEN
  artifact_token_env: INFRAFLOW_SITE_A_GRPC_TOKEN
  tls:
    enabled: true
    ca_file: /etc/infraflow/provider-ca.pem
    certificate_file: /run/secrets/agent.crt
    key_file: /run/secrets/agent.key
```

For optional gRPC mutual TLS, configure `tls.client_ca_file` on the Provider
with a CA that issues Agent client certificates, and configure the matching
Agent `certificate_file` and `key_file`. The Agent always verifies the
Provider certificate using the configured CA file or the operating system
trust store; there is no verification-disable option. Mutual TLS applies to
the Agent gRPC channel. The separate HTTP registration/API listener continues
to use server-authenticated TLS and its existing bearer/session authentication.

This scenario proves local generation, authenticated transfer, hash checks,
and report acceptance only. Agent status `completed` means supported artifact
processing/staging; it does not mean Terraform ran, a bootstrap
service started, or a network device was configured or verified. The demo does
not perform any network/device operation.

The provider can also persist validated planning jobs and expose them through its authenticated HTTP API when `api_listen_address` is set in the provider configuration. Plain HTTP is loopback-only; a remote API requires the configured TLS certificate/key pair. Job creation validates and plans only; it does not provision devices.

The same API exposes a separate admin-only execution lifecycle at
`/api/v1/executions`. It uses an idempotency key, persists task attempts and
results, and is connected to the existing dependency scheduler. The Provider
registers two operational task executors: `artifact.verify` for read-only
cryptographic artifact verification, and `cisco.ios.configure` for strictly
validated Cisco IOS hostname and Ethernet interface configuration over SSH.
The Cisco executor reads the requested running-config state before mutation,
reports it separately, and verifies the post-apply read-back; it does not
persist startup-config. Unsupported task types are rejected. Execution
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

To serve generated files to a single-site legacy agent, set a strong token through a secret manager or environment variable. For multisite gRPC access, configure unique `agent_credentials` entries in the Provider configuration and set the matching `artifact_token_env` on each Agent. The provider uses gRPC streaming; plaintext is restricted to loopback. Remote deployments require TLS.

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

The Linux CI workflow runs `govulncheck` in source mode, which fails when it
finds a reachable vulnerability. It also enforces package coverage floors of
90% for `agent/internal/adapters/processor` and 65% for
`agent/internal/delivery/cli`; the current measured results are 93.2% and
66.9%, respectively. `BenchmarkGenerateAll` measures repeated generation, and
`BenchmarkArtifactRepositoryOpen` measures verified artifact reads. Benchmarks
are available via `go test -run=^$ -bench 'BenchmarkGenerateAll|BenchmarkArtifactRepositoryOpen' -benchmem ./internal/adapters/generation ./provider/internal/adapters/filesystem`.

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
- [x] Generic data-only Terraform configuration generation (without provider or apply)
- [x] Generic DHCP/DNS/TFTP/PXE/iPXE bootstrap artifact generation (without agent execution)
- [x] Removal of Ansible generation and execution surfaces; ZTP/AutoInstall remain the next provisioning focus
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
