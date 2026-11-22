# GNS3 release validation of `c9f5a86` — 2026-11-22

Commit under test: `c9f5a8676006f505e711f071bdf89f5056290dfa` (`develop`, = `origin/develop`).
Every report in
[`artifacts/finalization/evidence/gns3-c9f5a86/`](../../artifacts/finalization/evidence/gns3-c9f5a86/)
names this commit. Reports produced with the commit's own binaries say
`worktree_dirty: false`; the one scenario that needed the uncommitted
persistence patch says `worktree_dirty: true` and names the patch hash. Earlier
reports (`gns3-20261122/`, commit `c1917ac`, dirty) are kept as history and are
**not** used as evidence for `c9f5a86`.

`infraflow-provider gns3 report-check -dir artifacts/finalization/evidence/gns3-c9f5a86`
→ exit 0 (schema, statuses, secret scan, evidence SHA-256).

## Binaries

| Binary | SHA-256 | Built from | Used for |
|---|---|---|---|
| `infraflow-provider` (static) | `78d64782…b06d2` | clean worktree of `c9f5a86` (`vcs.modified=false`) | inventory, topology, generation, c7200, CHR, IOSv executor, IOSv M6/M8 |
| `infraflow-agent` | `c9b90b23…f16f` | same | Agent sync, DHCP/TFTP, diagnose |
| `infraflow-provider` (static) | `0d668450…d447` | `c9f5a86` + uncommitted persistence patch (`logs/persist-patch-on-c9f5a86.diff`) | GNS-04-CIS only |

Full hashes: `logs/lab-binaries.txt`.

## Topology (`scripts/lab/topologies/infraflow-automated-validation-devices.json`)

Isolated project `InfraFlow-Automated-Validation` (same project as the earlier
run; no other project touched). One Ethernet switch, no Cloud/NAT node:

```
SW-PROV 0/0 IOSV1  (IOSv 15.9(3)M6, vendor-lab/r1)     0/4 R2     (c7200, iosv-lab/r2)
        0/1 REFDHCP (reference ISC dhcpd 4.4.3-P1)      0/5 MT1    (CHR 7.8, vendor-lab/mt1)
        0/2 PC1    (VPCS control client)                0/6 IOSVM8 (IOSv 15.9(3)M8)
        0/3 LAB    (Provider, Agents, serve-dhcp/serve-tftp, docker infraflow-e2e-lab:20261119)
```

`topology-apply` run twice: 3 nodes + 3 links created, then nothing created.
Inventory used: `inputs/lab-c9f5a86.yaml` (derived from the 2026-11-19 lab
inventory; r2 MAC follows the new node).

## Results

| Report | Scenario | Status | Facts |
|---|---|---|---|
| GNS-01 | Controller inventory | PASS | 3.1.0a4, v3; 30 templates; IOL-XE and WinBox images absent |
| GNS-03 | Isolated topology, idempotent re-apply | PASS | second apply: 0 created |
| DHCP-CTL-01 | VPCS vs reference ISC dhcpd | PASS | DORA, ping 3/3 |
| CIS-01 | Cisco 7200 AutoInstall from InfraFlow DHCP/TFTP | PASS | power cycle with empty NVRAM → DORA → `network-confg` + `r2-confg` → hostname, address, static route, ping 5/5 |
| CIS-02 | IOSv 15.9(3)M6 AutoInstall | UNSUPPORTED | generator refuses the profile; 122+ DISCOVER/OFFER, 0 REQUEST |
| CIS-03 | IOSv 15.9(3)M8 AutoInstall | UNSUPPORTED (was PARTIAL) | generator refuses the release; 0 REQUEST to InfraFlow **and** to the reference dhcpd (57 OFFER) |
| CIS-04 | IOSv SSH executor | PASS | apply, replay (200), idempotent re-run, IOS rejection → verified rollback |
| MTK-02 | CHR import (MTK-02/03/04) | PASS | unknown host key fails closed; import verified; idempotent; replay 200 |
| GNS-04-MTK | CHR config after reboot | PASS | owned objects present after `/system reboot` |
| GNS-04-CIS | Explicit `persist: true` + IOS reload | PASS, **uncommitted patch** | startup-config verified by the executor; P1 survives reload, the non-persisted P2 does not |

Each capture also shows the other clients, because the switch floods
broadcasts. In the same capture as the IOSv loops, the c7200 and the CHR
complete DORA with the InfraFlow server. That rules out the server for the IOSv
failure.

### Findings in this run

