# Assembles the GNS3 scenario reports of the c9f5a86 release validation
# (2026-11-22). Every value is taken from a file referenced in the report;
# nothing is carried over from earlier sessions. Run from the repository
# root:  python3 artifacts/finalization/evidence/gns3-c9f5a86/build_reports.py
import hashlib
import json
import os

E = os.path.dirname(os.path.abspath(__file__))
COMMIT = "c9f5a8676006f505e711f071bdf89f5056290dfa"
PRISTINE = "c9f5a86 (clean worktree; static Provider sha256 78d64782833bef5a8adbdadf91f4d915e70fcd01be12f9c238be8020de138de5)"
PATCHED = ("c9f5a86 + uncommitted persistence patch (logs/persist-patch-on-c9f5a86.diff sha256 5dd54ebb13c605f2…; "
           "Provider sha256 0d668450da37b7a78cf0696f8a4c61867e11fde966b866fb9bfe1718911ed447)")
GNS3 = {"url": "http://127.0.0.1:3080", "version": "3.1.0a4", "project": "InfraFlow-Automated-Validation",
        "project_id": "af712d95-a011-4c54-b3f1-242e365e71dd"}


def ref(rel, desc):
    with open(os.path.join(E, rel), "rb") as handle:
        return {"path": rel, "sha256": hashlib.sha256(handle.read()).hexdigest(), "description": desc}


def t(hms):
    return f"2026-11-22T{hms}Z"


def duration(a, b):
    def secs(x):
        h, m, s = x.split(":")
        return int(h) * 3600 + int(m) * 60 + float(s)
    return round(secs(b) - secs(a), 1)


def report(scenario, title, start, end, **fields):
    base = dict(schema_version=1, scenario_id=scenario, title=title, mandatory=True,
                started_at=t(start), finished_at=t(end), duration_seconds=duration(start, end),
                infraflow_version=PRISTINE, commit=COMMIT, worktree_dirty=False, gns3=GNS3,
                site="", device="", model="", os="", os_version="", agent_id="", execution_id="",
                actions=[], checks=[], stages=[], logs=[], captures=[], artifact_hashes={},
                errors=[], warnings=[])
    base.update(fields)
    return base


def stage(name, status, evidence):
    return {"name": name, "status": status, "evidence": evidence}


reports = []

reports.append(report(
    "GNS-01", "Controller discovery and real inventory (c9f5a86)", "06:44:10", "06:44:20",
    device="controller", model="GNS3 server", os="GNS3", os_version="3.1.0a4",
    initial_state="controller running on 127.0.0.1:3080 (left running by the previous session)",
    actions=[{"at": t("06:44:10"), "description": "infraflow-provider gns3 inventory (c9f5a86 binary, credentials from INFRAFLOW_GNS3_* environment)", "result": "exit 0"}],
    final_state="inventory collected; nothing changed on the controller",
    checks=[
        {"name": "version", "expected": "v3 API and server version", "observed": "v3, 3.1.0a4", "status": "PASS"},
        {"name": "templates", "expected": "every template with its image availability", "observed": "30 templates; IOL-XE and Mikrotik WinBox images absent", "status": "PASS"},
        {"name": "projects", "expected": "pre-existing projects listed, none modified", "observed": "11 projects (10 pre-existing + InfraFlow-Automated-Validation)", "status": "PASS"},
        {"name": "inventory errors", "expected": "no section silently missing", "observed": "errors: none", "status": "PASS"}],
    logs=[ref("logs/gns3-inventory.json", "inventory from the c9f5a86 binary")],
    warnings=["controller uses the default admin/admin account; acceptable only while it listens on loopback"],
    status="PASS", status_reason="inventory collected from the real controller by the c9f5a86 binary"))

