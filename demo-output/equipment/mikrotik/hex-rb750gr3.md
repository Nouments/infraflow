# MikroTik hEX RB750Gr3

**Evidence state:** model-level product facts are source-backed; RouterOS release compatibility and generated command syntax remain unverified.

## Source-backed notes

- MikroTik identifies the product code as `RB750Gr3`, marketed as hEX.
- The official product page lists five 10/100/1000 Ethernet ports, MMIPS architecture, 256 MB RAM, and RouterOS.
- The product page's current download listing is dynamic. This dossier does not pin a RouterOS 6 or RouterOS 7 release and does not claim both releases are validated on a particular unit.
- RouterOS documentation describes plain-text `.rsc` import and automatic execution of uploaded files ending in `.auto.rsc`; execution status is written to a corresponding `.auto.log`.

## Template targets

The current pilot emits a text-only RouterOS configuration, not a native `.rsc`/`.auto.rsc` AutoInstall artifact. Create distinct RouterOS 6 and RouterOS 7 provisioning targets only after the actual router release is collected. The demo must supply explicit interface bindings and addresses. Do not assume WAN/LAN roles from port numbers or infer live configuration from the product datasheet.

`.auto.rsc` is an upload-triggered execution path, not a harmless file extension. It must remain opt-in, use an approved transfer path, and include a plan to retrieve and inspect `.auto.log`. Do not generate reset or Netinstall commands as part of ordinary configuration generation.

## Official sources

- [MikroTik hEX product page and documents](https://mikrotik.com/product/RB750Gr3)
- [RouterOS Configuration Management: import, auto import, and reset cautions](https://manual.mikrotik.com/docs/getting-started/configuration-management/)
- [RouterOS Netinstall](https://manual.mikrotik.com/docs/getting-started/installation-and-upgrade/netinstall/)