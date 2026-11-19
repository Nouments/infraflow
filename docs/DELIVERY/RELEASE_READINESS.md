# Release readiness

## Delivery evidence score: 86/100 (84/100 until hosted CI confirms)

This score summarizes tested delivery evidence; it is not a security
certification or a release approval. Previous score: 74/100.

| Area | Before | Now | Reason |
|---|---:|---:|---|
| Provider–Agent flow | 19/20 | 19/20 | Real appliance runs over TLS; Agent now heartbeats during runs, announces shutdown, and authenticates over HTTP with its site-scoped credential. No persistent session channel exists, so liveness is heartbeat-based only. |
| Agent state and integrity | 18/20 | 19/20 | Agent status is evidence-based (online/degraded/offline/unknown), survives Provider restarts truthfully, and was verified with real process kills and restarts. Hash mismatches are no longer collapsed into "download failed". AutoInstall artifact types are no longer blocked. |
| Vendor/device compatibility | 5/15 | 11/15 | One complete AutoInstall workflow applied and verified on a virtual Cisco 7200 (IOS 12.4(24)T5): DHCP, TFTP, applied hostname/addressing, read-only post-check, Provider-correlated diagnosis. MikroTik and FortiOS remain unverified on devices; no SSH in the template; config not saved to NVRAM. |
| Security/operations | 9/15 | 12/15 | Scoped HTTP Agent credentials stop cross-site heartbeat/disconnect/log spoofing; bare bearer tokens are now redacted; leaked SSE goroutines (permanent 503) fixed; central log store compacts instead of blocking Provider startup; diagnostics never act on devices. GNS3 host settings (default credentials, wildcard console bind) are unchanged. |
| QA/platforms | 14/20 | 16/20 | CI workflow restored with an asserted Go 1.26.11 toolchain; the full Linux gate (govulncheck, test, race, vet, build, coverage, gofmt, diff) passes locally under exactly Go 1.26.11. Real browser, offline (no-route namespace), lifecycle, and GNS3 acceptance runs were executed. No hosted GitHub Actions run covers this revision, and Windows is unverified (counted as 14/20 → 84/100 until it runs). |
| Delivery evidence | 9/10 | 9/10 | Sanitized evidence for lifecycle, offline, and AutoInstall runs. |
| **Total** | **74** | **86** | |

## Gates before broader use

- Push the revision and obtain hosted Linux and Windows CI results.
- Rotate the GNS3 session token exposed earlier, replace default GNS3
  credentials, and restrict console binding (host settings, outside this repo).
- Validate MikroTik RouterOS and FortiOS bootstrap on real or virtual devices
  before claiming support; add SSH/management hardening to rendered configs.
- Deployments that still use only the shared Agent token should move to
  `agent_credentials` so every Agent identity operation is site-scoped.

## Evidence

- Agent status lifecycle: [agent-status-lifecycle.txt](../../artifacts/delivery/agent-status-lifecycle.txt)
- Offline acceptance: [offline-acceptance.txt](../../artifacts/delivery/offline-acceptance.txt)
- AutoInstall: [GNS3_ZTP_AUTOINSTALL.md](GNS3_ZTP_AUTOINSTALL.md)
