# 01 — Implementation report

All changes are on `develop`, commits `9932eca` … final (see 08_CHANGELOG.md). Each defect fix carries a
regression test that fails on the previous code (checked by mutation or by running against the old file).

## Provider

- **SSH algorithm policy** (`provider/internal/adapters/executors/ssh_policy.go`): only algorithms that x/crypto
  classifies as supported and that do not use SHA-1 are offered. `ssh_legacy_sha1_targets: ["site/device"]` grants
  the legacy set (dh-group14-sha1, hmac-sha1, ssh-rsa) to listed devices only. Negotiation failures name the exact
  entry to add. Applied to both the Cisco and RouterOS transports.
- **Verified rollback** (`cisco_ios_configure.go`): after an apply or verification failure the executor restores
  only the fields whose preflight value differs from the request (hostname, description, static/DHCP address,
  admin state), refuses to touch addressing it cannot represent, and verifies the result by read-back. The result
  reports `rollback.verified` or "rollback failed … state uncertain".
- **Cross-execution device locks** (`device_lock.go`): one configuration session per device across executions.
- **RouterOS import executor** (`mikrotik_routeros_import.go`, `routeros_transport.go`): resolves the device from the
  published inventory, re-hashes the stored script against the catalog, checks RouterOS 7, uploads over SCP,
  runs `/import verbose=yes` chained with a completion marker, removes the file, reads back identity, addresses,
  DHCP clients, routes and the OSPF instance and compares them with the intent. Never retried automatically.
- **No automatic retries** for device-mutating tasks (type list or `NeverRetry()`).
- **Session layer**: `end` at a known EXEC prompt is a no-op (IOS XE treats it as a host name; defect D5).
- **Web plan-and-generate** asks the native generator (`ports.NativeSupportChecker`) instead of a separate table (D6).

## Agent

- Per-Agent log directory by default (`<state_directory>/logs`) and `Outbox.PendingFor(agent, site)` so an Agent
  never sends — and is never blocked by — another identity's events (D0).

## Generation

- Cisco IOS `provisioning.method: ssh` accepted (render-only review config) instead of failing the whole site.
- RouterOS auto-rsc blocks adopt an identical entry without an owner comment before adding (D1) and quote prefix
  values in `find where` (D3).
- `CheckNativeDevice(site, device)` exposes the generator's own support decision.

## Diagnostics

- `internal/diagnostics/capture.go`: bounds-checked libpcap reader (Ethernet, 802.1Q, IPv4/UDP, BOOTP options,
  TFTP RRQ/DATA/ACK/ERROR), per-client selection by MAC, fuzzed. New codes: `DHCP_REQUEST_WITHOUT_ACK`,
  `DHCP_DISCOVER_WITHOUT_OFFER` (wire only), `DHCP_DECLINE`; wire evidence raises `DHCP_OFFER_NOT_ACCEPTED` to
  high confidence; a TFTP transfer cut by the end of a capture is incomplete evidence, not a failure.
- CLI: `infraflow-agent diagnose -capture <pcap> [-client-mac <mac>]`.

## Test tooling

- `scripts/qa/gate.sh`: one-command local gate with a summary file; NOT_RUN is never PASS.
- `scripts/qa/web_e2e.mjs`: 16 headless-Chromium scenarios over raw W3C WebDriver (no npm dependencies).
- `scripts/lab/e2e.py`: reusable GNS3 lab driver (setup, Provider/Agents, DHCP/TFTP, executions, known hosts).
- `provider/internal/adapters/executors/lab_session_probe_test.go` (`-tags lab`): drives the real SSH session
  against a lab router and times each command.

## Not implemented (by decision, documented as UNSUPPORTED)

FortiOS delivery executor; Cisco OSPF/routing through the executor; VLAN modelling; RouterOS import rollback;
IOS XE AutoInstall/ZTP; user deletion in the web console; routes/NAT through web plan-and-generate.
