package domain

import "strings"

// BootstrapProfile is the explicit selection result for vendor bootstrap
// methods. It describes a mechanism and its operational gates; it does not
// imply that the target device has accepted a generated configuration.
type BootstrapProfile struct {
	Vendor            string             `json:"vendor"`
	Family            string             `json:"family"`
	Model             string             `json:"model"`
	OSVersion         string             `json:"os_version"`
	Method            ProvisioningMethod `json:"method"`
	Mechanism         string             `json:"mechanism"`
	State             CapabilityState    `json:"state"`
	RequiredServices  []string           `json:"required_services,omitempty"`
	RequiredArtifacts []string           `json:"required_artifacts,omitempty"`
	Verification      []string           `json:"verification,omitempty"`
	Evidence          []string           `json:"evidence,omitempty"`
	Reason            string             `json:"reason"`
}

// BootstrapProfileFor returns a method-specific profile for recognized
// bootstrap methods. Unknown methods return false; known but unavailable
// methods return a profile with an explicit unsupported state and reason.
func BootstrapProfileFor(device Device) (BootstrapProfile, bool) {
	vendor := strings.ToLower(strings.TrimSpace(device.Vendor))
	family := strings.ToLower(strings.TrimSpace(device.Family))
	model := strings.ToLower(strings.TrimSpace(device.Model))
	version := strings.TrimSpace(device.OSVersion)
	method := ProvisioningMethod(strings.ToLower(strings.TrimSpace(string(device.Provisioning.Method))))
	profile := BootstrapProfile{
		Vendor: vendor, Family: family, Model: strings.TrimSpace(device.Model),
		OSVersion: version, Method: method, State: CapabilityUnsupported,
	}
	switch method {
	case ProvisioningMethodCiscoAutoInstall:
		profile.Mechanism = "Cisco IOS AutoInstall over DHCP and TFTP"
		profile.RequiredServices = []string{"DHCP", "TFTP"}
		profile.RequiredArtifacts = []string{"network-confg", "<hostname>-confg"}
		profile.Verification = []string{"show version", "show running-config", "show ip interface brief"}
		profile.Evidence = []string{"Cisco IOS 15.x AutoInstall guide", "Cisco IOS 7200 GNS3 appliance profile"}
		if vendor == "cisco" && family == "ios" {
			if entry, ok := CiscoIOSAutoInstallProfileFor(model, version); ok {
				profile.State = entry.State
				profile.Reason = entry.Reason
				profile.Evidence = append(profile.Evidence, entry.Evidence...)
				return profile, true
			}
		}
		profile.Reason = "Cisco AutoInstall is implemented only for exact cisco/ios model and release profiles (" + CiscoIOSAutoInstallProfileList() + "); no fallback mechanism is selected."
		return profile, true

	case ProvisioningMethodCiscoZTP, ProvisioningMethodCiscoIOSXEZTP:
		profile.Mechanism = "Cisco IOS XE Zero-Touch Provisioning"
		profile.RequiredServices = []string{"DHCP", "HTTPS bootstrap script service"}
		profile.RequiredArtifacts = []string{"vendor-specific ZTP script", "DHCP option 67 URL"}
		profile.Evidence = []string{"Cisco IOS XE 17.18 ZTP guide"}
		if vendor == "cisco" && (family == "iosxe" || family == "ios-xe" || family == "ios_xe") &&
			(model == "c9350" || model == "c9550" || model == "c9610") && version == "17.18.1" {
			profile.Reason = "Cisco documents ZTP for this target family, but InfraFlow has no secure script execution and result-reporting path for this profile."
			return profile, true
		}
		profile.Reason = "Cisco ZTP requires an exact supported IOS XE platform and release plus a secure script workflow; no AutoInstall fallback is selected."
		return profile, true

	case ProvisioningMethodCiscoIOSXEAutoInstall:
		profile.Mechanism = "Cisco IOS XE AutoInstall"
		profile.RequiredServices = []string{"DHCP", "TFTP"}
		profile.RequiredArtifacts = []string{"network-confg", "<hostname>-confg"}
		profile.Evidence = []string{"Cisco IOS XE 17.18 Provisioning guide; AutoInstall fallback behavior"}
		if vendor == "cisco" && (family == "iosxe" || family == "ios-xe" || family == "ios_xe") {
			profile.State = CapabilityPartial
			profile.Reason = "DHCP/TFTP AutoInstall files and options are implemented for the IOS XE family; exact model and release compatibility still requires target verification."
			return profile, true
		}
		profile.Reason = "Cisco IOS XE AutoInstall requires an IOS XE family declaration; the IOS 7200 IOS profile is not reused."
		return profile, true

	case ProvisioningMethodAutoRSC, ProvisioningMethodMikroTikRouterOS7AutoRSC:
		profile.Mechanism = "MikroTik RouterOS .auto.rsc Auto Import"
		profile.RequiredServices = []string{"SFTP or FTP upload"}
		profile.RequiredArtifacts = []string{"<device-id>.auto.rsc", "<device-id>.auto.log"}
		profile.Evidence = []string{"MikroTik RouterOS Configuration Management"}
		if vendor == "mikrotik" && family == "routeros" && (model == "rb750gr3" || model == "chr") && strings.HasPrefix(version, "7.") {
			profile.State = CapabilityExperimental
			profile.Reason = "RouterOS template selection is exact by model and v7 train; InfraFlow renders the script but does not upload it or read .auto.log."
			if model == "chr" && version == "7.8" {
				profile.Reason = "The rendered script executed via Auto Import on a GNS3 CHR running RouterOS 7.8 (identity, addresses, static route, OSPF; idempotent re-run; persisted across reboot), but the FTP upload and .auto.log retrieval were performed by the lab harness: InfraFlow does not yet upload it or read .auto.log."
				profile.Evidence = append(profile.Evidence, "artifacts/qa/mikrotik-report.md (2026-11-19 lab run)")
			}
			return profile, true
		}
		profile.Reason = "RouterOS Auto Import is limited to the RB750Gr3 v7 template and still requires an SFTP/FTP transfer and .auto.log verification path."
		return profile, true

	case ProvisioningMethodMikroTikNetinstall:
		profile.Mechanism = "MikroTik Netinstall"
		profile.RequiredServices = []string{"Netinstall host on an isolated Layer-2 segment"}
		profile.RequiredArtifacts = []string{"architecture-matched RouterOS packages", "explicit install script"}
		profile.Reason = "Netinstall is a destructive reinstall workflow and has no authorized installer path in InfraFlow."
		return profile, true

	case ProvisioningMethodFortiManagerZTP, ProvisioningMethodFortiOSFortiManager:
		profile.Mechanism = "FortiGate FortiManager ZTP"
		profile.RequiredServices = []string{"DHCP options 240 and 241", "FortiManager authorization and installation"}
		profile.RequiredArtifacts = []string{"FortiManager IP option", "FortiManager domain option"}
		profile.Verification = []string{"diagnose fdsm fmg-auto-discovery-status", "show system central-management"}
		profile.Evidence = []string{"FortiOS 6.4 ZTP with FortiManager guide"}
		if vendor == "fortinet" && family == "fortios" {
			profile.State = CapabilityPartial
			profile.Reason = "FortiManager discovery is implemented with DHCP options 240/241; FortiManager must authorize the serial number and install the device configuration."
			return profile, true
		}
		profile.Reason = "FortiManager ZTP requires a FortiOS device declaration and the FortiManager discovery DHCP profile."
		return profile, true

	case ProvisioningMethodFortiDeployCloud, ProvisioningMethodFortiOSFortiDeploy:
		profile.Mechanism = "FortiGate FortiDeploy cloud enrollment"
		profile.RequiredServices = []string{"FortiGate Cloud entitlement", "internet access", "factory-reset device"}
		profile.RequiredArtifacts = []string{"authorized product-key enrollment", "cloud configuration template"}
		profile.Evidence = []string{"FortiOS FortiDeploy ZTP guide"}
		profile.Reason = "FortiDeploy depends on an entitled external cloud account and is not available as an offline InfraFlow bootstrap service."
		return profile, true
	}
	return BootstrapProfile{}, false
}

