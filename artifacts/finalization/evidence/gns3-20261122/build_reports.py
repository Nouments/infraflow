# Assembles the 2026-11-22 GNS3 scenario reports from evidence collected in
# this session. Every value below is taken from a file referenced in the
# report; nothing is inferred from earlier sessions.
import json, hashlib, os, sys
E = sys.argv[1]
R = E
def ref(rel, desc):
    data = open(os.path.join(E, rel), "rb").read()
    return {"path": rel, "sha256": hashlib.sha256(data).hexdigest(), "description": desc}
COMMIT = "c1917ac22b3e82b05ea62e13a1660a24d1c57e4a"
GNS3 = {"url": "http://127.0.0.1:3080", "version": "3.1.0a4", "project": "InfraFlow-Automated-Validation", "project_id": "af712d95-a011-4c54-b3f1-242e365e71dd"}
base = dict(schema_version=1, infraflow_version="dev (develop, uncommitted changes of 2026-11-22)", commit=COMMIT, worktree_dirty=True,
            gns3=GNS3, site="lab-validation", agent_id="", execution_id="", artifact_hashes={}, errors=[], stages=[])
TZ = "+03:00"
def t(hms): return f"2026-11-22T{hms}{TZ}"
reports = []

reports.append(dict(base, scenario_id="GNS-01", title="Controller discovery and real inventory", mandatory=True,
  started_at=t("08:27:50"), finished_at=t("08:33:40"), duration_seconds=350,
  device="controller", model="GNS3 server", os="GNS3", os_version="3.1.0a4",
  initial_state="gns3server not running (nothing listening on 127.0.0.1:3080)",
  actions=[
    {"at": t("08:27:50"), "description": "started gns3server bound to 127.0.0.1:3080 (command-line only; persistent configuration unchanged)", "result": "listening"},
    {"at": t("08:33:00"), "description": "infraflow-provider gns3 inventory (credentials from INFRAFLOW_GNS3_* environment)", "result": "first run exposed a client decoding defect (float cpus); fixed, re-run"},
    {"at": t("08:33:40"), "description": "infraflow-provider gns3 host-check -disk ~/GNS3", "result": "ok"}],
  final_state="controller reachable on loopback only, v3 API, bearer authentication",
  checks=[
    {"name": "version", "expected": "API version and server version reported by /v3/version", "observed": "v3, 3.1.0a4", "status": "PASS"},
    {"name": "authentication", "expected": "bearer token from /v3/access/users/authenticate; HTTP Basic refused by v3", "observed": "token issued; Basic -> 401", "status": "PASS"},
    {"name": "templates", "expected": "every template listed with image availability checked, none guessed", "observed": "30 templates; 28 images present; IOL-XE (iol-xe/iol-xe:17-18-02) and Mikrotik WinBox (gns3/mikrotik-winbox) absent", "status": "PASS"},
    {"name": "inventory errors", "expected": "no section silently missing", "observed": "errors: none", "status": "PASS"},
    {"name": "host resources", "expected": "CPU, RAM, disk read before any node is started", "observed": "8 vCPU, 7.1 GiB available RAM, 56.9 GiB free disk", "status": "PASS"}],
  logs=[ref("logs/gns3-inventory.json", "inventory (remote compute host redacted)"), ref("logs/host-check.json", "host resources")], captures=[],
  warnings=["controller uses the default admin/admin account; change it before exposing the server", "the controller lists a remote compute that was unreachable; /v3/statistics blocks on it"],
  status="PASS", status_reason="inventory collected from the real controller with no missing section"))

