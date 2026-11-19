# Security review — 2026-11-19

Scope: changes made today plus controls exercised against real devices. This
is an engineering review, not a certification. Prior review:
[SECURITY_AUDIT.md](../../docs/DELIVERY/SECURITY_AUDIT.md).

## Findings fixed

| ID | Severity | Finding | Fix | Verification |
|---|---|---|---|---|
| S-1 | Medium | `KnownHostsFileVerifier` parsed `known_hosts` once at Provider start. Removing or rotating a trusted host key had no effect until restart: a revoked (e.g. compromised-router) key stayed trusted. | The file is re-read on every verification; an unreadable file fails closed. Mismatch errors now name both SHA-256 fingerprints. | `TestKnownHostsFileVerifierAppliesFileChangesWithoutRestart`; real IOSv: key swapped while running → `host key mismatch … expected ssh-rsa SHA256:JeM/…, presented ssh-rsa SHA256:nHU9…` |
| S-2 | Low | The lab harness defaulted GNS3 credentials to `admin/admin` in code. | `GNS3_USER`/`GNS3_PASSWORD` are required from the environment. | Code review |

## Controls exercised on a real device (IOSv)

| Control | Result |
|---|---|
| Host-key pinning via known_hosts, verified out of band from the console | Unknown/changed key fails closed before authentication |
| Credentials only in Provider process environment | Never present in task parameters, results, events, or logs (grep of evidence for the lab secrets: no match) |
| Inventory-based target authorization | Device absent from published inventory refused before any connection |
| Idempotency keys | Replay returns the same execution; reuse with another body → 409 |
| No automatic retries for device mutation | Enforced (`max_attempts: 1`) |
| startup-config persistence disabled | Confirmed on device |
| Provisioning services isolation | DHCP/TFTP bound to the lab container interface on an isolated GNS3 switch; nothing listening on host UDP 67/69 |

## Scans and gates

- `govulncheck ./...` (Go 1.27.2 local): 0 reachable vulnerabilities; required
  modules contain 3 unreachable findings.
- `go vet`, `go test -race` across all packages: pass.
- Secret scan of committed evidence against the three generated lab secrets and
  appliance serials: no match (raw console logs that echo secrets were not
  committed).

## Residual risks (not fixed)

| Risk | Note |
|---|---|
| IOSv 15.9 only offers SHA-1 key exchange; InfraFlow's Go SSH client accepts `diffie-hellman-group14-sha1` | Needed for this IOS release; there is no per-device KEX policy to forbid SHA-1 for modern targets. |
| AutoInstall/TFTP/FTP are cleartext and unauthenticated by protocol | Must stay on isolated provisioning segments. |
| The executor inventory (`inventory.json`) is an unsigned authorization source | Documented in `docs/cisco-ios-executor.md`. |
| GNS3 host uses default credentials and binds consoles on `0.0.0.0` | Host setting outside this repository; not changed. |
| Diagnostics keyword rules over free-form console text can misclassify interactive sessions (operator typos read as config rejection) | Documented; feed provisioning-only captures. |
| Shared Agent token remains supported alongside scoped credentials | Unchanged from prior review. |
