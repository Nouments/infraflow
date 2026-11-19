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
		if vendor == "cisco" && family == "ios" && (model == "7200" || model == "cisco 7200") && version == "15.3(3)XB12" {
			profile.State = CapabilityImplemented
			profile.Reason = "Exact Cisco IOS 7200 version profile selected; runtime verification is required before claiming device support."
			return profile, true
		}
		profile.Reason = "Cisco AutoInstall is implemented only for exact cisco/ios/7200/15.3(3)XB12; no fallback mechanism is selected."
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
		if vendor == "mikrotik" && family == "routeros" && model == "rb750gr3" && strings.HasPrefix(version, "7.") {
			profile.State = CapabilityExperimental
			profile.Reason = "RouterOS template selection is exact by model and v7 train; InfraFlow renders the script but does not upload it or read .auto.log."
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