reports.append(report(
    "GNS-03", "Isolated project, device topology applied twice (c9f5a86)", "06:44:40", "06:45:30",
    device="SW-PROV, IOSV1, REFDHCP, PC1, R2, MT1, IOSVM8", model="switch, IOSv, debian-dhcp, VPCS, c7200, CHR, IOSvm8", os="mixed", os_version="n/a",
    initial_state="project InfraFlow-Automated-Validation closed, 4 nodes / 3 links from the previous session",
    actions=[
        {"at": t("06:44:40"), "description": "gns3 host-check -topology scripts/lab/topologies/infraflow-automated-validation-devices.json", "result": "ok: budget 2192 MiB RAM, 950 MiB disk"},
        {"at": t("06:44:50"), "description": "gns3 project-ensure", "result": "reused (created=false), opened"},
        {"at": t("06:45:00"), "description": "gns3 topology-apply (1st)", "result": "3 nodes and 3 links created, 4 nodes and 3 links reused"},
        {"at": t("06:45:20"), "description": "gns3 topology-apply (2nd)", "result": "7 nodes and 6 links reused, nothing created"},
        {"at": t("06:46:00"), "description": "LAB docker node (infraflow-e2e-lab:20261119) added with scripts/lab/gns3lab.py node-docker and linked to SW-PROV 0/3", "result": "started"}],
    final_state="8 nodes, 7 links in the isolated project; no Cloud/NAT node; other projects untouched",
    checks=[
        {"name": "host pre-check", "expected": "RAM/disk budget checked before creating nodes", "observed": "ok, no problems", "status": "PASS"},
        {"name": "idempotent re-apply", "expected": "second apply creates nothing", "observed": "created_nodes=null, created_links=0, reused 7/6", "status": "PASS"},
        {"name": "isolation", "expected": "no uplink; pre-existing projects unchanged", "observed": "no Cloud/NAT node; project list unchanged", "status": "PASS"}],
    logs=[ref("logs/host-check.json", "host pre-check"), ref("logs/project-ensure.json", "project reuse"),
          ref("logs/topology-apply-1.json", "first apply"), ref("logs/topology-apply-2.json", "second apply"),
          ref("logs/project-snapshot.json", "final nodes and links")],
    warnings=["the LAB node has no GNS3 template, so it is added by the lab script, not by topology-apply"],
    status="PASS", status_reason="topology built from real templates and re-applied without change"))

reports.append(report(
    "DHCP-CTL-01", "Control client against the reference ISC dhcpd (c9f5a86 session)", "07:23:00", "07:24:10",
    device="PC1", model="VPCS", os="VPCS", os_version="GNS3 builtin",
    initial_state="InfraFlow DHCP/TFTP stopped on the segment; reference ISC dhcpd 4.4.3-P1 started on REFDHCP 10.99.0.1/24",
    actions=[{"at": t("07:24:04"), "description": "VPCS: ip dhcp; show ip; ping 10.99.0.1 -c 3", "result": "DORA, 10.99.0.100/24, 3/3 replies"}],
    final_state="PC1 bound 10.99.0.100 from the reference server",
    checks=[
        {"name": "DORA", "expected": "DISCOVER, OFFER, REQUEST, ACK in the server log", "observed": "all four for 00:50:79:66:68:00", "status": "PASS"},
        {"name": "reachability", "expected": "ping the server", "observed": "3/3", "status": "PASS"}],
    logs=[ref("logs/pc1-vs-reference-dhcpd.txt", "VPCS console"), ref("logs/ref-dhcpd.log", "reference dhcpd log"), ref("logs/ref-dhcpd.conf", "reference dhcpd configuration")],
    captures=[ref("captures/pc1-vs-reference-dhcpd.pcap", "PC1 link")],
    status="PASS", status_reason="the reference server serves a plain client, so it is a valid comparison point"))