// OSPFProfileFor reports code-level rendering support for a declared OS family.
// A PARTIAL state means the command template exists but has not been validated
// against the target model/release or a live neighbor session.
func OSPFProfileFor(device Device) (BootstrapProfile, bool) {
	vendor := strings.ToLower(strings.TrimSpace(device.Vendor))
	family := strings.ToLower(strings.TrimSpace(device.Family))
	version := strings.TrimSpace(device.OSVersion)
	profile := BootstrapProfile{
		Vendor: vendor, Family: family, Model: strings.TrimSpace(device.Model), OSVersion: version,
		Method: "ospf", Mechanism: "OSPFv2 native configuration", State: CapabilityUnsupported,
		RequiredArtifacts: []string{"router ID", "static IPv4 interface networks", "OSPF area assignments"},
		Verification:      []string{"show ip ospf neighbor", "/routing ospf neighbor print", "get router info ospf neighbor"},
	}
	switch {
	case vendor == "cisco" && (family == "iosxe" || family == "ios-xe" || family == "ios_xe"):
		profile.State = CapabilityPartial
		profile.Evidence = []string{"Cisco IOS XE OSPF configuration guide"}
		profile.Reason = "IOS XE OSPF configuration generation is implemented; exact model/release syntax and neighbor formation remain unverified."
	case vendor == "cisco" && family == "ios" && (strings.EqualFold(device.Model, "7200") || strings.EqualFold(device.Model, "Cisco 7200")):
		profile.State = CapabilityPartial
		profile.Evidence = []string{"Cisco IOS OSPF configuration guide", "exact IOS 7200 AutoInstall template"}
		profile.Reason = "IOS OSPF configuration generation is implemented for the registered 7200 profile; syntax and live neighbor formation remain unverified."
	case vendor == "mikrotik" && family == "routeros" && strings.HasPrefix(version, "7."):
		profile.State = CapabilityPartial
		profile.Evidence = []string{"MikroTik RouterOS v7 OSPF documentation"}
		profile.Reason = "RouterOS v7 OSPF instance, area and interface-template generation is implemented; model and neighbor behavior remain unverified."
	case (vendor == "fortinet" || vendor == "fortigate") && (family == "fortios" || family == "fortigate"):
		profile.State = CapabilityPartial
		profile.Evidence = []string{"FortiOS OSPF CLI reference"}
		profile.Reason = "FortiOS OSPF router, area and network generation is implemented; model/release syntax and live neighbor formation remain unverified."
	default:
		profile.Reason = "No native OSPF renderer matches this vendor/OS family declaration."
		return profile, true
	}
	return profile, true
}

