# 00 — Baseline audit

Baseline: `c26fa63` on `develop` (== `origin/develop` at start), 2026-11-19, starting score 76/100
(docs/DELIVERY/RELEASE_READINESS.md, same eight-dimension rubric and weights as this mission).

## Environment

- Host: Arch Linux, 8 CPU, 15 GiB RAM; Go 1.27.2 local (`GOTOOLCHAIN=local`, go.mod toolchain 1.26.11); Node 24.21; Chromium/chromedriver 153.
- GNS3 3.1.0a4 at 127.0.0.1:3080 (credentials kept out of the repository). Isolated project `infraflow-e2e-20261119`:
  lab host (Debian 12 Docker node: Provider, Agents, DHCP/TFTP), switch SW1, Cisco 7200 (R2), IOSv 15.9(3)M6 (R1),
  MikroTik CHR 7.8 (MT1, later MT2), FortiGate VM 7.0.8 (FGT1); added this session: CSR1000v 16.7 (CSR1), IOSv 15.9(3)M8 (IOSVM8).
  Addressing 192.0.2.0/24 (TEST-NET-1), no uplink to the user's networks.
- Hosted CI: run 44569729063 on `c26fa63` — success (Linux, Windows, presentation-demo).

## Baseline QA gate (`scripts/qa/gate.sh`, evidence/unit/baseline-c26fa63/summary.txt)

gofmt, vet, test, race, build, govulncheck, coverage thresholds, web syntax/unit, shell test, secrets grep,
file modes, diff-check: all PASS.

## Baseline gaps (as found, before this mission's changes)

| Area | Gap |
|---|---|
| Provider | No rollback after a partial apply (only "state uncertain"); no cross-execution device lock (scheduler lock is per execution); SHA-1 SSH KEX/MAC/host-key accepted for every device. |
| Agent | Agents on one host shared one log outbox; a rejected batch blocked every later run of every Agent. |
| Vendors | RouterOS script had to be uploaded by the harness (no InfraFlow delivery). IOS XE never run on a device. FortiGate login unknown. |
| Generation | A Cisco IOS device managed over SSH made its whole site fail to generate. |
| Diagnostics | Server logs only: no client REQUEST/DECLINE/RELEASE visibility, so REQUEST-without-ACK could not be told apart. |
| Web | No browser test of the current tree. |
| CI | No GNS3 category; workflow edits need a token with `workflow` scope. |
| Reports | No finalization report set. |
