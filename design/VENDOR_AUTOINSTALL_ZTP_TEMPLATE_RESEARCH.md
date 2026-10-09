# Vendor ZTP and Configuration Template Research

**Research date:** 2026-10-07  
**Status:** DESIGN PROPOSAL - NOT LAB-VERIFIED

This note records primary-source findings and a proposed contract for Cisco IOS XE, MikroTik RouterOS, and FortiGate. It is not evidence that any InfraFlow vendor capability is implemented or supported. The checked-in lab inventory only declares Cisco sample devices; it does not identify a real connected device, firmware release, or reachable provisioning network.

## Recommendation

Model two distinct phases instead of calling every vendor path "ZTP":

1. **Day-zero bootstrap** gets a factory/default device onto a trusted management network and reachable by the controller.
2. **Day-two desired-state configuration** configures interfaces, addresses, routes, firewall policy, SNAT, and DNAT through a vendor-specific Ansible connection/collection.

A bootstrap mechanism is not a substitute for an Ansible configuration adapter. The initial templates must require exact device model, OS version, bootstrap method, interface mapping, and address plan. Do not claim supported capability until rendered output and device behavior are tested on the exact target.

## Findings by Vendor

### Cisco IOS XE

- IOS XE ZTP is a first-boot path for a device without startup configuration. Cisco documents DHCP bootfile/option 67 providing an HTTP or TFTP URL to a Python script; the device obtains its bootstrap address and runs the script in Guest Shell. A Cisco Catalyst 9000 field guide describes the sequence and a Catalyst 9300 running 17.6.4 example.
- The Catalyst 9000 guide lists particular platform/software prerequisites (9300 and 9500 at 16.5.1a, 9400 at 16.6.2, and says C9600 is not supported in that guide). These are guide-specific historical minimums, not a general compatibility guarantee. The target model/release must be checked against its current release guide.
- Cisco AutoInstall is a separate legacy configuration-discovery flow. Cisco IOS XE System Management documentation describes it independently; it must not be emitted as a synonym for the ZTP Python/Guest Shell flow. AutoInstall discovery details and filename behavior need verification against the exact router/switch release selected for InfraFlow.
- Ansible uses `ansible.netcommon.network_cli` over SSH, `ansible_network_os: cisco.ios.ios`, and enable privilege escalation where needed. The published `cisco.ios` collection documents `ios_interfaces`, `ios_l3_interfaces`, `ios_static_routes`, `ios_command`, and `ios_config`. Resource modules provide `rendered`/`gathered`/`parsed` states; `ios_config` documents backups, diff, matching, and `save_when`.
- The research found no dedicated first-class NAT resource module in the checked `cisco.ios` index. The initial Cisco NAT template should therefore be a narrowly-scoped `ios_config` configuration section, or wait for a verified resource module. It must not generate an unbounded whole-device config. NAT syntax and feature availability still require validation against the exact IOS XE platform/release.

**Initial Cisco profile proposal:** `vendor=cisco`, `family=iosxe`, exact `model` and `software_version`; bootstrap method explicitly one of `cisco-ztp` or `cisco-autoinstall`; ZTP profile declares DHCP option 67 URL, script transport (`http` or `tftp`), Guest Shell prerequisite, and bootstrap interface. The day-two play uses `cisco.ios` with `network_cli`; interface/IP/routes use resource modules, NAT uses an explicit, reviewed config block.

### MikroTik RouterOS