// Cisco 7200 IOS versions accepted for AutoInstall. The lab-verified version is
// the one the GNS3 Dynamips 7200 appliance reports in "show version"; its image
// file is named after 15.3(3)XB12, which remains accepted but unverified.
const (
	CiscoIOS7200LabVerifiedVersion = "12.4(24)T5"
	CiscoIOS7200DeclaredVersion    = "15.3(3)XB12"
	// CiscoIOSvVersion is the IOSv (virtual IOS) release shipped as
	// vios-adventerprisek9-m.spa.159-3.m6.
	CiscoIOSvVersion = "15.9(3)M6"
)

// CiscoIOSAutoInstallProfile is one exact model/release pair for which the
// Cisco IOS AutoInstall renderer is enabled. TemplateModel selects the
// versioned native template directory under vendors/cisco/ios/.
type CiscoIOSAutoInstallProfile struct {
	Models        []string
	Version       string
	TemplateModel string
	TemplateID    string
	State         CapabilityState
	Reason        string
	Evidence      []string
}

var ciscoIOSAutoInstallProfiles = []CiscoIOSAutoInstallProfile{
	{
		Models: []string{"7200", "cisco 7200"}, Version: CiscoIOS7200LabVerifiedVersion,
		TemplateModel: "7200", TemplateID: "infraflow.cisco.ios.7200.native", State: CapabilityLabVerified,
		Reason:   "AutoInstall over InfraFlow DHCP/TFTP applied hostname and FastEthernet0/0 addressing on a GNS3 Dynamips 7200 reporting IOS 12.4(24)T5; the configuration is applied to running-config and is not saved to NVRAM by AutoInstall.",
		Evidence: []string{"docs/DELIVERY/GNS3_ZTP_AUTOINSTALL.md (2026-11-19 lab run)"},
	},
	{
		Models: []string{"7200", "cisco 7200"}, Version: CiscoIOS7200DeclaredVersion,
		TemplateModel: "7200", TemplateID: "infraflow.cisco.ios.7200.native", State: CapabilityImplemented,
		Reason: "Exact Cisco IOS 7200 version profile selected; this version has not been observed on a running device, so runtime verification is required before claiming device support.",
	},
	{
		Models: []string{"iosv", "vios", "cisco iosv"}, Version: CiscoIOSvVersion,
		TemplateModel: "iosv", TemplateID: "infraflow.cisco.ios.iosv.native", State: CapabilityPartial,
		Reason:   "On a GNS3 IOSv reporting 15.9(3)M6 the rendered configuration was delivered by the InfraFlow TFTP service and applied (hostname, GigabitEthernet addressing, static route forwarding verified), and the device took an InfraFlow DHCP lease with its standard DHCP client. The DHCP-triggered AutoInstall step itself did not complete: the IOSv AutoInstall/PnP client never requested an offered lease, from InfraFlow or from a reference ISC dhcpd, so zero-touch AutoInstall is not verified for this profile.",
		Evidence: []string{"artifacts/qa/cisco-autoinstall-report.md (2026-11-19 lab run)"},
	},
}

// CiscoIOSAutoInstallProfileFor returns the exact AutoInstall profile for a
// Cisco IOS model and release. Matching is exact; there is no train fallback.
func CiscoIOSAutoInstallProfileFor(model, version string) (CiscoIOSAutoInstallProfile, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	version = strings.TrimSpace(version)
	for _, entry := range ciscoIOSAutoInstallProfiles {
		if entry.Version != version {
			continue
		}
		for _, candidate := range entry.Models {
			if candidate == model {
				return entry, true
			}
		}
	}
	return CiscoIOSAutoInstallProfile{}, false
}

// CiscoIOSAutoInstallProfileList renders the accepted model/release pairs for
// operator-facing error messages.
func CiscoIOSAutoInstallProfileList() string {
	parts := make([]string, 0, len(ciscoIOSAutoInstallProfiles))
	for _, entry := range ciscoIOSAutoInstallProfiles {
		parts = append(parts, entry.TemplateModel+" "+entry.Version+" ("+string(entry.State)+")")
	}
	return strings.Join(parts, ", ")
}

// CiscoIOS7200AutoInstallVersion reports whether version is an accepted Cisco
// 7200 AutoInstall version.
func CiscoIOS7200AutoInstallVersion(version string) bool {
	_, ok := CiscoIOSAutoInstallProfileFor("7200", version)
	return ok
}
