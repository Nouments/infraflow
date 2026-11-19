package domain

import (
	"strings"
	"testing"
)

func TestBootstrapProfileSelectionIsVendorModelVersionAndMethodSpecific(t *testing.T) {
	tests := []struct {
		name       string
		device     Device
		wantMethod string
		wantState  CapabilityState
		wantText   string
	}{
		{
			name:       "Cisco IOS AutoInstall exact image profile",
			device:     Device{Vendor: "cisco", Family: "ios", Model: "7200", OSVersion: "15.3(3)XB12", Provisioning: Provisioning{Method: ProvisioningMethodCiscoAutoInstall}},
			wantMethod: "Cisco IOS AutoInstall over DHCP and TFTP", wantState: CapabilityImplemented, wantText: "runtime verification is required",
		},
		{
			name:       "Cisco IOSv AutoInstall exact release profile",
			device:     Device{Vendor: "cisco", Family: "ios", Model: "IOSv", OSVersion: "15.9(3)M6", Provisioning: Provisioning{Method: ProvisioningMethodCiscoAutoInstall}},
			wantMethod: "Cisco IOS AutoInstall over DHCP and TFTP", wantState: CapabilityPartial, wantText: "zero-touch AutoInstall is not verified",
		},
		{
			name:       "Cisco IOSv neighbouring release is not accepted",
			device:     Device{Vendor: "cisco", Family: "ios", Model: "IOSv", OSVersion: "15.9(3)M8", Provisioning: Provisioning{Method: ProvisioningMethodCiscoAutoInstall}},
			wantMethod: "Cisco IOS AutoInstall over DHCP and TFTP", wantState: CapabilityUnsupported, wantText: "no fallback mechanism",
		},
		{
			name:       "Cisco IOS XE AutoInstall has no profile",
			device:     Device{Vendor: "cisco", Family: "iosxe", Model: "csr1000v", OSVersion: "16.7.1", Provisioning: Provisioning{Method: ProvisioningMethodCiscoIOSXEAutoInstall}},
			wantMethod: "Cisco IOS XE AutoInstall", wantState: CapabilityPartial, wantText: "exact model and release compatibility still requires target verification",
		},
		{
			name:       "Cisco ZTP method is explicit and does not fall back",
			device:     Device{Vendor: "cisco", Family: "iosxe", Model: "C9350", OSVersion: "17.18.1", Provisioning: Provisioning{Method: ProvisioningMethodCiscoIOSXEZTP}},
			wantMethod: "Cisco IOS XE Zero-Touch Provisioning", wantState: CapabilityUnsupported, wantText: "no secure script execution",
		},
		{
			name:       "RouterOS Auto Import is generation only",
			device:     Device{Vendor: "mikrotik", Family: "routeros", Model: "RB750Gr3", OSVersion: "7.20.1", Provisioning: Provisioning{Method: ProvisioningMethodAutoRSC}},
			wantMethod: "MikroTik RouterOS .auto.rsc Auto Import", wantState: CapabilityExperimental, wantText: "does not upload it",
		},
		{
			name:       "RouterOS Netinstall is destructive and unavailable",
			device:     Device{Vendor: "mikrotik", Family: "routeros", Model: "RB750Gr3", OSVersion: "7.20.1", Provisioning: Provisioning{Method: ProvisioningMethodMikroTikNetinstall}},
			wantMethod: "MikroTik Netinstall", wantState: CapabilityUnsupported, wantText: "destructive reinstall",
		},
		{
			name:       "FortiManager ZTP requires explicit backend",
			device:     Device{Vendor: "fortinet", Family: "fortios", Model: "FG-60F", OSVersion: "7.0.8", Provisioning: Provisioning{Method: ProvisioningMethodFortiManagerZTP}},
			wantMethod: "FortiGate FortiManager ZTP", wantState: CapabilityPartial, wantText: "FortiManager must authorize",
		},
		{
			name:       "FortiDeploy requires cloud entitlement",
			device:     Device{Vendor: "fortinet", Family: "fortios", Model: "FG-60F", OSVersion: "7.0.8", Provisioning: Provisioning{Method: ProvisioningMethodFortiDeployCloud}},
			wantMethod: "FortiGate FortiDeploy cloud enrollment", wantState: CapabilityUnsupported, wantText: "external cloud account",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile, ok := BootstrapProfileFor(test.device)
			if !ok || profile.Mechanism != test.wantMethod || profile.State != test.wantState || !strings.Contains(profile.Reason, test.wantText) {
				t.Fatalf("unexpected method selection: %#v, known=%t", profile, ok)
			}
		})
	}
}

func TestBootstrapProfileRejectsWrongCiscoAutoInstallIdentity(t *testing.T) {
	device := Device{Vendor: "cisco", Family: "ios", Model: "7200", OSVersion: "15.3(3)XB13", Provisioning: Provisioning{Method: ProvisioningMethodCiscoAutoInstall}}
	profile, ok := BootstrapProfileFor(device)
	if !ok || profile.State != CapabilityUnsupported || !strings.Contains(profile.Reason, "no fallback") {
		t.Fatalf("wrong Cisco IOS version selected an AutoInstall profile: %#v, known=%t", profile, ok)
	}
}

func TestCisco7200AutoInstallVersionStates(t *testing.T) {
	base := Device{Vendor: "cisco", Family: "ios", Model: "7200", Provisioning: Provisioning{Method: ProvisioningMethodCiscoAutoInstall}}
	cases := []struct {
		version string
		state   CapabilityState
	}{
		{"12.4(24)T5", CapabilityLabVerified},
		{"15.3(3)XB12", CapabilityImplemented},
		{"15.2(4)M11", CapabilityUnsupported},
		{"", CapabilityUnsupported},
	}
	for _, test := range cases {
		device := base
		device.OSVersion = test.version
		profile, known := BootstrapProfileFor(device)
		if !known || profile.State != test.state {
			t.Fatalf("version %q: state %q known=%v, want %q", test.version, profile.State, known, test.state)
		}
		if test.state == CapabilityUnsupported && (profile.Reason == "" || !strings.Contains(profile.Reason, "no fallback")) {
			t.Fatalf("version %q rejected without a precise reason: %q", test.version, profile.Reason)
		}
	}
	other := base
	other.Model, other.OSVersion = "7500", "12.4(24)T5"
	if profile, _ := BootstrapProfileFor(other); profile.State != CapabilityUnsupported {
		t.Fatalf("a different model inherited the lab-verified state: %q", profile.State)
	}
}
