# 05 — Feature traceability matrix

Status values: PASS, FAIL, PARTIAL, BLOCKED, NOT_RUN, NOT_APPLICABLE, UNSUPPORTED.
PARTIAL, BLOCKED and NOT_RUN are never counted as PASS. "Impl" = implemented in
the code at the final commit. Rows changed on 2026-11-22 say so in their note and keep the previous status. "Test level": U = unit/race, I = integration
(real binaries, lab host), D = real device in GNS3, W = real browser.
Evidence paths are relative to `artifacts/finalization/evidence/`.

| ID | Feature | Area | Impl | Test level | Status | Evidence | Notes |
|---|---|---|---|---|---|---|---|
| PRV-01 | Execution API with Idempotency-Key, 202 on create | Provider | yes | U, D | PASS | cisco/r1-executor-rollback-idempotency.txt (I1) | |
| PRV-02 | Idempotent replay (same key, same body → same execution, 200) | Provider | yes | U, D | PASS | cisco/r1-executor-rollback-idempotency.txt (I2) | |
| PRV-03 | Same key, different body → 409 | Provider | yes | U, D | PASS | cisco/r1-executor-rollback-idempotency.txt (I3) | |
| PRV-04 | Device unreachable reported (immediate refusal) | Provider | yes | U, D | PASS | cisco/r1-executor-failures.txt (F1), mikrotik/routeros-import-executor.txt (M11) | |
| PRV-05 | Connect timeout (silent device) | Provider | yes | D | PASS | cisco/r1-executor-failures.txt (F1b, 10.0 s) | |
| PRV-06 | Invalid device credentials | Provider | yes | U, D | PASS | cisco/r1-executor-failures.txt (F2), mikrotik/… (M10) | secret not echoed |
| PRV-07 | Apply failure → automatic rollback verified by read-back (IOS) | Provider | yes | U, D | PASS | cisco/r1-executor-rollback-idempotency.txt (R1, R2, regression) | |
| PRV-08 | Apply failure → automatic rollback (IOS XE) | Provider | yes | U, D | PASS | cisco/r3-csr1000v-iosxe-executor.txt (X4b, X5b) | failed before fix D5 |
| PRV-09 | Rollback failure reported as "state uncertain", no retry | Provider | yes | U, D | PASS | cisco/r3-csr1000v-iosxe-executor.txt (X4) | observed for real before D5 |
| PRV-10 | Read-back verification failure | Provider | yes | U | PASS | unit-tested (cisco_ios_configure_test.go) | no device run |
| PRV-11 | Two concurrent executions on one device serialized | Provider | yes | U, D | PASS | cisco/r1-executor-concurrency-restart.txt (K1, router SSH log) | mutation-checked |
| PRV-12 | Provider killed mid-execution → task failed "outcome unknown" | Provider | yes | D | PASS | cisco/r1-executor-concurrency-restart.txt (K2) | |
| PRV-13 | Device not in published inventory rejected | Provider | yes | U, D | PASS | cisco/r1-executor-failures.txt (F3) | |
| PRV-14 | Device of another vendor rejected per executor | Provider | yes | U, D | PASS | cisco/… (F4), mikrotik/… (M13) | |
| PRV-15 | Automatic retries refused for device-mutating tasks | Provider | yes | U, D | PASS | cisco/… (F5), mikrotik/… (M12) | |
| PRV-16 | Strict parameter validation (unknown fields) | Provider | yes | U, D | PASS | mikrotik/… (M14) | |
| PRV-17 | Host key pinning, unknown key fails closed with fingerprint | Provider/Security | yes | U, D | PASS | cisco/r1-executor-security.txt (S2), mikrotik/… (M1b) | |
| PRV-18 | Host-key rotation / revocation without restart | Provider/Security | yes | U | PASS | unit-tested (prior session S-1 fix) | not re-run on device |
| PRV-19 | Load behaviour (concurrent API, plan preview, multi-site generation, SIGKILL recovery) | Provider | yes | I | PASS | gns3-20261122/logs/load-report-after-fix.json, gns3-20261122/logs/load-report-before-fix.json | 2026-11-22 (was NOT_RUN): scripts/qa/load; found and fixed D9, D10 |
| PRV-20 | Multi-hour soak | Provider | — | — | NOT_RUN | — | 2026-11-22: only 10 s stages were run |
| AGT-01 | Registration and site authorization | Agent | yes | U, I | PASS | integration/agent-negative.txt | |
| AGT-02 | Catalog sync and SHA-256 verified staging | Agent | yes | U, I | PASS | integration/agent-negative.txt (A4, A5, A5b) | |
| AGT-03 | Wrong token → 401 | Agent | yes | I | PASS | integration/agent-negative.txt (A1) | |
| AGT-04 | Cross-site token → 403 | Agent | yes | I | PASS | integration/agent-negative.txt (A2) | |
| AGT-05 | Expired token → 401, restored token works | Agent | yes | I | PASS | integration/agent-token-expiry.txt | |
| AGT-06 | Provider unavailable reported, process survives | Agent | yes | I | PASS | integration/agent-negative.txt (A3), agent-reconnection.txt | |
| AGT-07 | Reconnection after Provider restart; offline after timeout | Agent | yes | I | PASS | integration/agent-reconnection.txt | |
| AGT-08 | Multiple Agents on one host, two sites | Agent | yes | I | PASS | integration/agent-outbox-regression.txt | failed before fix (shared outbox) |
| AGT-09 | Log outbox never sends other identities' events | Agent | yes | U, I | PASS | integration/agent-outbox-regression.txt | defect D0 fixed |
| AGT-10 | Interrupted sync (connection cut mid-transfer) | Agent | yes | I | PASS | agent/internal/application/sync_interruption_test.go | 2026-11-22 (was NOT_RUN): real gRPC client + on-disk store, TCP cut mid-stream, Agent restart; no partial file published, previous version kept; mutation-checked |
| AGT-11 | Incomplete report handling | Agent | yes | U | PASS | provider/internal/application/report_outcome_test.go | 2026-11-22 (was NOT_RUN): outcome NO_REPORT/INCOMPLETE/STALE/FAILED/COMPLETE against the current catalog; rejected and duplicate reports recorded (D11) |
| CIS-01 | Cisco 7200 12.4(24)T5 AutoInstall (DHCP+TFTP by InfraFlow) | Multi-vendor | yes | D | PASS | cisco/r2-c7200-postcheck.txt, captures/diagnose-real-captures.txt | |
| CIS-02 | IOSv 15.9(3)M6 AutoInstall | Multi-vendor | yes | D | UNSUPPORTED | gns3-20261122/CIS-02-REF.json | 2026-11-22 (was PARTIAL): image-side — AutoInstall handler stops in 0 ms with two independent DHCP servers; regular IOS DHCP client binds |
| CIS-03 | IOSv 15.9(3)M8 AutoInstall | Multi-vendor | no profile | D | PARTIAL | cisco/iosvm8-autoinstall.txt | same behaviour as M6; not re-run against the reference server on 2026-11-22 |
| CIS-04 | IOSv SSH executor (hostname, interfaces, verify) | Multi-vendor | yes | D | PASS | cisco/r1-executor-*.txt | SHA-1 exception required |
| CIS-05 | CSR1000v IOS XE 16.7 SSH executor | Multi-vendor | yes | D | PASS | cisco/r3-csr1000v-iosxe-executor.txt | after fix D5; SHA-1 exception required |
| CIS-06 | IOL-XE 17.18 | Multi-vendor | yes (generic) | — | BLOCKED | gns3-20261122/logs/gns3-inventory.json | licensed image not present (controller API, 2026-11-22: "docker image is not present on the compute") |
| CIS-07 | IOS XE AutoInstall / ZTP | Multi-vendor | no | — | UNSUPPORTED | — | not implemented |
| CIS-08 | Cisco OSPF/routing via executor | Multi-vendor | no | — | UNSUPPORTED | gns3/ospf-adjacency-mt1-r1.txt | peer configured by hand |
| MTK-01 | RouterOS auto-rsc generation (identity, IPs, routes, OSPF) | Multi-vendor | yes | U, I | PASS | integration/generation-pipeline.txt | |
| MTK-02 | InfraFlow-driven delivery (SCP upload + /import) on CHR 7.8 | Multi-vendor | yes | U, D | PASS | mikrotik/routeros-import-executor.txt (M7) | after fixes D1–D4 |
| MTK-03 | Read-back verification of identity/addresses/routes/OSPF | Multi-vendor | yes | U, D | PASS | mikrotik/… (M7, M9) | |
| MTK-04 | Idempotent re-import, no duplicates | Multi-vendor | yes | D | PASS | mikrotik/… (M8, M9) | |
| MTK-05 | Interrupted transfer never imported | Multi-vendor | yes | U | PASS | mikrotik_routeros_import_test.go | no device run |
| MTK-06 | RouterOS 6 refused | Multi-vendor | yes | U, I | PASS | unit + integration/generation-pipeline.txt | |
| MTK-07 | OSPF adjacency from generated config | GNS3 | yes | D | PASS | gns3/ospf-adjacency-mt1-r1.txt | |
| MTK-08 | VLANs | Multi-vendor | no | — | UNSUPPORTED | — | not in the model |
| MTK-09 | Rollback of a failed import | Multi-vendor | no | — | UNSUPPORTED | — | reported, never retried |
| FTN-01 | FortiOS native config generation | Multi-vendor | yes | U, I | PASS | integration/generation-pipeline.txt | rendering only |
| FTN-02 | FortiOS delivery / verification | Multi-vendor | no | D | BLOCKED | fortinet/fgt1-blocked.txt | admin password unknown; no executor |
| GEN-01 | Deterministic output (byte-identical reruns) | Generation | yes | I | PASS | integration/generation-pipeline.txt (G1) | |
| GEN-02 | Input-order independence | Generation | yes | I | PASS | integration/generation-pipeline.txt (G2) | |
| GEN-03 | Traceability (template id/version, input/output hashes) | Generation | yes | I | PASS | integration/generation-pipeline.txt (G3) | |
| GEN-04 | Invalid inputs rejected, no device artifact | Generation | yes | U, I | PASS | integration/generation-pipeline.txt (G4, 8 cases) | |
| GEN-05 | Web plan-and-generate agrees with generate-all | Generation | yes | U, W | PASS | web/web-e2e.json (W08) | defect D6 fixed |
| GEN-06 | Routes/NAT through web plan-and-generate | Generation | no | W | UNSUPPORTED | web/web-e2e.json (W08) | reported, not dropped |
| GNS-01 | Reusable lab driver (setup, services, exec, agents) | GNS3 | yes | D | PASS | scripts/lab/e2e.py, all device evidence | |
| GNS-02 | Machine-readable (JSON) lab reports | GNS3 | yes | U, D | PARTIAL | gns3-20261122/*.json, web/web-e2e.json | 2026-11-22: schema, validator and `gns3 report-check` (non-zero exit); earlier device scenarios (CHR, CSR, c7200) remain text logs |
| GNS-03 | Reachability (ping) after configuration | GNS3 | — | D | PASS | cisco/r2-c7200-postcheck.txt, mikrotik/… (M9) | |
| GNS-04 | Persistence across device reload | GNS3 | no | — | NOT_RUN | — | executors do not write startup config |
| GNS-05 | Go GNS3 Controller client (v3 bearer, v2 fallback, typed errors, timeouts, no partial lists) | GNS3 | yes | U, D | PASS | internal/gns3/client_test.go, gns3-20261122/GNS-01.json | 2026-11-22; real-server run found D12 |
| GNS-06 | Reproducible isolated topology (templates by name, idempotent, refuses foreign project) | GNS3 | yes | U, D | PASS | internal/gns3/topology_test.go, gns3-20261122/GNS-03.json | scripts/lab/topologies/infraflow-automated-validation.json |
| GNS-07 | Host CPU/RAM/disk pre-check before starting nodes | GNS3 | yes | U, D | PASS | internal/gns3/topology_test.go, gns3-20261122/logs/host-check.json | unknown values fail closed |
| GNS-08 | Scenario report validation (statuses, stages, secrets, evidence hashes) | GNS3 | yes | U | PASS | internal/gns3/report_test.go, provider/internal/delivery/cli/gns3_test.go | |
| GNS-09 | GNS3 state in the Admin Web | GNS3 | no | — | NOT_RUN | — | GNS3 is CLI lab tooling only; the Provider never contacts GNS3 |
| DIA-01 | Classification from Provider/Agent/service logs | Diagnostics | yes | U, I | PASS | unit tests, prior session | |
| DIA-02 | pcap input (DHCP/TFTP wire states) | Diagnostics | yes | U, D | PASS | captures/diagnose-real-captures.txt | new |
| DIA-03 | DISCOVER without OFFER | Diagnostics | yes | U | PASS | capture_test.go | no real capture of it |
| DIA-04 | OFFER not accepted (wire-confirmed) | Diagnostics | yes | U, D | PASS | captures/…, cisco/iosvm8-autoinstall.txt | |
| DIA-05 | REQUEST without ACK | Diagnostics | yes | U | PASS | capture_test.go | no real capture of it |
| DIA-06 | NAK / DECLINE | Diagnostics | yes | U | PASS | capture_test.go | |
| DIA-07 | TFTP missing file / transfer failure / truncated capture | Diagnostics | yes | U, D | PASS | capture_test.go, captures/… | |
| DIA-08 | Hash mismatch, auth rejection, unreachable Provider | Diagnostics | yes | U, I | PASS | unit tests, integration/agent-negative.txt | |
| DIA-09 | Reference DHCP server comparison for IOSv | Diagnostics | — | D | PASS | gns3-20261122/CIS-02-REF.json | 2026-11-22 (was NOT_RUN): ISC dhcpd 4.4.3 in the isolated project, 3 option sets, captures |
| WEB-01 | Sign-in / wrong password | API/Web | yes | W | PASS | web/web-e2e.json (W01–W03) | |
| WEB-02 | Session survives refresh; sign-out clears it | API/Web | yes | W | PASS | web/… (W04, W15) | |
| WEB-03 | Server-side session revocation / expiry | API/Web | yes | W | PASS | web/… (W13) | TTL expiry itself unit-tested |
| WEB-04 | Non-admin cannot see admin views or call admin API | API/Web | yes | W | PASS | web/… (W14, 403) | |
| WEB-05 | Users: create, disable, enable | API/Web | yes | W | PASS | web/… (W06) | |
| WEB-06 | Users: delete | API/Web | no | — | UNSUPPORTED | — | disable only |
| WEB-07 | Editor validation errors, generation, plan submission | API/Web | yes | W | PASS | web/… (W07–W09) | |
| WEB-08 | Executions list, status filter, detail | API/Web | yes | W | PASS | web/… (W10) | |
| WEB-09 | Technical logs filter | API/Web | yes | W | PASS | web/… (W11) | |
| WEB-10 | Diagnostics form | API/Web | yes | W | PASS | web/… (W12) | |
| WEB-11 | Provider unavailable shown, recovery detected | API/Web | yes | W | PASS | web/… (W16) | |
| WEB-12 | Artifact download from the web console | API/Web | no | — | NOT_APPLICABLE | — | artifacts go to Agents over gRPC |
| SEC-01 | SHA-1 SSH algorithms off by default | Security | yes | U, D | PASS | cisco/r1-executor-security.txt (S1), cisco/r3-… (X1) | |
| SEC-02 | Per-device SHA-1 exception, nothing global | Security | yes | U, D | PASS | cisco/r1-… (S3), cisco/r3-… (X2) | |
| SEC-03 | Exception revocation (removed from config → refused) | Security | yes | U | PASS | ssh_policy_test.go | device run shows default-deny (X1) |
| SEC-04 | No secrets in repository or history | Security | — | I | PASS | 04_SECURITY_REPORT.md | value scan of all lab secrets |
| SEC-05 | Dependency vulnerabilities | Security | — | I | PASS | unit/final-*/vuln.txt | govulncheck: 0 |
| SEC-06 | DHCP/TFTP integrity | Security | n/a | — | NOT_APPLICABLE | 04_SECURITY_REPORT.md | protocol has none; mitigations documented |
| CI-01 | Hosted CI (Linux, Windows) on final commit | CI | yes | I | PASS | ci/hosted-ci.txt | run 44667749894 |
| CI-02 | GNS3 tests as a separate CI category | CI | no | — | NOT_RUN | — | needs a lab runner; workflow edits need workflow scope |

## Counts (computed from the table above, 2026-11-22)

- Features inventoried: 94
- Implemented (fully or partly): 76
- Tested (at least one executed test level): 82
- BLOCKED: 2 · NOT_APPLICABLE: 2 · NOT_RUN: 4 · PARTIAL: 2 · PASS: 77 · UNSUPPORTED: 7

Counts of 2026-11-20 (88 features): BLOCKED 2 · NOT_APPLICABLE 2 · NOT_RUN 6 · PARTIAL 3 · PASS 69 · UNSUPPORTED 6.