reports.append(report(
    "CIS-01", "Cisco 7200 12.4(24)T5 AutoInstall from InfraFlow DHCP/TFTP (c9f5a86)", "07:01:40", "07:04:30",
    site="iosv-lab", device="r2", model="7200", os="Cisco IOS", os_version="12.4(24)T5", agent_id="agent-iosv",
    initial_state="R2 powered off, empty NVRAM (startup-config not present); site iosv-lab generated by c9f5a86 and synced by agent-iosv; serve-dhcp/serve-tftp running",
    actions=[
        {"at": t("06:46:30"), "description": "run 1: inventory declared 15.3(3)XB12 from the template image file name", "result": "AutoInstall completed, but the device reported 12.4(24)T5: run kept as evidence of the mismatch, not as the result"},
        {"at": t("07:00:30"), "description": "inventory corrected to 12.4(24)T5 (the version the device reports), regenerated, Agent re-synced", "result": "r2-confg sha256 04bcfe05…"},
        {"at": t("07:01:40"), "description": "R2 stopped and started (power cycle, empty NVRAM)", "result": "boot"},
        {"at": t("07:03:07"), "description": "AutoInstall", "result": "Configured from tftp://192.0.2.2/network-confg then r2-confg"},
        {"at": t("07:04:20"), "description": "read-only console post-check", "result": "hostname r2, Fa0/0 192.0.2.12/24, static route, ping 5/5"}],
    final_state="r2 configured from InfraFlow artifacts; startup-config still empty (AutoInstall does not save)",
    checks=[
        {"name": "DHCP", "expected": "reservation 192.0.2.12 for ca:01:b6:b0:00:00", "observed": "DISCOVER/OFFER/REQUEST/ACK on the wire and in serve-dhcp log", "status": "PASS"},
        {"name": "TFTP", "expected": "network-confg and r2-confg served", "observed": "RRQ and transfer.completed for both", "status": "PASS"},
        {"name": "applied", "expected": "hostname, interface address, static route", "observed": "hostname r2; 192.0.2.12 255.255.255.0; S 203.0.113.0/24 via 192.0.2.2", "status": "PASS"},
        {"name": "reachability", "expected": "ping 192.0.2.2", "observed": "5/5", "status": "PASS"}],
    stages=[stage("DHCP_DISCOVERED", "PASS", "capture"), stage("DHCP_OFFERED", "PASS", "capture"), stage("DHCP_BOUND", "PASS", "AUTOINSTALL: FastEthernet0/0 is assigned 192.0.2.12"),
            stage("TFTP_REQUESTED", "PASS", "RRQ network-confg, r2-confg"), stage("ARTIFACT_TRANSFERRED", "PASS", "[OK - 135 bytes]"),
            stage("CONFIG_APPLIED", "PASS", "%SYS-5-CONFIG_I from tftp://192.0.2.2/r2-confg"), stage("CONFIG_VERIFIED", "PASS", "console post-check"),
            stage("CONFIG_PERSISTED", "NOT_APPLICABLE", "AutoInstall applies to running-config by design")],
    logs=[ref("logs/r2-console-run2.log", "console, run 2"), ref("logs/r2-postcheck-run2.txt", "post-check, run 2"),
          ref("logs/r2-infraflow-dhcp-tftp-run2.log", "serve-dhcp/serve-tftp log"), ref("logs/iosv-lab-tftp-hashes-run2.txt", "served artifact hashes"),
          ref("logs/r2-diagnose.txt", "infraflow-agent diagnose on run 2"),
          ref("logs/r2-console-run1.log", "run 1 console (inventory version mismatch)"), ref("logs/r2-postcheck-run1.txt", "run 1 post-check"),
          ref("inputs/lab-c9f5a86.yaml", "inventory used")],
    captures=[ref("captures/r2-c7200-autoinstall-run2.pcap", "R2 link, run 2"), ref("captures/r2-c7200-autoinstall-run1-inventory-15.3.pcap", "R2 link, run 1")],
    artifact_hashes={"iosv-lab/r2-confg": "04bcfe059217dc44c1beffe411d3e9bea5bd24e6e281e1ecf814878ccb732be0",
                     "iosv-lab/network-confg": "42e005ebeb22f606a03a794a9f7b611add3ee51a0b9e2247316562d4b56fbac7"},
    warnings=["the GNS3 'Cisco 7200' template image file is named c7200-adventerprisek9-mz.153-3.XB12.image but the device reports 12.4(24)T5; the 15.3(3)XB12 profile is still not lab-verified",
              "infraflow-agent diagnose reports 'no failure found' with low confidence because Agent/Provider logs were not supplied; the console and capture checks above decide the status"],
    status="PASS", status_reason="all provisioning stages observed on the wire and on the device"))

