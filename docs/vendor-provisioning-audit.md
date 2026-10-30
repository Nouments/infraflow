# Vendor provisioning audit

**Audit date:** 2026-10-30  
**Scope:** repository state on `develop`, official-source review, and the
requested Cisco IOS XE, MikroTik RouterOS, and FortiOS provisioning paths.
**Evidence rule:** generated text and mock tests are not proof of device
compatibility. No capability below is marked `IMPLEMENTED` or `LAB-VERIFIED`
unless an actual implementation and matching evidence exist.

## Current-state summary

InfraFlow already has a common network declaration, planning, native text
rendering, artifact manifests, authenticated publication, and hash-checked
Agent transfer. It does not have the requested vendor-specific bootstrap
generators or MikroTik/FortiOS API executors. Most multi-vendor native output
is family-level render-only text; it is not selected by exact model and
software release and is not proof of accepted device syntax.

The current Agent stages verified files and processes the generic inventory,
topology, and manifest formats. It does not execute a vendor configuration
just because that configuration was downloaded. The Provider currently
registers `artifact.verify` and a Cisco IOS SSH executor; this does not
constitute Cisco ZTP or AutoInstall.

## Capability matrix

| Feature | Current state | Target state | Relevant existing areas |
|---|---|---|---|
| Common network model | `NetworkDeployment` and the domain network types describe vendor-independent device/interface/route intent. Validation and rendering exist, with feature limits. | Keep desired state independent of vendor; validate each declared intent before adapter selection. | `internal/domain/`, `internal/adapters/config/`, `internal/adapters/generation/declarative.go` |
| Vendor catalogs | Embedded equipment catalog includes a partial Cisco ISR 1100 record. Equipment dossiers include a MikroTik hEX reference and a FortiGate ZTP reference. These do not form a verified model/release capability matrix. | Exact model, software release, requested features, method, template version, and source-backed evidence required for selection. | `internal/adapters/catalog/`, `internal/domain/template_registry.go`, `demo-output/equipment/` |
| Native templates | Declarative catalog/template selection exists for the Cisco ISR 1100 partial profile. A separate native renderer emits Cisco, RouterOS 7, and FortiOS interface/route text, but marks compatibility unknown and output render-only. | Select native templates using exact model, OS release, method, feature coverage, template version, and documented provenance; reject unknown combinations. | `internal/adapters/generation/`, `internal/adapters/generation/templates/vendors/` |
| Cisco ZTP | Generic DHCP/bootstrap output and an explicitly started bootstrap HTTP service exist. No Cisco ZTP script, option-67 profile, or ZTP artifact workflow is implemented. | Model the documented first-boot/DHCP/script/Guest Shell flow separately from device configuration; emit only justified artifacts and report outcomes. | `internal/adapters/generation/`, `agent/internal/delivery/{dhcp,artifacts}/`, `pkg/protocol/` |
| Cisco AutoInstall | Not implemented. No AutoInstall-specific file discovery profile or artifact type exists. | A separate, model/release-evidenced backend; never reuse the ZTP bootstrap implicitly. | `internal/adapters/generation/`, `pkg/protocol/`, `provider/internal/` |
| MikroTik API | RouterOS family is recognized and text rendering exists. No typed RouterOS API client or operation-plan executor exists. | Typed allowlisted operations over authenticated TLS, with preflight, read-back, idempotency controls, and mock coverage. | `internal/domain/`, `provider/internal/adapters/executors/`, `agent/internal/` |
| MikroTik `.auto.rsc` | No RouterOS-native `.auto.rsc` generator, publication type, or `.auto.log` verification flow exists. | An explicit, versioned upload-triggered path, separate from API and Netinstall; SFTP preferred and log read-back required. | `internal/adapters/generation/`, `pkg/protocol/`, `agent/internal/`, `provider/internal/` |
| MikroTik Netinstall | Not implemented. | Separate, explicitly authorized installation/reinstallation workflow with destructive-operation confirmation and preconditions; never an ordinary apply action. | `provider/internal/`, `agent/internal/`, `pkg/protocol/` |
| FortiGate ZTP | Neither FortiDeploy enrollment nor FortiManager DHCP-option ZTP is generated. The repository has an explanatory FortiGate workflow dossier. | Separate Cloud enrollment and FortiManager workflows; backend, model/release, ownership, DHCP values, and authorization must be explicit. | `internal/adapters/generation/`, `agent/internal/delivery/dhcp/`, `pkg/protocol/` |
| FortiOS API | FortiOS family-level text rendering exists. No FortiOS HTTPS API client, typed operations, token integration, or device read-back executor exists. | External token, verified TLS, explicit VDOM, typed allowlisted operations, policy-order-aware diff, and read-back verification. | `provider/internal/adapters/executors/`, `internal/domain/`, `agent/internal/` |
| Vendor DHCP options | Generic DHCP bootstrap configuration exists; vendor options 67, 240, and 241 are not modeled as vendor provisioning contracts. | Generate only options required by a selected method and target, with exact format/ownership validation and documented references. | `internal/adapters/generation/templates/bootstrap/dhcp/`, `agent/internal/delivery/dhcp/` |
| Artifact distribution | Provider manifests include paths, hashes, sizes, and capability summaries. Native device entries additionally carry additive site/device/vendor/template/input/status provenance; the Agent validates it. Catalog publication validates paths, size, and SHA-256. Agent transfer is authenticated, site-scoped, and hash-checked before atomic storage. | Preserve these controls; extend vendor artifact metadata and handlers without making download imply execution. | `pkg/protocol/`, `internal/adapters/generation/`, `provider/internal/adapters/filesystem/`, `agent/internal/application/runner.go` |
| Apply and verification | Planning is non-executing; generic agent processing stages artifacts. Provider has artifact verification and a Cisco IOS SSH configuration executor with read-back. Other vendors have no apply/verification handler. | Explicit authorization, preflight, preview, apply, read-back, verify, and traceable report per supported method. | `provider/internal/application/`, `provider/internal/adapters/executors/`, `agent/internal/application/`, `internal/application/reconcile/` |

