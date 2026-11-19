# Environment inventory — 2026-11-19

Collected by the engineering agent from command output on the local host.
No host network configuration, existing GNS3 project, or appliance image was
modified.

## Repository

| Item | Observed |
|---|---|
| Branch | `develop`, clean, equal to `origin/develop` after `git fetch` |
| HEAD at start | `62a28b0` "stabilization12 and gaining over 85+ score benchmark" |
| Notable | `62a28b0` **deleted** `.github/workflows/go-ci.yml`, which `041cbc3` had added. `CURRENT.md` still says the workflow was restored. |
| Hosted CI history (GitHub API, public) | Every recorded `Go CI` run failed (latest `dc85a7f`, 2026-11-18). The only failing step in each was `linux / Check reachable vulnerabilities`; `windows` and `presentation-demo` jobs passed. |
| Size | ~46k lines of Go (excluding `infraflow-network-reference/`) |

## Host

| Item | Observed |
|---|---|
| OS | Arch Linux, kernel 7.2.9-arch1-1, x86_64 |
| CPU | Intel Core i5-1135G7, 8 logical CPUs, `/dev/kvm` present |
| RAM | 15 GiB total, ~6.8 GiB available at start |
| Disk | 218 GB root, 60 GB free |
| Go | `go1.27.2` installed; `go.mod` requires `go 1.26.0`, toolchain `go1.26.11`. Local runs used `GOTOOLCHAIN=local` (1.27.2). |
| Node.js | v24.21.0 (Admin Web script checks) |
| Docker | 29.9.0, overlayfs |
| QEMU | 11.1.2 |
| Capture | tcpdump 4.99.7, TShark 4.7.4 |
| Scanners | `govulncheck` installed; `golangci-lint`, `staticcheck`, `gh` not installed |
| Host DHCP/TFTP | No `dnsmasq`, `isc-dhcp-server`, or `tftpd` service active; nothing listening on UDP 67/69 on the host. All provisioning services ran only inside the isolated GNS3 lab segment. |

## GNS3

| Item | Observed |
|---|---|
| Controller | `http://127.0.0.1:3080`, version **3.1.0a4**, v3 API |
| Authentication | The user-provided lab credentials were valid (JWT issued). They are default credentials; see the security review. |
| Computes | local + one remote compute `debian-server` (not used) |
| Existing projects | 9 (one, `infraflow-provisioning-validation-20261119`, was open with a stopped Cisco 7200 + Provider + Agent). **None were modified.** |
| Project created for this work | `infraflow-e2e-20261119` (`f4e36d28-…`): one Ethernet switch, no Cloud/NAT node, so the lab segment has no path to the host or any external network. |

### Appliance templates and images

| Template | Type | Image (present on disk) | Used |
|---|---|---|---|
| Cisco IOSv | qemu | `vios-adventerprisek9-m.spa.159-3.m6.qcow2` | **Yes** — reports 15.9(3)M6 |
| Cisco IOSvm8 | qemu | `vios-…159-3.m8.qcow2` | No |
| Cisco 7200 | dynamips | `c7200-adventerprisek9-mz.153-3.XB12.image` (reports 12.4(24)T5) | Prior run only |
| Cisco 3640 | dynamips | `c3640-a3js-mz.124-25d.image` | No |
| Cisco CSR1000v | qemu | `csr1000v-universalk9.16.07.01-serial.qcow2` (template 4 GB RAM) | No — RAM budget |
| Cisco IOL-XE | docker | `iol-xe/iol-xe:17-18-02` | No — time budget |
| Cisco IOSvL2 | qemu | `vios_l2-…high_iron_20200929` | No |
| MikroTik CHR (3 templates) | qemu | `chr-7.8.img` (templates request 8 GB / 36 vCPU) | **Yes**, overridden to 256 MB / 1 vCPU — reports RouterOS 7.8 (stable), board CHR |
| fortigate | qemu | `fortigate-7.0.8-gns3.qcow2` | **Booted** (FortiOS-VM64-KVM). Login blocked: the image carries a pre-set admin password that is not known. |
| InfraFlow Provider/Agent | docker | `infraflow-provider/agent:delivery-20261118*` | No — a fresh lab image was built from the current tree |
| Others (Chromium, DNS, nginx, debian-*, haproxy, code-server, WinBox) | docker | — | No |

### Appliance capabilities observed

| Appliance | Console | Mgmt | DHCP behavior | Provisioning observed | Persistence |
|---|---|---|---|---|---|
| IOSv 15.9(3)M6 | telnet | SSH v2 after keygen; only `diffie-hellman-group14-sha1` / `gex-sha1` KEX | Template attaches a CVAC day-0 disk (`IOSv_startup_config.img`) that applies and saves a config, disabling PnP/AutoInstall. Without it, AutoInstall/PnP sends `ciscopnp` DISCOVERs (client-ID = serial) but never REQUESTs an offered lease. The standard DHCP client works. | `copy tftp` from InfraFlow TFTP: works | `write memory` only |
| RouterOS 7.8 CHR | telnet | SSH, FTP, API, WinBox enabled by default | Default `dhcp-client` on ether1; took an InfraFlow DHCP lease | `.auto.rsc` Auto Import over FTP: works; writes `.auto.log` | Automatic (verified across reboot) |
| FortiOS 7.0.8 VM | telnet | unknown (no login) | No DHCP traffic from port1 (pre-set static config) | Not testable | Not testable |

## Lab topology used

```
            SW1 (GNS3 ethernet_switch, isolated: no Cloud/NAT)
   p0 ─ LAB  docker infraflow-e2e-lab:20261119  eth0 192.0.2.2/24
        (Provider serve + Agent run/serve-dhcp/serve-tftp, tcpdump)
   p1 ─ R1   Cisco IOSv 15.9(3)M6   Gi0/0  (MAC 0c:eb:25:a0:00:00)
   p2 ─ MT1  MikroTik CHR 7.8       ether1 (MAC 0c:27:a8:1f:00:00)
   p3 ─ FGT1 FortiGate VM 7.0.8     port1  (stopped after login blocker)
```

All addressing is TEST-NET (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24).
The lab image packages statically linked Provider and Agent binaries built
from this working tree (`go build -tags netgo,osusergo`, static external link).
