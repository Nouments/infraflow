# 08 — Changelog (c26fa63 → final, branch develop)

- `9932eca` feat(provider): SSH algorithm policy, verified rollback, device locks, RouterOS import executor
- `5a180f5` fix(agent): per-Agent log outbox; never send other identities' events
- `6edeb5c` fix(generation): allow provisioning.method ssh for Cisco IOS devices
- `2cf5d8d` test(lab): GNS3 e2e driver, QA gate script and finalization evidence
- `30711a1` fix(generation): RouterOS auto-rsc adopts identical unowned entries
- `2f35012` fix(provider): prove RouterOS import success with a completion marker
- `a1e0bd5` test(lab): IOSv executor, CHR RouterOS import evidence
- `dd7b140` feat(diagnostics): packet-capture input for DHCP/TFTP wire states
- `c81a6ba` test(lab): diagnose real IOSv and c7200 bootstrap captures
- `93a7ebc` fix(provider): web plan-and-generate uses the native generator's support check
- `605a09b` test(web): headless Chromium E2E suite for the Provider console
- `431fd17` test(lab): generation determinism/traceability and OSPF adjacency evidence
- `1c20d22` fix(provider): never send "end" at an EXEC prompt (IOS XE rollback)
- `fb66054` test(lab): Cisco CSR1000v IOS XE executor evidence (apply, replay, rollback)
- `65fe758` stabilization14 && finalisation
- `93bae62` test(lab): IOSvm8 AutoInstall capture, IOSv regression after D5, web suite update
- `88e6207` chore: stop tracking Python bytecode from scripts/lab
- `aa79e52` test(provider): make the RouterOS unreachable-device test OS-independent

`65fe758` was committed by the repository owner (evidence update plus two .pyc files, untracked again in `88e6207`).
The report commit that follows these is documentation only.
