# GNS3 Provider–Agent acceptance results

## Topology and run

The local project contained the InfraFlow Provider Docker appliance, the
InfraFlow Agent Docker appliance, and one isolated Ethernet switch. Provider
and Agent were attached to the switch. No Cloud/NAT node or vendor router was
used.

| Check | Result | Evidence |
|---|---|---|
| Provider starts and exposes HTTPS API and TLS gRPC | PASS | [`provider-console.log`](../../artifacts/delivery/gns3/provider-console.log) |
| Agent reaches Provider network | PASS | Ping recorded in [`provider-agent-run.json`](../../artifacts/delivery/gns3/provider-agent-run.json) |
| Agent accesses Provider HTTPS endpoint | PASS, HTTP 200 | Same run record and Agent console log |
| Agent registers as `agent-gns3` for `site-gns3` | PASS, online | Same run record |
| Agent receives, verifies, and stages catalog artifacts | PASS, 13 completed | [`agent-report-summary.json`](../../artifacts/delivery/gns3/agent-report-summary.json) |
| Agent report is submitted and persisted by Provider | PASS | Report ID `db4385b701279fc45c870bb35dbd01402dedd351db51ac4ebe91113b861d661e` |
| Native configuration and manifest match Provider bytes | PASS | SHA-256 values below; comparison was run between the two appliance filesystems. |
| Device configuration applied | NOT RUN | No Cisco device or other router was started. |
| Bootstrap service or Terraform execution | NOT RUN | Files were staged only; no service/tool execution. |

The successful report has 13 artifacts: 13 completed, zero failed, zero
blocked. Its timestamp is `2026-11-18T19:20:27Z`.

| Native artifact | SHA-256 |
|---|---|
| `device-cisco-r1.cfg` | `da595d1de68d97cf34c3cb11dfd7bbfdae48739bcb53bfc6ec394c0c00264c8e` |
| `native-manifest.json` | `61ab91191e6aa0cab720b2625441bca088ccb754fced288b96484b5b68c9d891` |

## Defect found and fix

The first real run transferred ordinary artifacts but rejected
`native_device_config` and `native_manifest` because the Agent filesystem
allowlist omitted those verified artifact types. The failure is retained in
[`initial-agent-sync-failure.log`](../../artifacts/delivery/gns3/initial-agent-sync-failure.log).

The Agent allowlist now accepts those types under its existing safe path and
hash checks. Regression tests cover saving and reopening both native artifact
types and generic staging without execution. The rebuilt Agent appliance
passed the repeat run; see the successful report and
[`agent-console.log`](../../artifacts/delivery/gns3/agent-console.log).

## Limits

This is a real Provider–Agent appliance integration test, not a Cisco device
test. The `.cfg` file was rendered for a declared IOS-XE profile but was not
parsed by Cisco software or applied to an IOS/IOS-XE node. It does not verify
vendor syntax or compatibility. No ZTP, AutoInstall, DHCP, TFTP, PXE, Terraform,
or device configuration execution was performed.