reports.append(dict(base, scenario_id="GNS-03", title="Isolated, reproducible validation project", mandatory=True,
  started_at=t("08:37:30"), finished_at=t("08:38:30"), duration_seconds=60,
  device="SW-PROV, IOSV1, REFDHCP, PC1", model="Ethernet switch, Cisco IOSv, debian-dhcp, VPCS", os="mixed", os_version="n/a",
  initial_state="project InfraFlow-Automated-Validation absent; 10 projects on the server",
  actions=[
    {"at": t("08:37:30"), "description": "gns3 host-check -topology scripts/lab/topologies/infraflow-automated-validation.json", "result": "budget 656 MiB RAM, 250 MiB disk: ok"},
    {"at": t("08:37:40"), "description": "gns3 project-ensure -project InfraFlow-Automated-Validation", "result": "created (auto_open=false, auto_start=false)"},
    {"at": t("08:37:50"), "description": "gns3 topology-apply (1st run)", "result": "4 nodes, 3 links created"},
    {"at": t("08:38:00"), "description": "gns3 topology-apply (2nd run)", "result": "4 nodes reused, 3 links reused, nothing created"}],
  final_state="project opened with 4 nodes and 3 links; no other project touched",
  checks=[
    {"name": "templates by name", "expected": "every node built from a template that exists on this server", "observed": "Ethernet switch, Cisco IOSv, debian-dhcp, VPCS resolved", "status": "PASS"},
    {"name": "idempotent re-apply", "expected": "second apply changes nothing", "observed": "created_nodes=null, reused 4, created_links=0, reused_links=3", "status": "PASS"},
    {"name": "isolation", "expected": "no uplink to the host network; other projects unchanged", "observed": "no Cloud/NAT node; only this project created", "status": "PASS"}],
  logs=[ref("logs/project-ensure.json", "project creation"), ref("logs/topology-apply-1.json", "first apply"), ref("logs/topology-apply-2.json", "second apply"), ref("logs/project-snapshot.json", "final nodes and links")], captures=[],
  warnings=[], status="PASS", status_reason="project and topology created from real templates and re-applied without change"))

dhcp_stages = [
  {"name": "DHCP_DISCOVERED", "status": "PASS", "evidence": "DISCOVER xid 0x5c6, vendor class ciscopnp, client-id cisco-0c4a.6a7a.0000-Gi0/0"},
  {"name": "DHCP_OFFERED", "status": "PASS", "evidence": "broadcast OFFER 10.99.0.101 from the reference ISC dhcpd reaches the IOSv link"},
  {"name": "DHCP_BOUND", "status": "FAIL", "evidence": "no DHCPREQUEST in 3 runs (19 + 42 + 18 DISCOVER/OFFER pairs); client falls back to RARP"},
  {"name": "TFTP_REQUESTED", "status": "NOT_RUN", "evidence": "not reached"},
  {"name": "ARTIFACT_TRANSFERRED", "status": "NOT_RUN", "evidence": "not reached"},
  {"name": "CONFIG_APPLIED", "status": "NOT_RUN", "evidence": "not reached"},
  {"name": "CONFIG_VERIFIED", "status": "NOT_RUN", "evidence": "not reached"},
  {"name": "CONFIG_PERSISTED", "status": "NOT_RUN", "evidence": "not reached"}]
