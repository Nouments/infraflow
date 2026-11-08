# FortiGate ZTP profile

**Evidence state:** the vendor documents separate ZTP workflows; no exact FortiGate model, FortiOS release, entitlement, or backend has been selected for this demo.

## Do not collapse these workflows

| Workflow | What InfraFlow may prepare | Required external owner/check |
| --- | --- | --- |
| FortiDeploy Cloud | Enrollment checklist and a reviewed cloud-template input | FortiGate Cloud account/entitlement, serial or product key registration, factory-reset state, Internet/DHCP access, and model eligibility |
| FortiManager ZTP | A DHCP-option declaration for options 240 and 241, after target details are supplied | DHCP server and isolated bootstrap network; FortiManager reachability, authorization, domain, and release-specific option behavior |
| FortiOS day-two configuration | A separate configuration template after interfaces, VDOM, and policy intent are known | Exact FortiGate model/FortiOS release, interface mapping, API access, and syntax/device validation |

FortiDeploy is not a local configuration file generator. FortiManager DHCP-option ZTP is not interchangeable with FortiDeploy. Neither workflow should be emitted as if it were supported by every FortiGate model.

## Inputs still required

- Exact appliance model and FortiOS patch release.
- Chosen backend: FortiDeploy Cloud or FortiManager.
- VDOM and management/WAN interface mapping.
- DHCP option ownership and values for the isolated provisioning network.
- Evidence that the selected model/release supports the chosen bootstrap path.

The current multi-vendor pilot renders a FortiOS day-two interface task only. It does not generate FortiDeploy enrollment or FortiManager DHCP options. Until the inputs above are supplied, this dossier is a source-backed design reference, not a deployable ZTP artifact. No ZTP device commands or DHCP configuration are generated here.

## Official sources

- [Fortinet FortiGate product family](https://www.fortinet.com/products/next-generation-firewall/fortigate)
- [FortiDeploy zero-touch provisioning](https://docs.fortinet.com/document/fortigate/6.4.16/administration-guide/316039/zero-touch-provisioning-with-fortideploy)
- [FortiManager ZTP and DHCP options 240/241](https://docs.fortinet.com/document/fortigate/6.4.16/administration-guide/861490/zero-touch-provisioning-with-fortimanager)
- [FortiOS 7.6.2 administration guide](https://docs.fortinet.com/product/fortigate/7.6.2)