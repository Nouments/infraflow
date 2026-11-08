# Equipment pilot dossier

This folder is the evidence index for template work focused on `demo-output`. Each fact sheet separates official product information from InfraFlow rendering assumptions. An official product page does not by itself prove a template works on a specific device.

| Vendor | Pilot target | Generation target | Evidence state |
| --- | --- | --- | --- |
| Cisco | ISR 1100 family; exact SKU not selected | Existing IOS XE `.cfg` render | Partial catalog record; render-only |
| MikroTik | hEX RB750Gr3 | RouterOS 6/7 `.rsc`; optional `.auto.rsc` suffix | Hardware facts source-backed; target release and command validation unknown |
| Fortinet | FortiGate; exact model not selected | FortiManager ZTP declaration or FortiDeploy enrollment plan | ZTP workflows documented; model/backend eligibility not confirmed |

## Boundaries

- Address values in demo artifacts use documentation-only ranges and must not be used as live allocations.
- Interface names in examples are explicit demo inputs, not inferred hardware mappings.
- RouterOS 6 and 7 need separate reviewed outputs. Similar command spelling is not proof of version compatibility.
- FortiDeploy is a cloud enrollment workflow. FortiManager ZTP uses a separately managed DHCP path. Neither is represented as a generic FortiOS `.cfg` template.
- Exact firmware, bootstrap network, interfaces, licensing/entitlement, and target access must be captured before a profile can move beyond `render-only`.
- Datasheets remain linked to their publishers; this repository does not redistribute vendor PDFs.

## Fact sheets

- [Cisco ISR 1100](cisco/isr-1100.md)
- [MikroTik hEX RB750Gr3](mikrotik/hex-rb750gr3.md)
- [FortiGate ZTP profile](fortinet/fortigate-ztp.md)

The broader vendor research, including AutoInstall and the documented bootstrap prerequisites, is in [VENDOR_AUTOINSTALL_ZTP_TEMPLATE_RESEARCH.md](../../design/VENDOR_AUTOINSTALL_ZTP_TEMPLATE_RESEARCH.md).