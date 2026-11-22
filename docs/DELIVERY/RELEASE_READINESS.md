# Release readiness

## Score: 87/100 (2026-11-22, evidence-based)

Same eight-dimension rubric and weights as the 76 and 85 scores, so they are comparable. Not a release approval.
Base `c1917ac` plus uncommitted changes; hosted CI has not run on them.

| Dimension | Weight | 2026-11-20 | Now | Justification (evidence) |
|---|---:|---:|---:|---|
| Provider correctness and reliability | 15 | 14 | 14 | New: load + SIGKILL recovery harness (0 errors, recovery 1.7 s, same sites, session kept); two performance defects fixed (D9, D10). −1: no multi-hour soak; execution load against devices not measured. |
| Agent reliability and lifecycle | 15 | 13 | 14 | New: interrupted sync tested over a real cut TCP connection with restart (AGT-10); incomplete/stale/rejected/duplicate reports classified and tested (AGT-11, D11). −1: Agents still deliver to devices only through DHCP/TFTP. |
| Provisioning and vendor compatibility | 20 | 14 | 14 | IOSv M6 AutoInstall explained (image-side) and reclassified UNSUPPORTED; no new capability. −6: FortiGate BLOCKED, IOL-XE BLOCKED, IOSv AutoInstall unusable, no VLANs, no FortiOS executor. |
| Real GNS3 integration tests | 15 | 12 | 13 | New: Go Controller client, reproducible isolated topology, host pre-check, validated JSON reports, all run on the real server. −2: two appliances blocked; earlier device scenarios are text logs and were not re-run; no GNS3 CI category. |
| Admin Web and API functionality | 10 | 9 | 9 | Report outcome now shown truthfully. −1: no user deletion; routes/NAT not plannable from the web; no GNS3 view. |
| Diagnostics and observability | 10 | 9 | 9 | New: reference DHCP comparison with a confirmed, evidence-backed device-side cause; report rejection events. −1: REQUEST-without-ACK still proven on synthetic captures only. |
| Security | 10 | 9 | 9 | govulncheck 0 reachable; GNS3 credentials from environment only, never logged; report secret scan. −1: SHA-1 exception for IOSv/IOS XE 16.7; DHCP/TFTP unauthenticated; GNS3 controller uses default admin/admin. |
| Documentation and delivery evidence | 5 | 5 | 5 | 05/06/07 updated with dated notes, GNS3_VALIDATION_20261122.md, evidence SHA256SUMS. |
| **Total** | **100** | **85** | **87** | |

### What would raise the score

1. FortiGate access, then a FortiOS executor (+ provisioning, GNS3).
2. IOL-XE image; re-running CHR/CSR/c7200 scenarios as JSON reports; a GNS3 CI runner.
3. Startup-config persistence in the executors (GNS-04); a soak run.
4. VLANs in the model (MTK-08), routes/NAT from the web (GEN-06), user deletion with audit (WEB-06).

## Previous score: 85/100 (2026-11-20)

Same eight-dimension rubric and weights as the 76/100 baseline of 2026-11-19, so the two are comparable.
This is not a release approval. Details: [artifacts/finalization/07_FINAL_DELIVERY_REPORT.md](../../artifacts/finalization/07_FINAL_DELIVERY_REPORT.md).

| Dimension | Weight | Before | Now | Justification (evidence) |
|---|---:|---:|---:|---|
| Provider correctness and reliability | 15 | 13 | 14 | Verified rollback (IOS and IOS XE, on devices), cross-execution device locks, crash → "outcome unknown", timeouts, idempotency, retry refusal, all on real routers. −1: no load/soak test. |
| Agent reliability and lifecycle | 15 | 13 | 13 | Shared-outbox defect fixed and re-tested with two Agents; reconnection, token expiry, cross-site 403 in the lab. −2: interrupted sync and incomplete reports have no dedicated test; Agents deliver to devices only via DHCP/TFTP. |
| Provisioning and vendor compatibility | 20 | 11 | 14 | New: InfraFlow-driven RouterOS 7 delivery verified on CHR (4 device defects fixed); IOS XE 16.7 executor on CSR1000v; IOSv executor. c7200 AutoInstall PASS. −6: FortiGate BLOCKED (password), IOL-XE BLOCKED (image), IOSv AutoInstall PARTIAL (M6 and M8), no VLANs, no FortiOS executor. |
| Real GNS3 integration tests | 15 | 11 | 12 | Seven appliances in an isolated project; ~60 scenarios with execution IDs and console read-back; OSPF FULL between generated CHR and IOSv. −3: two appliances blocked; device scenarios are harness-driven text logs, no GNS3 CI category. |
| Admin Web and API functionality | 10 | 8 | 9 | 16/16 headless-Chromium scenarios on the final tree (auth, revocation, RBAC 403, users, editor, executions, logs, diagnostics, outage); one defect found and fixed. −1: no user deletion; routes/NAT not plannable from the web. |
| Diagnostics and observability | 10 | 8 | 9 | pcap input, fuzzed; wire-confirmed diagnoses on real IOSv, IOSvm8 and c7200 captures; two false diagnoses fixed. −1: REQUEST-without-ACK and DISCOVER-without-OFFER proven on synthetic captures only. |
| Security | 10 | 8 | 9 | SHA-1 SSH algorithms off by default with per-device exceptions; host keys pinned and fail-closed (fingerprints matched device consoles); secret value scan clean; 0 reachable vulns. −1: IOSv and IOS XE 16.7 require the SHA-1 exception; DHCP/TFTP unauthenticated. |
| Documentation and delivery evidence | 5 | 4 | 5 | Reports 00–08, traceability matrix (88 features), SHA256SUMS, hosted CI on the final code commit. |
| **Total** | **100** | **76** | **85** | |

## What would raise the score

1. FortiGate access, then a FortiOS executor.
2. A reference DHCP server comparison to explain the IOSv AutoInstall behaviour.
3. IOL-XE image; a GNS3 CI runner with JSON scenario reports.
4. Tests for interrupted Agent sync and incomplete reports; a load test.

## Evidence

[artifacts/finalization/](../../artifacts/finalization/) (00–08 and `evidence/`, hashes in `evidence/SHA256SUMS`).
Earlier: [artifacts/qa/](../../artifacts/qa/), [artifacts/security/](../../artifacts/security/).
