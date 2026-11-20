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