- Current MikroTik documentation describes Netinstall as an installation/reinstallation utility, not as an ordinary network configuration API. A device must enter Etherboot/BOOTP mode on the same Layer-2 segment. Netinstall reformats the system drive and erases configuration and files.
- Linux `netinstall-cli -s <userscript>` installs a custom default configuration script. The script is retained and used after later configuration resets until replaced/removed. `-sm <modescript>` is a one-time first-boot script; the current manual requires RouterOS and Netinstall 7.22 or newer for it. These options are destructive/install-time workflows and must be opt-in, not silently triggered by normal `apply`.
- RouterOS configuration management also documents `.auto.rsc`: a script uploaded over FTP or SFTP is automatically executed when its filename has that suffix, and RouterOS creates an `.auto.log` with success/failure. Prefer SFTP; treat this as a separate, explicit bootstrap/automation path with a narrow upload directory and verified log retrieval.
- The Ansible `community.routeros` collection documents RouterOS API and SSH connections. `community.routeros.api_modify` supports check mode/diff and idempotent behavior for supported paths, but its own docs warn that path coverage is partial and under active development. Its default handling of absent entries is `ignore`; setting `remove` can delete configuration and must be restricted to a named, owned rule scope.
- RouterOS documentation distinguishes `srcnat` from `dstnat`. `masquerade` is intended for dynamic public interface addresses and clears related conntrack entries after interface/IP changes; static `src-nat` is appropriate when a fixed translated address is intended. `dstnat` forwards to a private address/port. NAT is IPv4-only in the current CLI reference, matches the first packet, and rule changes may require conntrack cleanup before the effect is visible.

**Initial MikroTik profile proposal:** `vendor=mikrotik`, `family=routeros`, exact hardware model and RouterOS release; bootstrap is explicitly `netinstall-script`, `netinstall-mode-script`, or `auto-rsc`, never a generic `ztp` boolean. The day-two profile pins `community.routeros` and its Python/API dependency; prefer API over CLI where the exact path is supported. Model NAT as explicit RouterOS records (`chain`, match selectors, `action`, translated address/ports, order/comment), and do not use broad `handle_absent_entries: remove` by default.

### Fortinet FortiGate / FortiOS

- FortiDeploy ZTP is a cloud-managed enrollment workflow, not a local Ansible template renderer. Fortinet's documented flow registers the serial/product key in FortiGate Cloud, prepares a basic cloud configuration template, connects a factory-reset FortiGate with a DHCP-configured interface to Internet, and lets it join the cloud service. The product key is invalidated after a successful join to prevent spoofing. It therefore depends on FortiCloud availability/account/product entitlement and the correct factory-reset state.
- FortiManager ZTP is a separate option for sites without Internet: FortiGate boots factory-reset with a DHCP client; DHCP option 240 conveys FortiManager IP and option 241 conveys its domain. FortiGate configures central management from the option and the FortiManager administrator authorizes it and installs a configuration. FortiGate documents ignoring a different manager address after the device has been configured (`config-touched=1`). Confirm option semantics with the actual FortiOS release before generating DHCP configuration.
- Fortinet's Ansible collection uses the FortiOS HTTPAPI plugin and token-based authentication. The current Ansible docs list `fortinet.fortios` 2.6.0 and ansible-core 2.16+; legacy `fortiosapi` is deprecated. The documented modules include `fortios_system_interface`, `fortios_firewall_policy`, `fortios_firewall_vip`, and `fortios_firewall_ippool`, with check-mode support.
- FortiOS has separate semantics for SNAT and DNAT. A firewall policy can enable SNAT and optionally reference an IP pool. DNAT uses a VIP object, which must then be attached as the destination of a firewall policy. VIPs can include external interface/address, mapped address, service/source filters and optional port forwarding. FortiOS documentation warns that VIP overlaps are not blocked by the configuration workflow; InfraFlow should preflight conflicting external interface/IP/port/protocol tuples.

**Initial Fortinet profile proposal:** `vendor=fortinet`, `family=fortios`, exact FortiGate model, FortiOS release, and VDOM. Bootstrap method explicitly `fortideploy-cloud` or `fortimanager-dhcp-options`; neither is represented as local Ansible bootstrap. Day-two Ansible uses HTTPS HTTPAPI with a narrowly scoped API token. Interface addresses use `fortios_system_interface`; SNAT is a firewall policy plus optional IP pool; DNAT is a VIP plus its allow policy. Policy IDs/order, VDOM, interface names, address objects, and management access must be explicit.

## Proposed Shared InfraFlow Template Contract

Keep common network intent separate from each generated native representation. A template selection key should include vendor, family, exact model, OS/release range, and configuration-template version. Bootstrap method is separate from day-two transport.

