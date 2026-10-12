# InfraFlow Current Status

Updated: 2026-10-12
Branch: `develop`
Base Commit: `b2c65cce4d0ad76c1514817e4de1285bc0d3856f`

## Implemented and verified in the repository

- Provider validation and deterministic planning are separate from generation and execution. Creating or retrying a job does not provision infrastructure.
- Planning jobs are persisted as private JSON records; the authenticated API supports list, read, cancel, and retry within the implemented status rules.
- A separate execution aggregate persists queued/running/succeeded/failed/cancelled/timed_out state, tasks, attempts, outputs, errors, actors, and transition history. It uses a typed executor port and the existing scheduler.
- Admin HTTP routes submit with idempotency, list with filters/pagination, read attempts/results/events, and request cancellation. The default Provider registers no executor and rejects unsupported types instead of reporting a false success.
- Artifact generation publishes deterministic, hash-verified catalog entries. Vendor artifacts remain marked unverified; execution and verification are not requested.
- The Agent downloads published artifacts, verifies the stream and content hashes, stores them locally, processes generic inventory/topology, and reports artifact-level outcomes.
- The Agent has a local log outbox and can synchronize logs over the authenticated provider contract.
- The scheduler implements dependency graphs, global concurrency, resource locks, cancellation, retries, and timeouts. Provider startup wires it to the persistent execution service; no device executor is registered.
- Bootstrap artifacts can be generated. Agent DHCPv4, read-only TFTP, and bootstrap HTTP servers are separate explicit commands; they are not started by the normal agent run. There is no DNS daemon.
- Authenticated provider HTTP/gRPC APIs expose their existing planning, agent, event, and log records. The execution API is admin-only; administrators can inspect paginated execution lists, view task/attempt/error details, and request audited cancellation from both the web dashboard and the Linux TUI.

## Evidence from Phase 3.1 (Correction des états réels et validation du TUI)

- **Local Components Truthful Status Inspection**:
  - **Artifact Store**: live observable inspection of `StateDirectory` (`os.Stat`, `os.ReadDir`), reporting explicit states: `Non configuré`, `Absent`, `Inaccessible`, `Dégradé`, or `Opérationnel` with real directory entry count. Explicit truth statement: integrity is marked "Non auditée par le TUI (intégrité SHA256 validée par l'agent lors des réceptions gRPC)".
  - **Log Outbox**: live observable inspection of `filepath.Join(LogDirectory, "pending-events.json")`, distinguishing `Non configuré`, `Absent` (missing file), `Inaccessible` (permission error), `Opérationnel` (empty file or N pending events decoded), and `Dégradé` (invalid JSON/mode).
  - **Tool Runner**: live verification of executables via `exec.LookPath("ansible-playbook")` and `exec.LookPath("terraform")`, distinguishing `Opérationnel` (both present), `Dégradé` (one missing), and `Absent` (neither on PATH).
  - **Network Bootstrap Services**: documented honestly as distinct CLI commands (`serve-dhcp`, `serve-tftp`, `serve-bootstrap`) that are not started by default (`Non démarré`), and confirmation that no DNS daemon is implemented.

- **Process Separation**:
  - Local process PID correctly attributed to `TUI Process (PID %d)`.
  - Agent daemon reported as `Agent Daemon: Non vérifié (aucun mécanisme de supervision locale)` — no fabricated PIDs or heartbeat metrics.

- **CLI Configuration Loading & Validation**:
  - In `agent/internal/delivery/cli/cli.go`, if `-config` is supplied to `infraflow-agent tui` and cannot be loaded or fails validation, execution halts with exit code 2 and a clean error message to stderr with secret redaction. Unsupplied `-config` preserves fallback behavior.

- **Offline Mode & Session In-Memory Distinction**:
  - Eliminated false claims of local disk cache: disconnected state with in-memory data displays `[Données en mémoire de session (non persistées sur disque)]`, with the last successful sync timestamp clearly shown.
  - Startup disconnect displays `Indisponible (aucune donnée reçue)`.
  - Full local navigation is retained when offline.

- **Verification and Testing**:
  - Deterministic tests added in `agent/internal/delivery/tui/model_test.go` covering unconfigured, absent, inaccessible, empty, populated, and corrupt states for all components, plus initial vs subsequent disconnect scenarios.
  - Tests added in `agent/internal/delivery/cli/cli_test.go` covering `-config` missing, malformed, and incomplete scenarios.
  - `go test -count=1 ./...`: 100% passed cleanly.
  - `make fmt-check`: clean.
  - `make vet`: clean.
  - `make build`: compiles `bin/infraflow-provider` and `bin/infraflow-agent` without errors.

## Remaining work

- Register a real, explicitly authorized executor only after defining its allowed task type, parameter schema, privilege boundary, and independent evidence. Until then all unregistered execution types remain rejected.
- Implement or explicitly defer a DNS service and other unimplemented bootstrap services.
- Run the final full test suite and binary builds in the separate final campaign. Docker and GNS3 validation remain separate deployment gates.

## Next implementation task

Define and authorize the first operational task executor adapter with its strict safety policy, schema validation, and acceptance evidence. Do not register vendor executors without real lab verification.