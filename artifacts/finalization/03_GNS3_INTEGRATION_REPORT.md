# 03 — GNS3 integration report

## Topology (isolated project `infraflow-e2e-20261119`, GNS3 3.1.0a4)

```
                 SW1 (GNS3 Ethernet switch, 192.0.2.0/24 — no uplink)
  ┌──────┬──────┬──────┬──────┬──────┬──────┬──────┐
 LAB    R2     R1    MT1    FGT1   MT2   CSR1  IOSVM8
 .2    .12    .11  (stopped)(.31)   .21   .13   (DHCP)
 Provider, Agents, serve-dhcp/serve-tftp (iosv-lab), tcpdump
```

Inventory: `evidence/gns3/inputs/lab-all.yaml` — site `iosv-lab` (r2, AutoInstall) and site `vendor-lab`
(r1 IOSv ssh, r3 CSR1000v ssh, mt1 CHR auto-rsc + OSPF, fgt1 FortiOS). Two Agents (`agent-iosv`,
`agent-vendor`) with site-scoped tokens on the lab host.

## Results per device

| Device | Path | Status | Key facts |
|---|---|---|---|
| Cisco 7200 12.4(24)T5 | AutoInstall via InfraFlow DHCP+TFTP | PASS | `network-confg` 22 B and `r2-confg` 135 B served; hostname, address, static route, ping 5/5 |
| IOSv 15.9(3)M6 | SSH executor | PASS | apply, verify, rollback, idempotency, failures, concurrency, crash recovery |
| IOSv 15.9(3)M6/M8 | AutoInstall | PARTIAL | PnP/AutoInstall client sends Discover (vendor class `ciscopnp`, options 66/67 requested) and never REQUESTs InfraFlow offers; another IOS DHCP client on the same box accepts them |
| CSR1000v IOS XE 16.07.01 | SSH executor | PASS (after D5) | needs SHA-1 exception (device offers only dh-group14/gex-sha1); vmxnet3 NIC link stayed down, virtio works |
| MikroTik CHR 7.8 | `mikrotik.routeros.import` | PASS (after D1–D4) | upload, import, read-back, idempotent re-run, no leftover file, ping; OSPF FULL with IOSv |
| FortiGate VM 7.0.8 | — | BLOCKED | admin password unknown; factory default refused; no bypass attempted |
| IOL-XE 17.18 | — | BLOCKED | Docker image `iol-xe/iol-xe:17-18-02` not present (licensed) |

## Functional verification

- Interfaces/addresses: console read-back on every configured device (evidence files above).
- Routes: c7200 static route; CHR static route and OSPF-learned 10.99.99.1/32 (`DAo`), ping 3/3.
- OSPF adjacency: FULL both sides (mt1 DR); caveat: the CHR keeps its factory DHCP client on ether1, and OSPF used that address.
- VLANs: UNSUPPORTED (not in the model).
- Persistence across reload: NOT_RUN (executors change running configuration only, by design).

## Harness

`scripts/lab/e2e.py` (lab host setup, Provider/Agents, services, executions, known hosts), `scripts/lab/gns3lab.py`
(GNS3 API and console), `scripts/qa/web_e2e.mjs` (JSON + text report). Device scenarios produce text logs with
execution IDs and console reads; a JSON report for them is not implemented (GNS-02 PARTIAL).

## Lab safety

No pre-existing GNS3 project or appliance was modified or deleted; nodes added this session (MT2, CSR1, IOSVM8)
belong to the lab project. Host network configuration was not changed. DHCP/TFTP ran only on the isolated
segment; no second DHCP server was started.
