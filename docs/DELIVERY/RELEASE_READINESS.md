# Release readiness

## Score: 76/100 (2026-11-19, evidence-based)

This revision uses the eight-dimension rubric below. The previous figure (86)
used a different six-area rubric and is **not** comparable; it also credited a
CI workflow that the commit after it deleted. This is not a release approval.

| Dimension | Weight | Score | Justification (evidence) |
|---|---:|---:|---|
| Provider correctness and reliability | 15 | 13 | Full gate passes incl. race. First real-device run of the `cisco.ios.configure` executor: apply, preflight, read-back, restore, idempotency replay (202/200 same ID, 409 on body change). Fixed: generic validation rejection now carries the reason. −2: hosted CI result pending at writing; no load/soak testing. |
| Agent reliability and lifecycle | 15 | 13 | gRPC sync, hash-verified staging and report against the Provider in the lab; DHCP/TFTP services served real IOSv and RouterOS clients; prior session verified heartbeat/offline/restart. −2: no reconnection test against a real device run today; Agent has no device delivery for non-Cisco vendors. |
| Provisioning and vendor compatibility | 20 | 11 | Cisco 7200 AutoInstall end-to-end (prior run). IOSv: generated config delivered by InfraFlow TFTP and applied, routing verified; DHCP-triggered AutoInstall **does not complete on this appliance** (reproduced with ISC dhcpd) → PARTIAL. RouterOS CHR 7.8: script verified on device (state, routing, idempotency, persistence) but upload was done by the harness → EXPERIMENTAL. FortiOS: rendering only (login blocked). Cisco ZTP and IOS-XE AutoInstall untested. |
| Real GNS3 integration tests | 15 | 11 | 13 real scenarios on IOSv, CHR and FortiGate VM in an isolated project, with captures. −4: FortiGate blocked; CSR1000v/IOL-XE not run; scenarios are harness-driven, not an automated suite. |
| Admin Web and API functionality | 10 | 8 | Execution, auth and idempotency APIs exercised for real; Admin Web scripts pass syntax/unit checks; browser test from prior session. −2: no browser run of today's tree. |
| Diagnostics and observability | 10 | 8 | Engine run on real evidence: two defects fixed (plain service lines ignored; local flash "File not found" misread as TFTP failure) and a new `DHCP_OFFER_NOT_ACCEPTED` diagnosis. Conservative when evidence is incomplete. −2: console evidence must be supplied manually; interactive captures can still misclassify. |
| Security | 10 | 8 | Fixed host-key revocation requiring restart (S-1); fail-closed host key, inventory authorization and secret handling verified on a device; 0 reachable vulns. −2: SHA-1 KEX accepted globally; cleartext provisioning protocols; default GNS3 host credentials (outside repo). |
| Documentation and delivery evidence | 5 | 4 | Reports with sanitized evidence and hashes under `artifacts/`. −1: hosted CI evidence not yet attached. |
| **Total** | **100** | **76** | |

## What would raise the score (in order of value)

1. FortiGate access (password for the image or a fresh FortiOS 7.0.x VM) → apply and verify `fortios-native` output.
2. InfraFlow-driven RouterOS delivery (SFTP upload + `.auto.log` read-back) so the verified script becomes a zero-touch path.
3. IOS-XE AutoInstall on CSR1000v 16.7 or IOL-XE 17.18; a Cisco target whose AutoInstall client completes for a second model.
4. A hosted CI run on this revision (Linux + Windows).
5. Turning `scripts/lab/` scenarios into a repeatable automated GNS3 suite.

## Evidence

- [Environment inventory](../../artifacts/audit/environment-inventory.md) · [Architecture review](../../artifacts/audit/architecture-review.md)
- [Test matrix](../../artifacts/qa/test-matrix.md) · [GNS3 integration](../../artifacts/qa/gns3-integration-report.md)
- [Cisco](../../artifacts/qa/cisco-autoinstall-report.md) · [MikroTik](../../artifacts/qa/mikrotik-report.md) · [FortiGate](../../artifacts/qa/fortigate-report.md)
- [Security review](../../artifacts/security/security-review.md)
- Earlier: [GNS3_ZTP_AUTOINSTALL.md](GNS3_ZTP_AUTOINSTALL.md), [agent-status-lifecycle.txt](../../artifacts/delivery/agent-status-lifecycle.txt), [offline-acceptance.txt](../../artifacts/delivery/offline-acceptance.txt)