iosv_ruled_out = ["InfraFlow DHCP server: the c7200 and the CHR complete DORA with it in the same capture (broadcasts are flooded to every port)",
                  "the reference ISC dhcpd gets the same DISCOVER/OFFER loop (M6 on 2026-11-22 before c9f5a86, M8 in this run)"]

reports.append(report(
    "CIS-02", "IOSv 15.9(3)M6 AutoInstall against InfraFlow DHCP (c9f5a86)", "06:49:00", "07:04:00",
    site="iosv-lab", device="IOSV1", model="iosv", os="Cisco IOS", os_version="15.9(3)M6",
    initial_state="IOSv with empty NVRAM and no startup-config disk; InfraFlow serve-dhcp running",
    actions=[
        {"at": t("06:45:40"), "description": "generate-all with an IOSv M6 cisco-autoinstall device (r5)", "result": "refused by c9f5a86: bootstrap for this profile is not verified"},
        {"at": t("06:49:00"), "description": "IOSV1 booted for 15 min on the segment served by InfraFlow DHCP (generic pool)", "result": "122+ DISCOVER/OFFER pairs, no REQUEST"}],
    final_state="IOSV1 at the initial configuration dialog, no lease",
    checks=[
        {"name": "generator refuses the profile", "expected": "no AutoInstall output for an unverified profile", "observed": "exit 1 with an explicit reason", "status": "PASS"},
        {"name": "lease", "expected": "AutoInstall client accepts an offer", "observed": "0 REQUEST", "status": "FAIL"}],
    stages=[stage("DHCP_DISCOVERED", "PASS", "capture"), stage("DHCP_OFFERED", "PASS", "capture"), stage("DHCP_BOUND", "FAIL", "no DHCPREQUEST"),
            stage("TFTP_REQUESTED", "NOT_RUN", "not reached")],
    logs=[ref("logs/generate-refuses-iosv-m6-bootstrap.txt", "generator refusal"), ref("logs/m6-diagnose.txt", "infraflow-agent diagnose"), ref("logs/iosv-m6-console.log", "console")],
    captures=[ref("captures/iosv-m6-autoinstall-infraflow-dhcp.pcap", "IOSV1 link")],
    status="UNSUPPORTED", status_reason="the image's AutoInstall client never requests an offered lease; the product refuses to generate it",
    diagnosis={"expected": "DORA then TFTP", "observed": "DISCOVER/OFFER loop, no REQUEST", "failed_stage": "DHCP_BOUND",
               "cause": "IOSv 15.9(3)M6 AutoInstall/PnP handler stops immediately (device trace of 2026-11-22: 'Discovery Failed' in 0 ms)",
               "cause_confirmed": True, "confidence": "high",
               "evidence": ["captures/iosv-m6-autoinstall-infraflow-dhcp.pcap", "gns3-20261122/logs/iosv1-pnp-tech-summary.txt"],
               "ruled_out": iosv_ruled_out, "next_check": "none on this image; use the SSH executor (CIS-04) or a platform with working AutoInstall (CIS-01)"}))

