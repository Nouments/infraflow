# Local hardware catalog

Equipment records are embedded from `data/*.yaml` and searchable through
`catalog.Load`, `Catalog.FindByID`, and `Catalog.Search`. Each record separates
known values from unknown/unverified values and records the official source and
check date.

The initial Cisco ISR 1100 record is intentionally partial. Its official Cisco
product-family URL was reachable on 2026-10-26, but exact SKU details, supported
IOS XE versions, interfaces, and hardware capabilities were not verified. No
datasheet or image was downloaded. A URL with `reference_only` status is not a
local file.

The device-configuration generator checks catalog/template references and
exact declared manufacturer, model, and OS family before rendering. The
template is labelled `render-only`: successful resolution is not evidence that
the generated commands are supported by or have been applied to physical
hardware. Do not upgrade the record's validation status or add ports,
features, model aliases, OS versions, datasheets, or images without a
verifiable source and an explicit validation status.

The current renderer supports only the IOS XE interface fields listed in
`templates/vendors/cisco/iosxe/isr-1100/metadata.yaml`. OSPF, VLANs, static
routes, and additional models are rejected until a matching template and
documented compatibility evidence are available.
