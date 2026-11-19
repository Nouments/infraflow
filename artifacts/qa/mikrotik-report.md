# MikroTik RouterOS — Auto Import on a real CHR 7.8 (2026-11-19)

Device: GNS3 qemu MikroTik CHR, `/system resource print`: `version: 7.8 (stable)`,
`board-name: CHR`, `architecture-name: x86_64`, package `routeros 7.8`;
ether1 MAC `0c:27:a8:1f:00:00`. RouterOS 6 is not available locally.

## Defects found by the run and fixed

| Defect | Effect | Fix |
|---|---|---|
| Template metadata declares the train `7.x`, but the matcher accepted only the literal string `7.x` | Every real RouterOS release (`7.8`, `7.20.1` from `examples/site-provisioning-methods.yaml`) failed generation with "no auto-rsc template supports routeros version" | `matchesReleaseTrain`: a trailing `.x` after a numeric major covers dotted numeric releases with that major only (`7.x` ⊇ `7.8`, not `17.8`, `6.49.10`, `7.8beta1`) |
| Auto Import accepted only RB750Gr3 | A CHR could not be described exactly | New exact `CHR` model profile + versioned template `infraflow.mikrotik.routeros7.chr.auto-rsc` |

## Results

| Stage | Result | Evidence |
|---|---|---|
| Generation (`vendors-lab.yaml`, `mt1`, CHR 7.8, `auto-rsc`) | PASS | `device-mt1.auto.rsc` sha256 `e380341c…a51b` ([file](evidence/mikrotik-chr/device-mt1.auto.rsc)) |
| InfraFlow DHCP → RouterOS `dhcp-client` | PASS — DISCOVER/OFFER/ACK 192.0.2.1 | [chr-dhcp-lease-from-infraflow.txt](evidence/mikrotik-chr/chr-dhcp-lease-from-infraflow.txt) |
| Delivery: FTP upload of `mt1.auto.rsc` | PASS (226, 2270 bytes) — **performed by the lab harness (`curl -T ftp://`), not by InfraFlow** | lab log |
| Device execution (`mt1.auto.log`) | PASS — `Script file loaded and executed successfully` | lab log |
| Applied state | PASS — identity `mt1`; `192.0.2.21/24` ether1 and `198.51.100.129/25` ether2 with owned comments; static route `203.0.113.0/24 → 192.0.2.2` **active**; OSPF instance `infraflow-v2` router-id 192.0.2.21, area `0.0.0.0`, two interface templates | [mt1-postcheck.txt](evidence/mikrotik-chr/mt1-postcheck.txt) |
| Data plane | PASS — lab → `192.0.2.21` ping; CHR → `203.0.113.1` 3/3 via the generated route | [mt1-idempotency.txt](evidence/mikrotik-chr/mt1-idempotency.txt) |
| Idempotency (re-upload identical script) | PASS — still 2 addresses / 1 route / 2 OSPF templates / 1 instance | same file |
| Persistence (`/system reboot`) | PASS — identical counts and identity after reboot | [mt1-persistence.txt](evidence/mikrotik-chr/mt1-persistence.txt) |
| OSPF neighbor formation | Not tested (no OSPF peer in topology) | — |
| VLAN | Not tested — the generator has no VLAN feature for RouterOS | — |
| RouterOS API / REST | Not tested — no InfraFlow client exists | — |

## Status

`auto-rsc` for CHR 7.8 stays **EXPERIMENTAL**: the script is verified on a real
RouterOS, but InfraFlow does not upload it or read `.auto.log`; an operator or
external tool must perform the transfer. Netinstall stays UNSUPPORTED
(destructive). The `native-config` RouterOS renderer (non-idempotent `add`
commands) was not run on the device.