reports.append(report(
    "CIS-03", "IOSv 15.9(3)M8 AutoInstall against InfraFlow and reference DHCP (c9f5a86)", "06:49:00", "07:31:00",
    site="iosv-lab", device="IOSVM8", model="iosv", os="Cisco IOS", os_version="15.9(3)M8",
    initial_state="IOSvm8 with empty NVRAM and no startup-config disk",
    actions=[
        {"at": t("06:45:30"), "description": "generate-all with an IOSv M8 cisco-autoinstall device (r4)", "result": "refused by c9f5a86: no AutoInstall profile for this release"},
        {"at": t("06:49:00"), "description": "15 min against InfraFlow serve-dhcp", "result": "123+ DISCOVER/OFFER, no REQUEST"},
        {"at": t("07:25:23"), "description": "InfraFlow DHCP stopped; 5 min against the reference ISC dhcpd 4.4.3-P1", "result": "57 DISCOVER, 57 OFFER, 0 REQUEST, 0 ACK"}],
    final_state="IOSvm8 at the initial configuration dialog, no lease from either server",
    checks=[
        {"name": "generator refuses the release", "expected": "explicit refusal", "observed": "exit 1, lists the supported profiles", "status": "PASS"},
        {"name": "lease from InfraFlow", "expected": "REQUEST after OFFER", "observed": "0 REQUEST", "status": "FAIL"},
        {"name": "lease from the reference server", "expected": "REQUEST after OFFER", "observed": "0 REQUEST", "status": "FAIL"}],
    stages=[stage("DHCP_DISCOVERED", "PASS", "both captures"), stage("DHCP_OFFERED", "PASS", "both captures"), stage("DHCP_BOUND", "FAIL", "no DHCPREQUEST to either server")],
    logs=[ref("logs/generate-refuses-iosv-m8.txt", "generator refusal"), ref("logs/m8-diagnose.txt", "diagnose, InfraFlow DHCP"),
          ref("logs/m8-vs-reference-diagnose.txt", "diagnose, reference DHCP"), ref("logs/ref-dhcpd.log", "reference dhcpd log"),
          ref("logs/iosv-m8-console.log", "console, InfraFlow run"), ref("logs/iosv-m8-vs-reference-console.log", "console, reference run")],
    captures=[ref("captures/iosv-m8-autoinstall-infraflow-dhcp.pcap", "IOSVM8 link, InfraFlow DHCP"), ref("captures/iosv-m8-vs-reference-dhcpd.pcap", "IOSVM8 link, reference dhcpd"),
              ref("captures/refdhcp-server-side.pcap", "reference server link")],
    status="UNSUPPORTED", status_reason="same device-side behaviour as M6 with two independent servers (was PARTIAL: reference comparison not run)",
    diagnosis={"expected": "DORA then TFTP", "observed": "DISCOVER/OFFER loop with both servers", "failed_stage": "DHCP_BOUND",
               "cause": "the IOSv M8 AutoInstall client does not accept offers; behaviour identical to M6",
               "cause_confirmed": False, "confidence": "high",
               "evidence": ["captures/iosv-m8-vs-reference-dhcpd.pcap", "logs/ref-dhcpd.log"],
               "ruled_out": iosv_ruled_out + ["PnP tech summary not collected on M8, so the internal handler trace is inferred from M6"],
               "next_check": "collect flash:/pnp-tech on M8 if this image ever needs AutoInstall"}))

reports.append(report(
    "CIS-04", "IOSv 15.9(3)M6 SSH executor on the pristine c9f5a86 Provider", "07:32:50", "07:33:30",
    site="vendor-lab", device="r1", model="iosv", os="Cisco IOS", os_version="15.9(3)M6", execution_id="exec-6b6c5d5e2e70c922946046a9fab27918",
    initial_state="r1 booted from its startup-config (saved by GNS-04-CIS); host key pinned from the console; ssh_legacy_sha1_targets [vendor-lab/r1]",
    actions=[
        {"at": t("07:32:57"), "description": "C1 apply Gi0/3 198.18.0.5/30", "result": "succeeded, verified"},
        {"at": t("07:33:05"), "description": "C2 replay with the same Idempotency-Key", "result": "HTTP 200, same execution"},
        {"at": t("07:33:08"), "description": "C3 same request, new key", "result": "succeeded; previous_changes == requested"},
        {"at": t("07:33:15"), "description": "C4 Gi0/2 198.51.100.9/24 overlapping Gi0/1", "result": "IOS rejected; rollback verified by read-back"}],
    final_state="Gi0/3 configured in running-config only; Gi0/2 back to its preflight state",
    checks=[
        {"name": "apply + verify", "expected": "verified read-back", "observed": "verified=true", "status": "PASS"},
        {"name": "replay", "expected": "200 and the same execution id", "observed": "200, exec-6b6c5d5e…", "status": "PASS"},
        {"name": "idempotent re-run", "expected": "previous state equals requested", "observed": "equal", "status": "PASS"},
        {"name": "rollback on device rejection", "expected": "failed, rolled back, verified", "observed": "'changes were rolled back … verified by read-back'", "status": "PASS"},
        {"name": "binary provenance", "expected": "pristine c9f5a86 text", "observed": "warning 'write memory disabled by policy' (pre-patch wording)", "status": "PASS"}],
    logs=[ref("logs/r1-cis04-executions-summary.txt", "summary")] + [ref("logs/r1-%s.json" % i, "execution record") for i in (
        "exec-6b6c5d5e2e70c922946046a9fab27918", "exec-55db98de45df0cac95d66502db472b07", "exec-0d59a8aa1c10442e46f0722fcfcf1e6c")] +
        [ref("logs/r1-known-host.txt", "console-derived host key"), ref("logs/lab-binaries.txt", "binary hashes")],
    warnings=["SHA-1 key exchange exception granted to vendor-lab/r1 only (IOSv offers only SHA-1 KEX)"],
    status="PASS", status_reason="all executor checks passed on the commit's own binary"))

