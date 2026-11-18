# Feature and evidence matrix

| Capability | Status | Evidence and boundary |
|---|---|---|
| Provider YAML validation and planning | Implemented | Existing unit and integration coverage; not expanded by the GNS3 pass. |
| Native configuration generation | Implemented as render-only output | Cisco IOS-XE output generated for `site-gns3`; no vendor parser or device was used. |
| Provider authenticated catalog and report service | Exercised | Provider ran in its appliance; Agent registered, fetched artifacts, and submitted a report. |
| Agent site-scoped transfer | Exercised in the GNS3 appliance run | One Agent and one site (`site-gns3`); broader multisite behavior is covered by the existing loopback integration test. |
| Artifact hash verification | Exercised | Successful report contains output hashes; Provider and Agent copies of both native artifacts matched byte-for-byte. |
| Native artifact local storage | Fixed and exercised | `native_device_config` and `native_manifest` are now accepted by the Agent filesystem state adapter. |
| Native configuration application | Not performed | Agent stages verified files; no device executor ran. |
| Cisco IOS/IOS-XE compatibility | Not verified | No Cisco GNS3 appliance or physical device was started or contacted. |
| Terraform execution | Not performed | Terraform files were transferred and staged only. |
| DHCP/TFTP/PXE/iPXE service operation | Not performed | Bootstrap services were not started. |
| GNS3 product integration | Not implemented | GNS3 was external test tooling only; no GNS3 API code or dependency was added. |
| Local GNS3 appliance topology | Tested | Provider and Agent Docker appliances connected through an isolated Ethernet switch; no Cloud/NAT. |
| Windows CI on this revision | Unverified | No Actions run covers the uncommitted source revision. |

This matrix reports observed behavior from the current pass. Historical docs may
describe earlier repository states and are not evidence for the current run.
