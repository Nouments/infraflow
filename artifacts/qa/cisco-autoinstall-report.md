# Cisco IOS — AutoInstall, TFTP delivery and SSH executor on a real IOSv (2026-11-19)

Device: GNS3 qemu **Cisco IOSv**, `show version`: *Cisco IOS Software, IOSv
Software (VIOS-ADVENTERPRISEK9-M), Version 15.9(3)M6, RELEASE SOFTWARE (fc1)*,
4× GigabitEthernet, Gi0/0 MAC `0c:eb:25:a0:00:00`. Project
`infraflow-e2e-20261119`, isolated switch, lab host `192.0.2.2`.
Previous evidence for the Dynamips 7200 (12.4(24)T5) AutoInstall PASS is in
[GNS3_ZTP_AUTOINSTALL.md](../../docs/DELIVERY/GNS3_ZTP_AUTOINSTALL.md) and was
not re-run.

## Results by stage

| Stage | Result | Evidence |
|---|---|---|
| Generation (`generate-all`, site `iosv-lab`, model `iosv` 15.9(3)M6) | PASS — new exact IOSv profile/template | `r1-confg` sha256 `200767a2…4f39`, `network-confg` sha256 `01d7026d…a2ae` (published = Agent-staged) |
| Provider → Agent sync (gRPC, token auth) | PASS — all artifacts completed, report persisted | Agent run log in lab |
| DHCP with **no server** (failure scenario) | As expected: 7 DISCOVERs in 45 s, no OFFER | [dhcp-unavailable-capture.txt](evidence/cisco-iosv/dhcp-unavailable-capture.txt) |
| DHCP-triggered **AutoInstall** | **FAIL (appliance)** — InfraFlow sent RFC-correct OFFERs (yiaddr .11, siaddr, option 54/66/150, echoed client-ID); the IOSv AutoInstall/PnP client never sent REQUEST | [dhcp-autoinstall-pcap-summary.txt](evidence/cisco-iosv/dhcp-autoinstall-pcap-summary.txt): 180 Discover, 103 Offer, 0 Requests from the AutoInstall client |
| A/B with reference ISC dhcpd 4.4.1 (same segment, same reservation) | Same behavior: DISCOVER → OFFER, never REQUEST | console run log |
| Offer variants (no hostname, no client-ID echo, router option, bootfile, PnP option 43, all combined) | None produced a REQUEST | lab cycle log |
| Standard IOS DHCP client (`ip address dhcp`) against InfraFlow | PASS — Discover/Offer/Request/Ack, `192.0.2.11 YES DHCP up up` | `debug dhcp detail` transcript |
| **TFTP delivery** of the generated config (`copy tftp://192.0.2.2/r1-confg running-config`) | PASS — `[OK - 221 bytes]`, `Configured from tftp://192.0.2.2/r1-confg` | [r1-provisioning-console.txt](evidence/cisco-iosv/r1-provisioning-console.txt), [tftp-pcap-summary.txt](evidence/cisco-iosv/tftp-pcap-summary.txt) |
| Configuration applied | PASS — `hostname r1`, Gi0/0 `192.0.2.11/24` (Method TFTP), Gi0/1 `198.51.100.1/24`, `ip route 203.0.113.0/24 192.0.2.2` | [r1-postcheck.txt](evidence/cisco-iosv/r1-postcheck.txt) |
| Routing data plane | PASS — `ping 203.0.113.1` 5/5 via the generated static route; CEF next hop 192.0.2.2 Gi0/0 | same file |
| Persistence | Not persisted (by design): `show startup-config` has no hostname | same file |

### Why IOSv AutoInstall is classified as an appliance limitation, not an InfraFlow defect

1. The same OFFER bytes are accepted immediately by the device's regular DHCP client.
2. A reference ISC dhcpd shows the identical DISCOVER → OFFER → no-REQUEST pattern.
3. The device's PnP tech summary shows `PnP Auto-Install Service: Run Count 1 … Elapsed 346655 ms`
   and `PnP DHCP Discovery Service: Never Run`; boot logs `%C3600-3-NOMAC: Can't allocate MAC address for interface 0/0`.

The IOSv profile is therefore **PARTIAL** (not usable as zero-touch): the
rendered configuration and InfraFlow TFTP delivery are verified, the
DHCP-triggered step is not.

### Other appliance finding

The GNS3 IOSv template attaches `IOSv_startup_config.img` (CVAC). It applied a
banner/line config **and saved it to NVRAM**, which makes PnP stop with
"Startup Config Present". AutoInstall testing requires removing that disk and
`write erase`.

## Cisco SSH executor (`cisco.ios.configure`) — first real-device run

Provider `serve` with `known_hosts_file`; credentials only in the Provider
process environment (`INFRAFLOW_DEVICE_IOSV_LAB_R1_SSH_*`). Host key verified out
of band: the RSA key printed on the console (`show crypto key mypubkey rsa`)
was converted to `SHA256:nHU9UOg1dy2bKj3eKGStHe8yZj3d7Q1Yr6sj86QuNNY`, identical
to the key served on TCP/22.

| Test | Result |
|---|---|
| Go `x/crypto/ssh` vs IOSv 15.9 SHA-1-only KEX | PASS — handshake negotiates (OpenSSH 10 refuses by default) |
| Apply hostname + Gi0/2 description/address/admin-up | PASS — `verified: true`, preflight captured previous state, read-back matched ([executor-success-execution.json](evidence/cisco-iosv/executor-success-execution.json)) |
| Independent console check | PASS — `Configured from console by infraflow on vty0 (192.0.2.2)`; startup-config untouched ([r1-executor-console-check.txt](evidence/cisco-iosv/r1-executor-console-check.txt)) |
| Wrong password | FAIL closed: `unable to authenticate, attempted methods [none password]` |
| Tampered `known_hosts` (Provider restarted) | FAIL closed: `host key mismatch (possible man-in-the-middle attack)` |
| Tampered `known_hosts` **while running** | **Defect found** — accepted (key cached at startup). Fixed; now fails closed with both fingerprints ([executor-negative-tests.txt](evidence/cisco-iosv/executor-negative-tests.txt)) |
| Device not in the published inventory (`r9`) | Refused before any connection: `target device is not authorized or not found` |
| Hostname-only request | **Defect found** — rejected with generic "task parameters failed validation". Fixed: now names the task and the reason |
| Idempotency: same key + body twice; same key + other body | 202 then 200 with the same execution ID; 409 `idempotency key was used for a different request` ([executor-restore-idempotency.txt](evidence/cisco-iosv/executor-restore-idempotency.txt)) |
| Restore (hostname back, Gi0/2 admin down) | PASS, `verified: true` |

## Cisco ZTP

Not tested. IOS-XE ZTP (Python/guestshell over HTTP(S)) is not supported by
IOSv, the 7200, or (officially) CSR1000v 16.7; InfraFlow's generator blocks the
`cisco-ztp` method by design. AutoInstall results are **not** evidence for ZTP.

## Not tested

CSR1000v 16.7 and IOL-XE 17.18 IOS-XE AutoInstall (time/RAM budget);
TFTP-unavailable, invalid-config and interrupted-transfer scenarios on the
device (covered only by unit tests in `agent/internal/delivery/tftpserver` and
`internal/diagnostics`).

## Reproduce

See [gns3-integration-report.md](gns3-integration-report.md#reproduce).