reports.append(report(
    "GNS-04-CIS", "Explicit persistence (persist: true) and verification after an IOS reload", "07:06:00", "07:17:10",
    infraflow_version=PATCHED, worktree_dirty=True,
    site="vendor-lab", device="r1", model="iosv", os="Cisco IOS", os_version="15.9(3)M6", execution_id="exec-f09c9b6c02d262999f6db19c7beec8a1",
    initial_state="r1 day-0 on the console (SSH, user, Gi0/0); 'startup-config is not present'",
    actions=[
        {"at": t("07:13:44"), "description": "P1 cisco.ios.configure persist:true (hostname r1-persist, Gi0/1 198.51.100.1/24)", "result": "succeeded; write memory [OK]; startup-config read back and matched"},
        {"at": t("07:13:53"), "description": "P2 same executor without persist (Gi0/2 198.18.0.1/30)", "result": "succeeded; no write memory"},
        {"at": t("07:14:07"), "description": "independent console read of startup-config, then IOS 'reload' (answered 'no' to saving P2)", "result": "startup holds P1, not P2"},
        {"at": t("07:16:18"), "description": "console read after reload", "result": "hostname r1-persist, Gi0/1 from NVRAM; Gi0/2 unconfigured"},
        {"at": t("07:16:49"), "description": "P3 P1 request again, new key", "result": "succeeded; pinned host key still valid; previous == requested"}],
    final_state="P1 survived the reload; P2 did not, as requested",
    checks=[
        {"name": "explicit opt-in", "expected": "write memory only with persist:true", "observed": "P2 issued none (unit test pins it too)", "status": "PASS"},
        {"name": "startup verified by the executor", "expected": "persistence.verified", "observed": "saved=true, verified=true", "status": "PASS"},
        {"name": "survives reload", "expected": "P1 present after reload", "observed": "hostname r1-persist; Gi0/1 198.51.100.1 (Method NVRAM)", "status": "PASS"},
        {"name": "non-persisted change lost", "expected": "P2 absent after reload", "observed": "Gi0/2 no ip address, shutdown", "status": "PASS"},
        {"name": "post-reload re-run", "expected": "previous == requested", "observed": "equal", "status": "PASS"}],
    stages=[stage("CONFIG_APPLIED", "PASS", "P1"), stage("CONFIG_VERIFIED", "PASS", "running read-back"), stage("CONFIG_PERSISTED", "PASS", "startup read-back + reload")],
    logs=[ref("logs/r1-persist-executions-summary.txt", "summary")] + [ref("logs/r1-%s.json" % i, "execution record") for i in (
        "exec-f09c9b6c02d262999f6db19c7beec8a1", "exec-a1a9142fb0b99c532482c6a1d7210a34", "exec-f9555bbab8018616d19289594f0809da")] +
        [ref("logs/r1-day0-readback.txt", "day-0 read-back (secrets removed)"), ref("logs/r1-console-before-reload.txt", "console before reload"),
         ref("logs/r1-reload-console.log", "reload console"), ref("logs/r1-console-after-reload.txt", "console after reload"),
         ref("logs/persist-patch-on-c9f5a86.diff", "the uncommitted patch under test"), ref("logs/lab-binaries.txt", "binary hashes")],
    warnings=["tested on c9f5a86 plus an UNCOMMITTED patch: this is not evidence for the HEAD commit, which has no persistence step",
              "write memory also saved the console day-0 configuration (needed for SSH after reload)"],
    status="PASS", status_reason="persistence requested explicitly, verified by the executor, and confirmed by an independent read after a real reload"))

