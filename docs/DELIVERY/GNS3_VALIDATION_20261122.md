# GNS3 validation — 2026-11-22

Base commit `c1917ac` on `develop`, plus the uncommitted changes listed in
[07_FINAL_DELIVERY_REPORT.md](../../artifacts/finalization/07_FINAL_DELIVERY_REPORT.md#addendum--2026-11-22).
Machine-readable reports and their evidence:
[`artifacts/finalization/evidence/gns3-20261122/`](../../artifacts/finalization/evidence/gns3-20261122/)
(`*.json` reports, `logs/`, `captures/`, `SHA256SUMS`). The reports pass
`infraflow-provider gns3 report-check` (schema, statuses, secret scan,
evidence SHA-256).

## Controller

| Item | Observed |
|---|---|
| State at session start | **not running** — nothing listened on 127.0.0.1:3080 although the brief said it was started |
| Action | `gns3server --host 127.0.0.1 --port 3080` (command line only; no persistent setting changed) |
| Version / API | 3.1.0a4, Controller API v3 |
| Authentication | v3 bearer token from `/v3/access/users/authenticate`; HTTP Basic is refused (401) |
| Credentials | `admin`/`admin` — valid. **Risk:** default administrator account; acceptable only while the controller listens on loopback |
| Computes | `local` plus one remote compute (address redacted in evidence) that was unreachable; `/v3/statistics` blocks on it, so host resources are read locally |
| Projects | 10 pre-existing; none changed. One (`infraflow-provisioning-validation-20261119`) has `auto_open` and opened itself at start; only its builtin switch runs |

## Images and templates actually available (non-builtin)

Availability is checked through the controller (`/v3/images`, and
`/v3/computes/local/docker/images` for Docker templates), not assumed.

| Template | Type | Image | Image | RAM MiB |
|---|---|---|---|---:|
| Cisco 3640 | dynamips | c3640-a3js-mz.124-25d.image | present | 192 |
| Cisco 7200 | dynamips | c7200-adventerprisek9-mz.153-3.XB12.image | present | 512 |
| Cisco CSR1000v | qemu | csr1000v-universalk9.16.07.01-serial.qcow2 | present | 4096 |
| Cisco IOL-XE | docker | iol-xe/iol-xe:17-18-02 | **absent** | |
| Cisco IOSv | qemu | vios-adventerprisek9-m.spa.159-3.m6.qcow2 | present | 512 |
| Cisco IOSvL2 | qemu | vios_l2-adventerprisek9-m.ssa.high_iron_20200929.qcow2 | present | 768 |
| Cisco IOSvm8 | qemu | vios-adventerprisek9-m.spa.159-3.m8.qcow2 | present | 512 |
| MikroTik CCR1036-8G-2S | qemu | chr-7.8.img | present | **8192** (template value; exceeds this host's free RAM) |
| MikroTik CRS328-4C-20S-4S | qemu | chr-7.8.img | present | 512 |
| MikroTik RB1100AHx4 Dude Edition | qemu | chr-7.8.img | present | 1024 |
| fortigate | qemu | fortigate-7.0.8-gns3.qcow2 | present | 1258 |
| Mikrotik WinBox | docker | gns3/mikrotik-winbox | **absent** | |
| debian-dhcp | docker | debian-dhcp:latest (ISC dhcpd 4.4.3-P1) | present | |
| infraflow-provider / -agent (dev, delivery-20261118) | docker | | present | |
| 15 other Docker utility templates (debian, nginx, DNS, …) | docker | | present | |

The Cisco 7200 template now points to a 15.3(3)XB12 image; the 2026-11-19
AutoInstall PASS reported IOS 12.4(24)T5. That PASS was not re-run.

## Topology (`scripts/lab/topologies/infraflow-automated-validation.json`)

```
IOSV1 Gi0/0 ──┐
              │   SW-PROV (builtin Ethernet switch, no uplink)
REFDHCP eth0 ─┤   10.99.0.0/24 provisioning segment
              │
PC1 (VPCS) ───┘
```

- `IOSV1` — Cisco IOSv 15.9(3)M6, `hdb_disk_image` removed at creation so it
  boots with an empty NVRAM and no startup-config disk.
- `REFDHCP` — `debian-dhcp` container running a reference ISC dhcpd.
- `PC1` — VPCS control client.
- No Cloud or NAT node: the segment cannot reach the host or other projects.

Commands (credentials only from `INFRAFLOW_GNS3_URL`, `INFRAFLOW_GNS3_USERNAME`,
`INFRAFLOW_GNS3_PASSWORD`):

```sh
go build -o /tmp/ifp ./provider/cmd
T=scripts/lab/topologies/infraflow-automated-validation.json
/tmp/ifp gns3 inventory -out inventory.json
/tmp/ifp gns3 host-check -topology $T -disk ~/GNS3          # exit 1 if RAM/disk insufficient or unknown
/tmp/ifp gns3 project-ensure -project InfraFlow-Automated-Validation   # reuses only an exact name; never deletes
/tmp/ifp gns3 topology-apply -topology $T                    # idempotent; refuses missing templates before creating anything
/tmp/ifp gns3 start -project InfraFlow-Automated-Validation -nodes REFDHCP,PC1
/tmp/ifp gns3 snapshot -project InfraFlow-Automated-Validation -out snapshot.json
/tmp/ifp gns3 report-check -dir artifacts/finalization/evidence/gns3-20261122
```

The reference dhcpd configuration is `logs/ref-dhcpd-final.conf`; runs 1 and 2
used the same file without option 43 (run 1 also without `echo-client-id`).

## Scenarios

| ID | Scenario | Status | Key evidence |
|---|---|---|---|
| GNS-01 | Controller discovery and inventory | PASS | `GNS-01.json`, `logs/gns3-inventory.json` |
| GNS-03 | Isolated project, topology applied twice (2nd run: no change) | PASS | `GNS-03.json`, `logs/topology-apply-{1,2}.json` |
| DHCP-CTL-01 | VPCS lease from the reference server | PASS | `DHCP-CTL-01.json` |
| CIS-02-REF | IOSv 15.9(3)M6 AutoInstall vs reference ISC dhcpd | **UNSUPPORTED** (image-side) | `CIS-02-REF.json`, 3 captures, IOS PnP tech summary |

### IOSv AutoInstall — what was established

| Stage | Result |
|---|---|
| DHCP_DISCOVERED | PASS — vendor class `ciscopnp`, client-id `cisco-0c4a.6a7a.0000-Gi0/0` |
| DHCP_OFFERED | PASS — valid broadcast OFFER on the IOSv link |
| DHCP_BOUND | **FAIL** — no DHCPREQUEST in 3 runs; RARP fallback |
| TFTP_REQUESTED … CONFIG_PERSISTED | NOT_RUN (not reached) |

Ruled out, each by an observation: InfraFlow's DHCP server (an independent
ISC dhcpd behaves identically), reply addressing (the broadcast OFFER is on
the client link), client-id echo (run 2 with `echo-client-id`), PnP option 43
(run 3), interface down (Gi0/0 up/up during discovery), startup config present
(hdb detached, setup dialog reached).

Confirmed on the device: `flash:/pnp-tech/pnp-tech-discovery-summary` shows
`[Auto Installer] started` → `stopped, reason:[Discovery Failed]` in the same
millisecond (×16), `PnP autoinstall exceeded timeout (0) ms` and
`DHCP Auto-Install … IP-Resolved: No`. The same image's regular DHCP client
(`ip address dhcp`) bound 10.99.0.101 from the same server in 13 s and pinged
it. Conclusion (high confidence): DHCP-triggered AutoInstall does not work on
this IOSv image. The Cisco profile keeps the SSH executor (PASS on IOSv) and
AutoInstall on platforms where it was observed working (c7200).

## Not run in this session

- MikroTik CHR, CSR1000v, c7200 and FortiGate scenarios — the evidence of
  2026-11-19/20 stands and was not re-executed on this tree.
- FortiGate — still BLOCKED (unknown pre-set admin password; no password was guessed).
- IOL-XE — BLOCKED (image absent, confirmed through the controller API).
- Physical hardware — never tested; a GNS3 result is not hardware evidence.

## Cleanup

IOSV1, REFDHCP and PC1 stopped, project closed (not deleted). The controller
was left running on 127.0.0.1:3080, as the brief expected it to be.

## Correction — later on 2026-11-22 (release validation of `c9f5a86`)

- "The Cisco 7200 template now points to a 15.3(3)XB12 image" (Images section) was read from the image file name only. Booted
  on 2026-11-22, that image reports **12.4(24)T5**; the 15.3(3)XB12 profile is not lab-verified.
- The c7200 AutoInstall, CHR, IOSv executor and IOSv M8 scenarios were re-run on `c9f5a86`: see
  [GNS3_VALIDATION_c9f5a86.md](GNS3_VALIDATION_c9f5a86.md). This report is kept unchanged above as the record of the
  `c1917ac` (dirty) run.