### Architecture implications

- Keep the Provider generator as the owner of desired-state validation,
  capability resolution, vendor-native output, and manifest construction.
- Keep artifact paths, hashes, size bounds, site scoping, and authenticated
  transfer in the existing publication/Agent pipeline.
- Add vendor executors as separate typed adapters; do not make a generic
  command-string runner or a downloaded configuration auto-applying.
- Keep the common network schema independent of Cisco CLI, RouterOS script/API,
  and FortiOS API representations.
- Do not add or restore Ansible. It is absent from the implementation and
  excluded by the current architecture.
- Do not publish a profile as supported based on family matching alone.
  Until exact model/release evidence and required tests exist, report the
  capability as unknown or render-only.

## Official references consulted

Consultation date for the following pages: **2026-10-30**. The links are
references, not locally downloaded vendor manuals. No third-party documents
were copied into the repository.

| Vendor | Official source and version | Information reviewed | Scope/limitations |
|---|---|---|---|
| Cisco | [IOS XE 17.15 Programmability Configuration Guide: ZTP](https://www.cisco.com/c/en/us/td/docs/ios-xml/ios/prog/configuration/1715/b_1715_programmability_cg/m_1715_prog_ztp.html) | ZTP/ownership bootstrapping documentation; page retrieval included voucher material. | This fetch did not establish a complete model/release support matrix or validate a generated script. |
| Cisco | [Configure and troubleshoot ZTP on Catalyst 9000](https://www.cisco.com/c/en/us/support/docs/switches/catalyst-9300-switch/220634-configure-and-troubleshoot-ztp-on-cataly.html) | Catalyst 9300/9500/9400 prerequisite examples, no-startup-config context, DHCP option 67 URL, HTTP script download, Guest Shell, and example execution flow. | The page scopes its listed platform/software facts to particular Catalyst models/releases and its lab example; it is not a general IOS XE compatibility guarantee. |
| Cisco | [IOS XE 17.x System Management Guide: AutoInstall](https://www.cisco.com/c/en/us/td/docs/routers/ios/config/17-x/syst-mgmt/b-system-management/m_cf-autoinstall-0.html) | AutoInstall material, including DHCP client-identifier and interface-specific setup examples. | The retrieved section does not verify a universal model-specific file-discovery profile. AutoInstall remains unimplemented. |
| MikroTik | [RouterOS API](https://manual.mikrotik.com/docs/developer-guides/api/) | API framing/replies, login behavior, service enablement, and API-SSL certificate requirement. | The page describes the protocol, not an InfraFlow client implementation or operation coverage. |
| MikroTik | [RouterOS Configuration Management](https://manual.mikrotik.com/docs/getting-started/configuration-management/) | Official manual reviewed as the source for configuration import/management; existing equipment dossier identifies `.auto.rsc` and `.auto.log` as a separate workflow. | Exact release/model behavior and SFTP execution have not been tested in InfraFlow or on a device. |
| MikroTik | [Netinstall](https://manual.mikrotik.com/docs/getting-started/installation-and-upgrade/netinstall/) | Reinstallation utility, same-L2/Etherboot workflow, and warning that reformatting erases configuration and files. | Do not treat as day-two configuration; no Netinstall operation is implemented. |
| MikroTik | [Netinstall for Linux](https://manual.mikrotik.com/docs/getting-started/installation-and-upgrade/netinstall/netinstall-linux/) | Linux tooling documentation, including configuration preservation caveat when neither `-r` nor `-e` is specified. | The reviewed content does not establish support for destructive script flags across all releases; no flags are invoked by InfraFlow. |
| Fortinet | [FortiOS 7.6.2 Administration Guide](https://docs.fortinet.com/document/fortigate/7.6.2/administration-guide) | Official guide entry point and product/version context. | Retrieval redirected to a getting-started page; no FortiOS API operation contract was verified in this audit. |
| Fortinet | [Zero touch provisioning with FortiManager](https://docs.fortinet.com/document/fortigate/6.4.16/administration-guide/861490/zero-touch-provisioning-with-fortimanager) | Factory-reset context, DHCP options 240/241, FortiManager authorization/install step, and behavior after configuration has been touched. | The page redirected to the FortiOS 6.4.0 guide; confirm against the exact target release before implementation. |
| Fortinet | [Zero-touch provisioning with FortiDeploy](https://docs.fortinet.com/document/fortigate/6.4.16/administration-guide/316039/zero-touch-provisioning-with-fortideploy) | Official source URL recorded for the distinct FortiDeploy Cloud workflow. | Retrieval returned a feedback page rather than the procedure; details must be rechecked from accessible official documentation before implementing it. |

## Validation and evidence

- Cisco pilot selected for the next implementation slice: Catalyst 9300 with
  IOS XE 17.6.4, ZTP, render-only. The consulted Catalyst guide documents
  DHCP option 67 with an HTTP script URL; TLS/HTTPS support for this exact
  bootstrap download was not confirmed. Per the requested TLS-only rule, no
  Cisco ZTP script or DHCP option artifact is generated until an official
  source confirms an acceptable secure transport for this target.
- Baseline `go test ./...` passed on 2026-10-30 before documentation changes.
- The additive native-artifact provenance contract was implemented and its
  focused tests passed. It remains render-only and does not change the
  protobuf/gRPC contract. Documentation-reference lists are present but empty
  for current family-level native outputs because their command syntax has not
  been tied to a source-backed, exact model/release template.
- Existing tests cover config parsing/validation, native generation,
  manifests/catalog publication, artifact path and hash validation, Agent
  processing, bootstrap services, Cisco executor behavior, and multisite
  transfer. They do not prove physical-device compatibility.
- Existing documented live Cisco/GNS3 validation remains `NON EXÉCUTÉ`.
- No MikroTik or FortiGate device was contacted; no generated candidate was
  tested against vendor syntax or hardware.
- Exact target models, firmware releases, interfaces, bootstrap ownership, and
  provisioning backend are still required before producing model/release
  compatible deployment artifacts.
