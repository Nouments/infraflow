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
Gate: `scripts/qa/gate.sh` 13/13 PASS on the final tree. Hosted CI did not cover these uncommitted changes at the time (they were later committed as `c9f5a86`; see the next addendum).
Evidence: evidence/gns3-20261122/ (SHA256SUMS inside), docs/DELIVERY/GNS3_VALIDATION_20261122.md.

## Addendum — 2026-11-22, release validation of `c9f5a86`

Commit under test: `c9f5a8676006f505e711f071bdf89f5056290dfa` (`develop` = `origin/develop`). It contains the
morning's changes (D9–D12, AGT-10/11, GNS3 client). Its commit message says "up to 100/100"; the evidence below does
not support that number.

### Evidence for the exact commit

| Check | Result | Evidence (evidence/release-c9f5a86/) |
|---|---|---|
| `scripts/qa/gate.sh` in a clean detached worktree of `c9f5a86` | 13/13 PASS (gofmt, vet, test, race, build, govulncheck, coverage, web, web-e2e 16/16, shell, secrets, perms, diffcheck); worktree clean before and after | gate/context.txt, gate/*.txt |
| govulncheck | 0 reachable vulnerabilities (3 in required modules, not called) | gate/vuln.txt |
| Hosted CI, run 44993837424 on `c9f5a86` (push) | success: linux (vuln, test, race, vet, build, coverage, web checks, gofmt, diff-check), windows (test, vet, build, gofmt, diff-check), presentation-demo | ci-runs-c9f5a86.json, ci-jobs-44993837424.json |
| GNS3 scenarios on the commit's binaries | 8 PASS, 2 UNSUPPORTED (+ 1 PASS needing an uncommitted patch) | gns3-c9f5a86/*.json, docs/DELIVERY/GNS3_VALIDATION_c9f5a86.md |
| Provider soak, c9f5a86 binary (4 clients, 1 min windows) | **PARTIAL**: stopped at the user's request after 1 h 53 of the planned 3 h (109 windows); 6.43 M requests, 0 errors, open files and threads flat, p99 23.6 → 28.7 ms (no drift); **RSS 575–710 MiB, above the 512 MiB objective** → D13 | soak-c9f5a86.json |
| Short load + SIGKILL recovery, c9f5a86 binary | PASS: 0 errors, p99 at c=32 within objective, recovery 1.68 s with the same 4 sites and the session, 8 parallel generations identical (throughput lower than in the morning because the soak shared the host) | load-report-c9f5a86.json |

Matrix (05) after this validation: 94 features — PASS 78, PARTIAL 2 (GNS-04, PRV-20), BLOCKED 2, NOT_RUN 1 (CI-02), UNSUPPORTED 8, NOT_APPLICABLE 3.
Score of `c9f5a86`: **88/100** (docs/DELIVERY/RELEASE_READINESS.md).

### Uncommitted changes on top of `c9f5a86` (not in the HEAD, not covered by hosted CI)

- D14 / GNS-04: `cisco.ios.configure` gains an explicit `persist: true`. It runs `write memory` only after a
  verified running state, requires `[OK]`, then reads `show startup-config` back and compares every requested
  field. A failed save is reported as FAILED ("NOT persisted") and is not rolled back. It is never implicit.
  Three unit tests, and a real IOSv reload: the persisted change survived; a non-persisted one did not.
- D13: the central log store keeps an index instead of a second copy of every event, and compaction no longer
  copies the whole store once more. On the same 57 MiB store, live heap fell from 233 to 133 MiB. Test
  mutation-checked. Comparison soak, 42 min, same parameters: PASS, 1.69 M requests, 0 errors, RSS peak 499 MiB (≤ 512, small margin) — soak-worktree-d13-fix-42min.json. Shorter than the HEAD soak, so it is indicative, not equivalent.
- `scripts/qa/load -soak`: multi-hour mode (per-window JSON report, drift and leak objectives).
- Topologies `infraflow-automated-validation-devices.json` and `-csr.json`.
- Working-tree gate: 12/13 PASS. web-e2e failed when Chrome's renderer died under host memory pressure; re-run
  alone, it gave 16/16 PASS. Both runs are kept in release-c9f5a86/gate-worktree/.

### Not done, and why

- CSR1000v (CIS-05) not re-run. `gns3 host-check` refused it (4096 MiB plus 1 GiB reserve, 5005 MiB available),
  and the reserve was not lowered. Executor production code is unchanged since its 2026-11-20 PASS.
- FortiGate: BLOCKED, no password guessed. IOL-XE: BLOCKED, image absent.
- VLANs (MTK-08), routes/NAT from the web (GEN-06), user deletion (WEB-06): re-evaluated, still UNSUPPORTED.
  - VLANs need a model field, per-vendor templates and executor read-back.
  - Routes/NAT need a web planner.
  - User deletion needs an audited delete with a last-administrator guard.
  - None was implemented in this validation pass.
- GNS3 view in the Admin Web: NOT_APPLICABLE. The Provider never contacts GNS3, by design.

Nothing was committed or pushed.
