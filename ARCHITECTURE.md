# Architecture

## Service boundary

The provider and agent live in separate top-level folders and build as independent binaries. The agent imports no provider package; they communicate only through the versioned protobuf contract in `api/proto/infraflow/v1` and the shared `pkg/protocol` data types.

```text
provider: YAML -> validate -> plan -> deterministic artifacts -> authenticated gRPC catalog/stream/report service
agent:    YAML config -> gRPC catalog -> streamed hash-verified files -> supported local processing -> state report
users:    HTTP login -> SQLite-backed session -> role-authorized provider API
```

The agent cannot validate infrastructure or generate provider artifacts. It streams published files to disk, verifies chunk metadata and final SHA-256 before atomic commit, processes supported generic inventory/topology, and reports state. Provider configuration and generation remain independent of agent implementation.

The provider executable owns user commands (`validate`, `plan`, `generate`, `serve`). Its gRPC API exposes health, authenticated catalog/stream/report operations; it does not accept remote desired-state submissions or execute device provisioning jobs.

The provider HTTP API separates machine and human authentication. The agent
token is accepted only for agent operations and remains compatible with the
gRPC service. Human sessions are opaque random bearer tokens whose SHA-256
hashes are stored in SQLite; password material is stored only as bcrypt hashes.
`admin` is required for user, agent, and audit-event management. `user` may
manage planning jobs but cannot access those administrative resources.

The provider core keeps domain types independent of YAML, HTTP, a database, Terraform, SSH, and device vendors. Ansible is not used. Configuration owns YAML decoding and diagnostics; planning consumes validated domain data; generation produces deterministic native configuration and bootstrap files.

The dependency direction is explicit: `internal/domain` contains business
objects, `internal/application` contains use cases, `internal/ports` contains
contracts owned by those use cases, and `internal/adapters` contains concrete
YAML, planning, generation, and filesystem integrations. The provider
application receives its parser, planner, repositories, and generators through
ports rather than constructing concrete adapters.

Cross-cutting implementation details are kept below the application boundary:
`internal/infrastructure` contains filesystem safety and authentication
helpers, while `internal/application` contains scheduling and desired/observed
state reconciliation. Delivery packages compose these layers at the binary
boundary.

## Safety boundary

- Validation never writes files or contacts infrastructure.
- Planning describes work; it does not execute it.
- Artifact generation is confined to the explicitly selected provider output directory.
- Agent streams are confined to the explicitly selected local state directory and hash-checked before atomic commit.
- The generic scheduler is connected through the Provider execution application service and a typed executor port. Execution records, attempts, results, and per-execution transition history are persisted; the Provider composition registers two operational executors, `artifact.verify` (read-only cryptographic SHA-256 verification of published artifacts) and `cisco.ios.configure` (verified Cisco IOS Ethernet interface and hostname configuration over SSH with read-back verification), and refuses all unregistered task types.
- Desired and observed state comparison is isolated in the reconciliation core; it reports drift and hashes without changing either state.
- Supported jobs, agent registration/heartbeat, and agent reports are recorded in a private append-only hash-chained event store.
- The generic admin-only execution API does not run shell commands and accepts only registered task types (`artifact.verify`, `cisco.ios.configure`); device configuration deployment for other vendors (FortiGate, MikroTik, Proxmox) is not supported. The agent includes explicitly started DHCPv4, read-only TFTP, and bootstrap HTTP commands, but these services do not start during the normal agent run and must be restricted to an isolated provisioning network. Bootstrap files are generated and manifest-published; generating them does not start a service.
- The Cisco executor's live GNS3 validation remains **NON EXÉCUTÉ**. A local GNS3 server process was observed during the 2026-10-14 check, but no authorized Cisco IOS node/address was confirmed; the observed QEMU guest was named `fortigate-1` and was not contacted. The read-only preflight and operator procedure are documented in [docs/lab/cisco-ios-gns3.md](docs/lab/cisco-ios-gns3.md).
- Unknown provider capabilities remain unknown and are never treated as supported.
- Provisioning development is focused on device-specific ZTP and AutoInstall. Existing DHCP/DNS/TFTP/PXE/iPXE output is generic bootstrap data, not a verified vendor provisioning flow.
- A new user database fails closed until an administrator is generated by the backend; the password is written only to protected local files and never logged or stored in YAML.

## Growth path

The provider has durable planning jobs and a separate persistent execution lifecycle connected to the scheduler, exposed to administrators via the web console and Linux TUI. Two operational task executors are registered: `artifact.verify` for catalog artifact integrity verification, and `cisco.ios.configure` for verified Cisco IOS configuration over SSH without automatic startup-config persistence. Remaining work includes live IOS/GNS3 lab validation (NON EXÉCUTÉ; no authorized IOS target was confirmed on 2026-10-14), adding other device-specific adapters (FortiGate, MikroTik, Proxmox) only after their evidence criteria are met, DNS service, and advanced networking features. Provider- and service-specific behavior belongs in isolated adapters and must satisfy the support evidence required by [INFRAFLOW_SPEC.md](INFRAFLOW_SPEC.md).