```yaml
vendor_template:
  id: "<vendor>:<family>:<model>:<template-version>"
  version: "<template-version>"
  target:
    vendor: "<vendor>"
    family: "<family>"
    model: "<exact-model>"
    os_version: "<tested-release-or-range>"
  bootstrap:
    method: "<vendor-specific-method>"
  ansible:
    core_minimum: "<version>"
    collections:
      - name: "<namespace.collection>"
        version: "<pinned-version>"
  addressing:
    management:
      interface: "<explicit-device-interface>"
      mode: static
      address: "<address/prefix>"
      gateway: "<gateway>"
      dns: []
  interfaces: []
  routes: []
  nat:
    source: []
    destination: []
```

Common semantic fields for NAT rules should make direction unambiguous and carry: stable rule ID/name, address family, source/destination CIDR or address-object references, ingress/egress interface, protocol, port ranges, translation mode (`interface`, `address`, `pool`, or `port-forward`), translated address/port, ordering, and whether hairpin behavior is requested. No defaults should create an allow-all policy or public exposure. Preserve the distinction between an address/subnet assigned to an interface and a NAT translation; they are not interchangeable.

The vendor adapter maps these semantics to its native API/CLI. Validation must reject missing interface bindings, malformed/overlapping pools, ambiguous rule order, unsupported NAT families, unsafe `allowaccess`, duplicate VIP tuples, and model/release combinations without evidence. Render/check/syntax-check should run before any apply. Secrets (Ansible Vault/API token/passwords) stay external to generated templates and manifests.

## Suggested First Template Scope

1. **Cisco IOS XE:** management + one explicit L3 interface/address + default/static route + one explicit SNAT overload rule; DNAT only after target NAT syntax and ACL/policy behavior are verified. Bootstrap artifact separate from the Ansible day-two play.
2. **MikroTik RouterOS:** management + explicit WAN/LAN interface mapping + DHCP or static WAN + LAN prefix + one `srcnat` masquerade rule; optional `dstnat` only as a separate, allowlisted request. Default firewall preservation is mandatory; do not reset/reinstall by default.
3. **FortiGate:** management/WAN/LAN addresses + default route + one allow policy with SNAT (interface NAT first; IP pool only when requested); DNAT uses separate VIP + policy. FortiDeploy/FortiManager enrollment remains an independently selected bootstrap backend.

This is a design target, not a claim that the listed features are supported or verified in InfraFlow. The example model `csr1000v` in the repo is sample input only; the lab inventory does not include MikroTik or FortiGate hardware/version information.

## Questions to Resolve Before Generating Applyable Templates

- Exact model and firmware/OS release for each real target (including IOS XE train, RouterOS release/architecture, FortiOS patch and VDOM mode).
- Which device is physically/virtually available first, and whether it is safe to factory-reset/reinstall for day-zero bootstrap testing.
- Cisco ZTP versus AutoInstall; for Fortinet, FortiDeploy Cloud versus FortiManager local. Who owns DHCP option 67/240/241 and the isolated bootstrap VLAN?
- Which interfaces are WAN, LAN, and management on each exact model; are those names stable between hardware variants?
- Addressing source of truth: static allocations/IPAM versus DHCP; per-site gateways, DNS, public NAT addresses/pools, and address ownership.
- Required NAT cases: outbound SNAT, static 1:1, DNAT/port forwarding, hairpin, IPv6/NAT64, and whether filtering policy is explicitly modeled.
- Ansible execution environment: ansible-core/collection versions, dependencies, controller-to-device reachability, credentials handling, backups, and validation evidence.

## Sources

### Cisco

