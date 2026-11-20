# 02 — Test execution report

Every result below was executed during this mission; nothing is carried over from earlier sessions unless
marked "prior". Raw outputs are under `evidence/`.

## Automated suites (final tree)

| Suite | Command | Result | Evidence |
|---|---|---|---|
| QA gate (13 gates) | `scripts/qa/gate.sh <dir>` | 13/13 PASS | unit/final-93bae62/summary.txt |
| Unit + integration | `go test -count=1 ./...` | PASS | gate `test` |
| Race | `go test -race -count=1 ./...` | PASS | gate `race` |
| Vulnerabilities | `govulncheck ./...` | 0 affecting | gate `vuln` |
| Coverage thresholds | processor ≥ 90 %, CLI ≥ 65 % | PASS | gate `coverage` |
| Capture parser fuzzing | `go test -fuzz FuzzCaptureParser -fuzztime 30s` | PASS, no crash | run log in this session |
| Browser E2E | `node scripts/qa/web_e2e.mjs …` | 16/16 PASS | web/web-e2e.json, screenshots |
| Windows | hosted CI | see 07 | ci/ |

The final tree was rebuilt after `93bae62` only for a test-portability change and a .gitignore change; `go test ./...`
was re-run and passes.

## Real-device scenarios (GNS3)

| Group | Scenarios | PASS | FAIL→fixed | Evidence |
|---|---|---:|---:|---|
| IOSv r1, `cisco.ios.configure` security | S1 default policy, S2 unknown host key, S3 pinned apply | 3 | 0 | cisco/r1-executor-security.txt |
| IOSv r1, rollback & idempotency | R1–R2, I1–I3, regression after D5 | 7 | 0 | cisco/r1-executor-rollback-idempotency.txt |
| IOSv r1, failures | F1 refused, F1b timeout, F2 bad password, F3 unauthorized, F4 wrong vendor, F5 retry policy | 6 | 0 | cisco/r1-executor-failures.txt |
| IOSv r1, concurrency & crash | K1 serialized (router SSH log), K2 SIGKILL → "outcome unknown" | 2 | 0 | cisco/r1-executor-concurrency-restart.txt |
| CSR1000v r3 (IOS XE 16.7) | X1 default refused, X2 apply, X3 re-apply, X4 rollback, X4b/X5b after fix | 5 | 1 (D5) | cisco/r3-csr1000v-iosxe-executor.txt |
| Cisco 7200 r2 | AutoInstall end to end, post-check | 1 | 0 | cisco/r2-c7200-postcheck.txt |
| IOSv M6/M8 AutoInstall | wire capture analysis | PARTIAL | — | captures/…, cisco/iosvm8-autoinstall.txt |
| CHR mt1 (MT2 node), `mikrotik.routeros.import` | M1b–M14 | 10 | 3 runs (D1–D4) | mikrotik/routeros-import-executor.txt |
| OSPF adjacency mt1 ↔ IOSv | FULL, route installed, ping | 1 | 0 | gns3/ospf-adjacency-mt1-r1.txt |
| Agents (two sites, one host) | A1–A5b, outbox regression, reconnection, token expiry | 11 | 1 (D0) | integration/agent-*.txt |
| Generation pipeline | G1–G4 (8 invalid inputs) | 4 | 0 | integration/generation-pipeline.txt |
| Diagnostics on real captures | IOSv phase 1/2, full, c7200, ambiguity | 5 | 0 | captures/diagnose-real-captures.txt |
| FortiGate | console login | BLOCKED | — | fortinet/fgt1-blocked.txt |

Invalid runs are kept and labelled in the evidence (for example M1, which hit a stale ARP entry and was
repeated as M1b).

## Not executed

Load/soak; interrupted Agent sync; incomplete-report handling in the lab; device reload persistence;
IOL-XE (image absent); reference DHCP server comparison; GNS3 tests in hosted CI.
