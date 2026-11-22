# 07 — Final delivery report

- Branch `develop`; final code commit `aa79e52`; this report set is committed on top of it (documentation only).
- Score: **85/100** (baseline 76/100, same rubric and weights) — breakdown in docs/DELIVERY/RELEASE_READINESS.md.
- Not a release approval: two appliances are BLOCKED and IOSv AutoInstall is PARTIAL.

## Features (05_FEATURE_TRACEABILITY.md)

| Inventoried | Implemented | Tested | PASS | FAIL | PARTIAL | BLOCKED | NOT_RUN | UNSUPPORTED | N/A |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 88 | 71 | 73 | 69 | 0 | 3 | 2 | 6 | 6 | 2 |

## Defects

Fixed: 14 (D0, D-SITE, D-ROLL, D-LOCK, D-SHA1, D1–D8) — each with a regression test (06_FAILURES_AND_BLOCKERS.md).
Remaining open defects in InfraFlow code: none known. Open items: IOSv AutoInstall root cause (PARTIAL),
FortiGate and IOL-XE (BLOCKED), and the UNSUPPORTED features listed in 05.

## Devices tested (GNS3, isolated project)

Cisco 7200 12.4(24)T5 · Cisco IOSv 15.9(3)M6 · Cisco IOSv 15.9(3)M8 · Cisco CSR1000v IOS XE 16.07.01 ·
MikroTik CHR 7.8 (two nodes) · FortiGate VM 7.0.8 (login only, BLOCKED).

## CI

GitHub Actions "Go CI" — linux, windows, presentation-demo: **success** on the final code commit `aa79e52`
(run 44667749894) and on `37a1d7c` (run 44667859035). The previous failure on `65fe758` (Windows) was defect D8.
Evidence: evidence/ci/hosted-ci.txt. There is no GNS3 job in hosted CI (CI-02 NOT_RUN).

## Reproduce

```sh
scripts/qa/gate.sh /tmp/gate            # 13 local gates incl. race, govulncheck, browser E2E
go build -o /tmp/infraflow-provider ./provider/cmd
node scripts/qa/web_e2e.mjs /tmp/infraflow-provider artifacts/finalization/evidence/gns3/inputs/lab-all.yaml /tmp/web
go run ./agent/cmd diagnose -device-id r1 -capture <file.pcap> -client-mac <mac>
# GNS3 scenarios: scripts/lab/e2e.py (needs a GNS3 server; credentials via environment only)
sha256sum -c artifacts/finalization/evidence/SHA256SUMS   # run from artifacts/finalization/evidence
```

## Cleanup done

Lab nodes stopped and the lab project closed (nothing deleted); lab secret files, exported host keys and packet
captures deleted from the scratch area and from the lab container; no pre-existing project, appliance or host
network setting was changed.

## Addendum — 2026-11-22

Base `c1917ac` (clean at start), branch `develop`. All changes below are **uncommitted**; no commit or push was made.
Score **87/100** on the unchanged rubric (was 85) — see docs/DELIVERY/RELEASE_READINESS.md.

Implemented:
- AGT-11: Agent report outcome against the current catalog (NO_REPORT / INCOMPLETE / STALE / FAILED / COMPLETE), shown in the Admin Web; rejected reports recorded as `agent.report.rejected` events (D11).
- AGT-10: integration tests with the real gRPC client, on-disk store and a TCP connection cut mid-stream, plus Agent restart. No code change was needed: staging, hashing and atomic rename already held. Mutation-checked.
- PRV-19: `scripts/qa/load` (real Provider, 4 stages, concurrent generation, SIGKILL recovery). Two defects found by CPU profile and fixed (D9, D10): at concurrency 64, 125 → 1 859 req/s, p99 1 188 → 131 ms, 0 errors.
- GNS3: `internal/gns3` Controller client and `infraflow-provider gns3 inventory|host-check|project-ensure|topology-apply|start|stop|snapshot|report-check`; reproducible topology; JSON scenario reports with validation (D12 found on the real server).
- DIA-09: reference DHCP comparison executed; IOSv M6 AutoInstall reclassified UNSUPPORTED (image-side), with evidence.

Counts (05): 94 features — PASS 77, PARTIAL 2, BLOCKED 2, NOT_RUN 4, UNSUPPORTED 7, NOT_APPLICABLE 2.
Gate: `scripts/qa/gate.sh` 13/13 PASS on the final tree (see below). Hosted CI does not cover these uncommitted changes.
Evidence: evidence/gns3-20261122/ (SHA256SUMS inside), docs/DELIVERY/GNS3_VALIDATION_20261122.md.