reports.append(dict(base, scenario_id="CIS-02-REF", title="Cisco IOSv 15.9(3)M6 AutoInstall against an independent reference DHCP server (DIA-09)", mandatory=True,
  started_at=t("08:40:30"), finished_at=t("08:57:00"), duration_seconds=990,
  device="IOSV1", model="Cisco IOSv", os="IOS", os_version="15.9(3)M6",
  initial_state="new node, empty NVRAM, startup-config disk (hdb) detached, setup dialog reached",
  actions=[
    {"at": t("08:40:40"), "description": "reference ISC dhcpd 4.4.3-P1 on REFDHCP, subnet 10.99.0.0/24, options 3/66/150, next-server", "result": "running"},
    {"at": t("08:41:03"), "description": "control client PC1 (VPCS) ip dhcp", "result": "DORA complete, 10.99.0.100"},
    {"at": t("08:41:30"), "description": "start IOSV1, capture on its link (run 1)", "result": "DISCOVER/OFFER loop, no REQUEST"},
    {"at": t("08:44:39"), "description": "run 2: echo-client-id true (RFC 6842), reload IOSV1", "result": "OFFER echoes option 61; still no REQUEST"},
    {"at": t("08:49:56"), "description": "run 3: add PnP option 43, reload IOSV1", "result": "OFFER carries option 43; still no REQUEST"},
    {"at": t("08:53:34"), "description": "control: answer no to the dialog, ip address dhcp on Gi0/0", "result": "bound 10.99.0.101 in 13 s, ping 3/3"},
    {"at": t("08:56:00"), "description": "read IOS flash:/pnp-tech/pnp-tech-discovery-summary", "result": "Auto Installer start/stop 'Discovery Failed' in 0 ms x16; 'PnP autoinstall exceeded timeout (0) ms'; DHCP Auto-Install IP-Resolved: No"}],
  final_state="IOSv up with a lease from its regular DHCP client (manual configuration, not AutoInstall); NVRAM not written",
  checks=[
    {"name": "server answers", "expected": "an RFC 2131-valid OFFER to each DISCOVER", "observed": "OFFER with matching xid/chaddr, broadcast flag, server-id, valid checksums", "status": "PASS"},
    {"name": "AutoInstall lease", "expected": "DHCPREQUEST/ACK from the AutoInstall client", "observed": "never sent in 3 runs with 3 server option sets", "status": "FAIL"},
    {"name": "same server, regular IOS DHCP client", "expected": "lease on the same wire", "observed": "Bound, 10.99.0.101, gateway 10.99.0.1, ping 100%", "status": "PASS"}],
  stages=dhcp_stages,
  logs=[ref("logs/iosv1-console.log", "IOSv console, ANSI stripped"), ref("logs/iosv1-pnp-tech-summary.txt", "IOS PnP tech summary read from the device"),
        ref("logs/iosv1-manual-dhcp-client.txt", "regular DHCP client control"), ref("logs/ref-dhcpd-run1.log", "dhcpd run 1"),
        ref("logs/ref-dhcpd-run2-echo.log", "dhcpd run 2 (echo-client-id)"), ref("logs/ref-dhcpd-run3-opt43.log", "dhcpd run 3 (option 43)"), ref("logs/ref-dhcpd-final.conf", "final dhcpd configuration")],
  captures=[ref("captures/iosv1-ref-dhcp.pcap", "run 1 (+ VPCS control)"), ref("captures/iosv1-ref-dhcp-echo-clientid.pcap", "run 2"), ref("captures/iosv1-ref-dhcp-opt43.pcap", "run 3 and manual client")],
  errors=["AutoInstall DHCP client never requests an offered lease"],
  warnings=["option 43 string used in run 3 was rejected by IOS as an invalid PnP option; it was a probe, not a PnP configuration"],
  status="UNSUPPORTED", status_reason="device-side: this image's AutoInstall handler stops ('Discovery Failed', timeout 0 ms) before any offer is processed, with two independent DHCP servers; InfraFlow cannot change it",
  diagnosis={
    "expected": "AutoInstall: DISCOVER, OFFER, REQUEST, ACK, then TFTP of network-confg/router-confg",
    "observed": "DISCOVER/OFFER repeated, no REQUEST; RARP fallback; IOS trace shows the Auto Installer handler starting and stopping in the same millisecond",
    "failed_stage": "DHCP_BOUND",
    "cause": "IOSv 15.9(3)M6 AutoInstall/PnP handler gives up immediately ('PnP autoinstall exceeded timeout (0) ms', 'IP-Resolved: No'); the image's own regular DHCP client accepts the same offers",
    "cause_confirmed": True, "confidence": "high",
    "evidence": ["pnp-tech-discovery-summary lines 'stopped, reason:[Discovery Failed]' and 'exceeded timeout (0) ms'", "3 captures: valid OFFERs, no REQUEST", "manual 'ip address dhcp' binds with the same server"],
    "ruled_out": ["InfraFlow DHCP server defect (reference ISC dhcpd behaves identically)", "reply addressing (broadcast OFFER observed on the client link)", "client-id not echoed (run 2)", "missing PnP option 43 (run 3)", "interface down (Gi0/0 up/up during discovery)", "startup-config present (hdb detached, setup dialog reached)"],
    "next_check": "use an image whose AutoInstall works (Cisco 7200 12.4(24)T5 PASS in 2026-11-19 evidence) or physical hardware; IOSv day-0 can only be validated via console or the existing SSH executor"}))

reports.append(dict(base, scenario_id="DHCP-CTL-01", title="Reference DHCP control: plain client lease (VPCS)", mandatory=False,
  started_at=t("08:41:00"), finished_at=t("08:41:12"), duration_seconds=12,
  device="PC1", model="VPCS", os="VPCS", os_version="builtin",
  initial_state="no address", actions=[{"at": t("08:41:03"), "description": "ip dhcp", "result": "DORA complete"}],
  final_state="10.99.0.100/24, gateway 10.99.0.1",
  checks=[{"name": "DORA", "expected": "DISCOVER, OFFER, REQUEST, ACK", "observed": "all four in dhcpd log and capture", "status": "PASS"}],
  stages=[{"name": "DHCP_DISCOVERED", "status": "PASS"}, {"name": "DHCP_OFFERED", "status": "PASS"}, {"name": "DHCP_BOUND", "status": "PASS"}],
  logs=[ref("logs/ref-dhcpd-run1.log", "dhcpd log")], captures=[ref("captures/iosv1-ref-dhcp.pcap", "capture")], warnings=[],
  status="PASS", status_reason="the reference server and the isolated segment work for a standard client"))

for r in reports:
    json.dump(r, open(os.path.join(R, r["scenario_id"] + ".json"), "w"), indent=2)
print("wrote", len(reports))