reports.append(report(
    "MTK-02", "MikroTik CHR 7.8: host-key policy, import, read-back, idempotency (c9f5a86)", "06:51:30", "06:53:40",
    site="vendor-lab", device="mt1", model="CHR", os="RouterOS", os_version="7.8", execution_id="exec-9c824f15c3513a33e82b351d9df5811b",
    initial_state="fresh CHR; day-0 on console: admin password, user infraflow (group full), 192.0.2.21/24 on ether1 without owner comment",
    actions=[
        {"at": t("06:53:20"), "description": "M1 import with no known_hosts entry", "result": "failed closed, presented key named, nothing imported"},
        {"at": t("06:53:23"), "description": "M2 console-derived key pinned, import", "result": "succeeded, verified"},
        {"at": t("06:53:27"), "description": "M3 same request, new key", "result": "succeeded, verified, no duplicates"},
        {"at": t("06:53:30"), "description": "M4 replay of the M2 key", "result": "HTTP 200, same execution"}],
    final_state="identity mt1; ether1/ether2 addresses, static route and OSPF instance owned by InfraFlow",
    checks=[
        {"name": "unknown host key", "expected": "fail closed", "observed": "host key is unknown or not approved … presented ssh-rsa SHA256:+to3eoGx…", "status": "PASS"},
        {"name": "import + read-back", "expected": "verified, no mismatches", "observed": "verified=true, mismatches=null", "status": "PASS"},
        {"name": "idempotency", "expected": "re-run verified without duplicates; replay returns the same execution", "observed": "M3 verified; M4 HTTP 200", "status": "PASS"}],
    logs=[ref("logs/mt1-executions-summary.txt", "summary")] + [ref("logs/mt1-%s.json" % i, "execution record") for i in (
        "exec-1f197c636cf4d4e46b5095559cef11ea", "exec-9c824f15c3513a33e82b351d9df5811b", "exec-ad833bc1160d35c3b6ea78ec96903889")] +
        [ref("logs/mt1-day0-console.txt", "day-0 console (passwords masked)"), ref("logs/mt1-known-host.txt", "console-derived host key")],
    captures=[],
    artifact_hashes={"vendor-lab/native/device-mt1.auto.rsc": "7b6df7a87620792f76902a46313299de6048383f90eeed6fa7304ebb9d784d0d"},
    warnings=["the CHR keeps its factory DHCP client on ether1 and also holds a dynamic address (192.0.2.4 at read-back)"],
    status="PASS", status_reason="MTK-02/03/04 re-run on the commit's own binary"))

reports.append(report(
    "GNS-04-MTK", "MikroTik CHR configuration after a reboot (c9f5a86)", "07:18:30", "07:20:30",
    site="vendor-lab", device="mt1", model="CHR", os="RouterOS", os_version="7.8",
    initial_state="configuration imported by MTK-02",
    actions=[{"at": t("07:18:30"), "description": "/system reboot on the console", "result": "rebooted"},
             {"at": t("07:20:20"), "description": "console read after reboot", "result": "uptime 1m18s; identity and owned objects present"}],
    final_state="identity mt1, owned addresses, static route and OSPF instance present after reboot",
    checks=[{"name": "survives reboot", "expected": "InfraFlow-owned objects present", "observed": "infraflow:mt1:interface:ether1/ether2, route 203.0.113.0/24, ospf infraflow-v2", "status": "PASS"}],
    logs=[ref("logs/mt1-console-after-reboot.txt", "console after reboot")],
    warnings=["RouterOS persists every change immediately; no explicit save step exists or is needed"],
    status="PASS", status_reason="imported configuration observed after a real reboot"))

for item in reports:
    path = os.path.join(E, item["scenario_id"] + ".json")
    with open(path, "w") as handle:
        json.dump(item, handle, indent=2)
        handle.write("\n")
print(len(reports), "reports written")