- [ZTP on Catalyst 9000: prerequisites, platforms, DHCP option 67, HTTP/TFTP and Guest Shell](https://www.cisco.com/c/en/us/support/docs/switches/catalyst-9300-switch/220634-configure-and-troubleshoot-ztp-on-cataly.html)
- [Cisco DevNet: IOS XE ZTP and DHCP option 67](https://blogs.cisco.com/developer/device-provisioning-with-ios-xe-zero-touch-provisioning)
- [Cisco IOS XE 17.15 Programmability Guide: ZTP](https://www.cisco.com/c/en/us/td/docs/ios-xml/ios/prog/configuration/1715/b_1715_programmability_cg/m_1715_prog_ztp.html)
- [Cisco IOS XE 17.x System Management Guide: AutoInstall](https://www.cisco.com/c/en/us/td/docs/routers/ios/config/17-x/syst-mgmt/b-system-management/m_cf-autoinstall-0.html)
- [Ansible IOS platform connection and enable-mode guide](https://docs.ansible.com/projects/ansible/latest/network/user_guide/platform_ios.html)
- [Ansible cisco.ios collection](https://docs.ansible.com/projects/ansible/latest/collections/cisco/ios/index.html)
- [ios_l3_interfaces](https://docs.ansible.com/projects/ansible/latest/collections/cisco/ios/ios_l3_interfaces_module.html), [ios_static_routes](https://docs.ansible.com/projects/ansible/latest/collections/cisco/ios/ios_static_routes_module.html), [ios_config](https://docs.ansible.com/projects/ansible/latest/collections/cisco/ios/ios_config_module.html)

### MikroTik

- [RouterOS Netinstall](https://manual.mikrotik.com/docs/getting-started/installation-and-upgrade/netinstall/)
- [Netinstall for Linux: `-s` and `-sm` scripts](https://manual.mikrotik.com/docs/getting-started/installation-and-upgrade/netinstall/netinstall-linux/)
- [RouterOS Configuration Management and `.auto.rsc`](https://manual.mikrotik.com/docs/getting-started/configuration-management/)
- [RouterOS first-time configuration, IP and NAT](https://manual.mikrotik.com/docs/getting-started/first-time-configuration/)
- [RouterOS NAT details](https://manual.mikrotik.com/docs/firewall-and-quality-of-service/firewall/nat/)
- [Ansible community.routeros collection](https://docs.ansible.com/projects/ansible/latest/collections/community/routeros/index.html), [API guide](https://docs.ansible.com/projects/ansible/latest/collections/community/routeros/docsite/api-guide.html), [api_modify](https://docs.ansible.com/projects/ansible/latest/collections/community/routeros/api_modify_module.html)

### Fortinet

- [FortiDeploy zero-touch flow (FortiOS 6.4 guide)](https://docs.fortinet.com/document/fortigate/6.4.16/administration-guide/316039/zero-touch-provisioning-with-fortideploy)
- [FortiManager ZTP and DHCP options 240/241 (FortiOS 6.4 guide)](https://docs.fortinet.com/document/fortigate/6.4.16/administration-guide/861490/zero-touch-provisioning-with-fortimanager)
- [FortiOS 7.6.2 firewall policy](https://docs.fortinet.com/document/fortigate/7.6.2/administration-guide/656084/firewall-policy)
- [FortiOS 7.6.2 static SNAT](https://docs.fortinet.com/document/fortigate/7.6.2/administration-guide/898655/static-snat), [dynamic SNAT/IP pools](https://docs.fortinet.com/document/fortigate/7.6.2/administration-guide/029961/dynamic-snat), [destination NAT/VIPs](https://docs.fortinet.com/document/fortigate/7.6.2/administration-guide/728694/destination-nat), [VIP configuration](https://docs.fortinet.com/document/fortigate/7.6.2/administration-guide/443514/configuring-vips)
- [Ansible fortinet.fortios collection](https://docs.ansible.com/projects/ansible/latest/collections/fortinet/fortios/index.html), [HTTPAPI plugin](https://docs.ansible.com/projects/ansible/latest/collections/fortinet/fortios/fortios_httpapi.html), [system_interface](https://docs.ansible.com/projects/ansible/latest/collections/fortinet/fortios/fortios_system_interface_module.html), [firewall_policy](https://docs.ansible.com/projects/ansible/latest/collections/fortinet/fortios/fortios_firewall_policy_module.html), [firewall_vip](https://docs.ansible.com/projects/ansible/latest/collections/fortinet/fortios/fortios_firewall_vip_module.html), [firewall_ippool](https://docs.ansible.com/projects/ansible/latest/collections/fortinet/fortios/fortios_firewall_ippool_module.html)