1. **The GNS3 c7200 template image is mislabeled.** The file is named
   `c7200-adventerprisek9-mz.153-3.XB12.image`, but the device reports
   **12.4(24)T5**. Run 1 used an inventory that declared 15.3(3)XB12 from the
   file name. It completed, but it is kept only as evidence of the mismatch.
   Run 2, with the version the device reports, is the result. The 15.3(3)XB12
   profile remains *not lab-verified*. The 2026-11-22 morning report said the
   template "now uses 15.3(3)XB12"; that statement was based on the file name
   and is wrong.
2. **The generator refuses IOSv AutoInstall.** It refuses M8 because no
   profile exists, and M6 because the bootstrap is not verified. This is the
   intended fail-closed behaviour (`logs/generate-refuses-*.txt`).
3. **The API rejects Idempotency-Keys shorter than 16 characters** with
   HTTP 400. This is the documented contract; the lab's first keys were too
   short.

### CSR1000v (CIS-05)

Not re-run: `gns3 host-check` refused the CSR topology. The CSR needs 4096 MiB
plus the 1 GiB reserve; 5005 MiB were available while the soak ran
(`logs/host-check-csr.json`). The reserve was not lowered. The executor
production code is identical between the 2026-11-20 CSR evidence (`93bae62`
lineage, after fix D5) and `c9f5a86`; only one test file changed. The earlier
PASS therefore still describes this code, but it was **not executed on
`c9f5a86`**. Prerequisite: about 5.2 GiB of free RAM on the GNS3 host.

### FortiGate (FTN-02)

BLOCKED, unchanged. No administrator password is available, and none was
guessed. An executor would also be needed. Prerequisite: the appliance's
password or a fresh FortiOS VM.

## Reproduction

```sh
# credentials only from the environment
export INFRAFLOW_GNS3_URL=http://127.0.0.1:3080 INFRAFLOW_GNS3_USERNAME=… INFRAFLOW_GNS3_PASSWORD=…
export GNS3_URL=$INFRAFLOW_GNS3_URL GNS3_USER=$INFRAFLOW_GNS3_USERNAME GNS3_PASSWORD=$INFRAFLOW_GNS3_PASSWORD
git worktree add --detach /tmp/wt c9f5a8676006f505e711f071bdf89f5056290dfa && cd /tmp/wt
CGO_ENABLED=1 go build -trimpath -tags osusergo,netgo,sqlite_omit_load_extension \
  -ldflags '-linkmode external -extldflags "-static"' -o /tmp/bin/infraflow-provider ./provider/cmd
CGO_ENABLED=0 go build -trimpath -o /tmp/bin/infraflow-agent ./agent/cmd
T=scripts/lab/topologies/infraflow-automated-validation-devices.json
/tmp/bin/infraflow-provider gns3 host-check -topology $T -disk ~/GNS3
/tmp/bin/infraflow-provider gns3 project-ensure -project InfraFlow-Automated-Validation
/tmp/bin/infraflow-provider gns3 topology-apply -topology $T      # twice
python3 scripts/lab/gns3lab.py node-docker InfraFlow-Automated-Validation infraflow-e2e-lab:20261119 LAB
python3 scripts/lab/gns3lab.py link InfraFlow-Automated-Validation LAB 0 0 SW-PROV 0 3
export LAB_CONTAINER=GNS3.LAB.<project-id> LAB_BIN=/tmp/bin LAB_STATE=<private file>
python3 scripts/lab/e2e.py setup artifacts/finalization/evidence/gns3-c9f5a86/inputs/lab-c9f5a86.yaml
python3 scripts/lab/e2e.py provider-start; python3 scripts/lab/e2e.py agent-run agent-iosv
python3 scripts/lab/e2e.py services iosv-lab
# then start nodes, day-0 on consoles, and submit executions with `e2e.py exec` (keys of 16+ characters)
python3 artifacts/finalization/evidence/gns3-c9f5a86/build_reports.py
/tmp/bin/infraflow-provider gns3 report-check -dir artifacts/finalization/evidence/gns3-c9f5a86
```

## Safety and cleanup

- Only `InfraFlow-Automated-Validation` was modified. It now has 8 nodes,
  including the LAB container.
- No other project, template, appliance, server setting or host network
  setting was changed.
- InfraFlow DHCP and the reference dhcpd never ran at the same time.
- Device passwords were generated randomly, kept in a mode-600 state file
  outside the repository, and masked in every console log.
- A scan of the evidence for every secret value found no match.
- At the end, the nodes were stopped and the project closed by the controller
  (`auto_close`). Nothing was deleted.
- The controller still uses the default `admin`/`admin` account. This is a
  risk; change it.
