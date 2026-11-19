# Cisco 7200 AutoInstall — real device validation (2026-11-19)

## Result

**PASS.** A Cisco 7200 router started with empty NVRAM, obtained its address
from the InfraFlow Agent's DHCP service, downloaded its configuration from the
Agent's TFTP service, applied it, and passed a read-only post-configuration
check. The Provider's diagnostics API correlated the complete lifecycle.

| Item | Value |
|---|---|
| GNS3 | 3.1.0a4, local, one new isolated project `infraflow-ztp-c7200-20261119` |
| Device | Dynamips `c7200`, NPE-400, slot0 C7200-IO-2FE, MAC `ca01.0000.0001` |
| Software reported by the device | **Cisco IOS 12.4(24)T5** (`C7200-ADVENTERPRISEK9-M`) |
| Image file name in GNS3 | `c7200-adventerprisek9-mz.153-3.XB12.image` (name does not match the running version) |
| Mechanism | Cisco IOS AutoInstall: DHCP (reservation, siaddr, option 66/150) + TFTP `network-confg` → `edge1-confg` |
| Applied configuration | `hostname edge1`; `FastEthernet0/0` `192.0.2.10/24`, no shutdown |
| Provider ↔ Agent | Separate Docker appliances built from this source tree; HTTPS API and TLS gRPC with a lab CA; site-scoped Agent credential |

## Lifecycle evidence

1. **Generation** — `ztp-lab.yaml` rendered `network-confg` (25 bytes) and
   `edge1-confg` (93 bytes) plus DHCP/TFTP configs.
2. **Publication and synchronization** — the Agent synchronized 17/17 artifacts
   over TLS gRPC. Published and staged bootstrap files have identical SHA-256
   ([published](../../artifacts/delivery/gns3-ztp/published-bootstrap-sha256.txt),
   [staged](../../artifacts/delivery/gns3-ztp/agent-staged-bootstrap-sha256.txt)).
3. **DHCP** — DISCOVER → OFFER → ACK `192.0.2.10` for `ca:01:00:00:00:01`
   ([Agent events](../../artifacts/delivery/gns3-ztp/agent-bootstrap-events.jsonl),
   [packet capture summary](../../artifacts/delivery/gns3-ztp/bootstrap-packets.txt)).
4. **TFTP** — RRQ and completed transfer of `network-confg` and `edge1-confg`
   to `192.0.2.10`.
5. **Device** — console: `Loading network-confg … [OK - 25 bytes]`,
   `Configuration mapped ip address 192.0.2.10 to edge1`,
   `Loading edge1-confg … [OK - 93 bytes]`
   ([console capture](../../artifacts/delivery/gns3-ztp/edge1-console-autoinstall.txt)).
6. **Post-check (read-only `show` commands)** — `hostname edge1`;
   `ip address 192.0.2.10 255.255.255.0` on FastEthernet0/0;
   `show ip interface brief`: `192.0.2.10 YES TFTP up up`; Agent → device ping 3/3.
7. **Reporting** — `infraflow-agent verify-device` read the console capture and
   synchronized `device.config.applied` and `device.postcheck.passed` to the
   Provider. `GET /api/v1/diagnostics?agent_id=agent-ztp&device_id=edge1`
   returned `observed_status: success` with every stage confirmed
   ([report](../../artifacts/delivery/gns3-ztp/provider-diagnostic-edge1.json)).
   Confidence is `medium` because run-level Agent events do not carry
   `device_id`; the engine states this rather than claiming full correlation.

## Defects found by the real run and fixed

| Defect | Effect before the fix | Fix |
|---|---|---|
| DHCP server bound to the unicast service address | Broadcast DISCOVERs (73 captured) were never received; no client could ever be served | Bind `0.0.0.0:67` on the selected interface only |
| DHCP replies sent to the request source `0.0.0.0:68` | An unaddressed client could not receive OFFER/ACK | RFC 2131 §4.1 destination: relay, ciaddr, or broadcast |
| AutoInstall artifact types missing from the Agent processor | Every AutoInstall catalog was reported as a failed run (blocked) | Staged like other verified bootstrap files |
| No address outside the DHCP pools for the Agent service | Generated configs for sites without a gateway were unservable | `bootstrap.service_address`, reserved and validated |
| Profile version taken from the image file name | Only `15.3(3)XB12` accepted; the device runs `12.4(24)T5` | `12.4(24)T5` is LAB-VERIFIED; `15.3(3)XB12` stays accepted but unverified |

## Limits

- One vendor, model and version only. MikroTik RouterOS and FortiOS bootstrap
  were not run on devices.
- AutoInstall applies to running-config; `show startup-config` was empty, so the
  configuration does not survive a reload unless saved.
- The rendered template has no SSH/management-plane hardening.
- The console capture and the read-only `show` commands were driven by the lab
  harness (GNS3 telnet console), not by an InfraFlow feature. InfraFlow never
  sends commands to the device in this workflow.
- GNS3 binds device consoles to `0.0.0.0` and uses default credentials; those
  are host settings outside this repository and were not changed.

## Cleanup

Only resources created by this test were stopped. The project was saved and
closed. Tokens were passed through process environments only and are not
stored in the project or in the evidence files. The lab image
`infraflow-ztp-lab:20261119` remains in the local Docker cache.
