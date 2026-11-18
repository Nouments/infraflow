# Vendor compatibility status

Generated text and successful transfer are not vendor compatibility evidence.

| Vendor / output | Current evidence | Compatibility status |
|---|---|---|
| Cisco IOS-XE `.cfg` | Provider generated and Agent staged the file. Its bytes matched the Provider copy. | Rendered and transferred; no parser, Cisco appliance, or hardware validation. Not applied. |
| MikroTik RouterOS `.auto.rsc` | Existing render-only template and tests. | Not exercised by this GNS3 run; model/release compatibility remains unknown. |
| FortiOS `.cfg` | Existing render-only template and tests. | Not exercised by this GNS3 run; no FortiGate parser or appliance validation. |
| Terraform | Generated files transferred and staged by the Agent. | No Terraform init, plan, or apply was run. |
| DHCP/DNS/TFTP/PXE/iPXE | Existing opt-in artifact generators. | No service was started; no end-to-end boot flow was tested. |

The test input used a declared Cisco IOS-XE profile to exercise native artifact
generation and transfer. This validates InfraFlow's artifact path only. The
result must not be described as IOS-XE syntax validation, configuration
deployment, Cisco compatibility, ZTP, or AutoInstall.
