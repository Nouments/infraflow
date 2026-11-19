# GNS3 integration report — 2026-11-19

Controller GNS3 3.1.0a4 (local). Project `infraflow-e2e-20261119`, created for
this work; topology and host details in
[environment-inventory.md](../audit/environment-inventory.md). No pre-existing
project was opened, changed, or deleted.

## Scenarios executed on real virtual appliances

| # | Scenario | Device | Outcome |
|---|---|---|---|
| 1 | Provider `generate-all` → `serve` → Agent `run` (gRPC sync, hash-verified staging, report) | lab host | PASS |
| 2 | DHCP unavailable (no server) | IOSv | Expected failure observed (7 unanswered DISCOVERs / 45 s) |
| 3 | DHCP-triggered Cisco AutoInstall | IOSv 15.9(3)M6 | FAIL — appliance client never REQUESTs; reproduced with ISC dhcpd |
| 4 | InfraFlow DHCP lease to standard IOS client | IOSv | PASS |
| 5 | InfraFlow TFTP delivery + apply of generated `r1-confg` | IOSv | PASS |
| 6 | Generated static route forwards traffic | IOSv | PASS |
| 7 | `cisco.ios.configure` over SSH (apply, read-back, restore) | IOSv | PASS |
| 8 | Wrong password / tampered host key / unauthorized device / idempotency replay | IOSv | Fail closed / refused / 200-same-ID / 409 |
| 9 | Host-key rotation while Provider runs | IOSv | Defect found → fixed → fails closed |
| 10 | InfraFlow DHCP lease to RouterOS `dhcp-client` | CHR 7.8 | PASS |
| 11 | `.auto.rsc` Auto Import (FTP by harness), state, routing, idempotency, reboot persistence | CHR 7.8 | PASS |
| 12 | FortiGate boot and access | FortiOS 7.0.8 VM | BLOCKED (unknown admin password) |
| 13 | Agent `diagnose` over real DHCP/TFTP/console evidence | lab host | Two defects found → fixed → correct diagnosis |

Reports: [Cisco](cisco-autoinstall-report.md) · [MikroTik](mikrotik-report.md) ·
[FortiGate](fortigate-report.md). Sanitized evidence: [evidence/](evidence/).
Large PCAPs and raw console logs (which contain echoed lab secrets) were kept
outside the repository; the committed summaries cite their SHA-256.

## Diagnostics over real evidence

| Input | Before fix | After fix |
|---|---|---|
| `serve-dhcp` lines of the failed AutoInstall (10 discover/offer, no request) | "No failure found … DHCP evidence missing" | `DHCP_OFFER_NOT_ACCEPTED`, cause "server sent 5 offers but the client never requested the lease", device-side next steps |
| Successful TFTP phase + full console | `TFTP_FILE_MISSING` (false positive from `%Error opening flash0:ios_config.txt (File not found)`) | No TFTP failure; with a provisioning-only console capture: "no failure found … success cannot be claimed" and the missing chain items listed |

Output: [diagnose-iosv-real.txt](evidence/diagnostics/diagnose-iosv-real.txt).
The full interactive console (with operator typos) still yields
`DEVICE_CONFIG_REJECTED` from `% Invalid input detected`; feed the engine the
provisioning capture, not an interactive session.

## Reproduce

```bash
# 0. credentials only via the environment
export GNS3_USER=… GNS3_PASSWORD=…
# 1. static binaries + lab image (from the repository root)
L=$(mktemp -d); for b in provider agent; do
  CGO_ENABLED=1 go build -trimpath -tags netgo,osusergo \
    -ldflags '-s -w -linkmode external -extldflags "-static"' \
    -o $L/out/infraflow-$b ./$b/cmd; done
printf 'FROM debian:bookworm-slim\nRUN apt-get update && apt-get install -y --no-install-recommends iproute2 iputils-ping tcpdump curl openssh-client sshpass python3 ca-certificates tini && rm -rf /var/lib/apt/lists/*\nCOPY out/ /usr/local/bin/\nENTRYPOINT ["/usr/bin/tini","--"]\nCMD ["sleep","infinity"]\n' > $L/Dockerfile
docker build -t infraflow-e2e-lab:local $L
# 2. isolated topology
cd scripts/lab
./gns3lab.py project-create infraflow-e2e
python3 -c 'import gns3lab as g; p=g.project_id("infraflow-e2e"); g.api("POST",f"/projects/{p}/nodes",{"name":"SW1","node_type":"ethernet_switch","compute_id":"local"})'
./gns3lab.py node-docker infraflow-e2e infraflow-e2e-lab:local LAB 1
./gns3lab.py node-from-template infraflow-e2e <IOSv-template-id> R1 '{"properties":{"hdb_disk_image":""}}'
./gns3lab.py node-from-template infraflow-e2e <CHR-template-id> MT1 '{"properties":{"ram":256,"cpus":1,"adapters":2}}'
./gns3lab.py link infraflow-e2e SW1 0 0 LAB 0 0
./gns3lab.py link infraflow-e2e SW1 0 1 R1 0 0
./gns3lab.py link infraflow-e2e SW1 0 2 MT1 0 0
./gns3lab.py start infraflow-e2e LAB   # then: ip addr add 192.0.2.2/24 dev eth0 inside it
# 3. inside LAB: generate, serve, sync, start services
infraflow-provider generate-all -f ztp-iosv.yaml -out /lab/pdata/artifacts
INFRAFLOW_AGENT_TOKEN=… infraflow-provider serve -config provider.yaml &
INFRAFLOW_AGENT_TOKEN=… infraflow-agent run -config agent.yaml
B=/lab/adata/iosv-lab/bootstrap
infraflow-agent serve-dhcp -config $B/dhcp/config.json -interface eth0 -listen 192.0.2.2 &
infraflow-agent serve-tftp -config $B/tftp/config.json -root $B -interface eth0 -listen 192.0.2.2 &
# 4. devices: start R1/MT1; on R1 `copy tftp://192.0.2.2/r1-confg running-config`;
#    for MT1: curl --netrc-file … -T device-mt1.auto.rsc ftp://<chr>/mt1.auto.rsc
# 5. executor: POST /api/v1/executions with an Idempotency-Key (see cisco report)
# 6. diagnosis: infraflow-agent diagnose -device-id r1 -bootstrap-log dhcp.log …
```

Inputs used: [ztp-iosv.yaml](evidence/cisco-iosv/ztp-iosv.yaml),
[vendors-lab.yaml](evidence/mikrotik-chr/vendors-lab.yaml).
