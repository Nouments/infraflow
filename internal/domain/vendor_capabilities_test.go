package domain

import "testing"

func TestVendorProfileForRecognizesSupportedVendorFamilies(t *testing.T) {
	tests := []struct {
		name       string
		device     Device
		wantName   string
		wantType   string
		wantMethod string
	}{
		{name: "cisco iosxe", device: Device{Vendor: "cisco", Family: "iosxe", Model: "csr1000v"}, wantName: "cisco", wantType: "iosxe", wantMethod: "netconf"},
		{name: "mikrotik routeros", device: Device{Vendor: "MikroTik", Family: "RouterOS", Model: "CCR1036"}, wantName: "mikrotik", wantType: "routeros", wantMethod: "api"},
		{name: "fortinet fortios", device: Device{Vendor: "fortigate", Family: "fortios", Model: "60E"}, wantName: "fortinet", wantType: "fortios", wantMethod: "https"},
	}

	for _, tt := range tests {
		profile, ok := VendorProfileFor(tt.device)
		if !ok {
			t.Fatalf("VendorProfileFor(%s) returned !ok", tt.name)
		}
		if profile.Vendor != tt.wantName || profile.Family != tt.wantType || profile.DefaultMethod != tt.wantMethod {
			t.Fatalf("VendorProfileFor(%s) = %#v, want vendor=%s family=%s method=%s", tt.name, profile, tt.wantName, tt.wantType, tt.wantMethod)
		}
	}
}

func TestVendorProfileForRejectsUnknownVendorFamilyCombinations(t *testing.T) {
	if _, ok := VendorProfileFor(Device{Vendor: "unknown", Family: "router", Model: "x"}); ok {
		t.Fatal("expected unsupported vendor/family combination to be rejected")
	}
}

func TestVendorCapabilityMatrixIncludesExplicitEvidenceRequirements(t *testing.T) {
	profile, ok := VendorProfileFor(Device{Vendor: "cisco", Family: "iosxe", Model: "ios-xe"})
	if !ok {
		t.Fatal("expected Cisco profile")
	}
	if len(profile.Methods) == 0 || profile.Evidence == "" {
		t.Fatalf("expected evidence-bearing vendor profile, got %#v", profile)
	}
	if profile.Status != CapabilityUnverified {
		t.Fatalf("expected vendor matrix to remain explicit and unverified until evidence exists, got %s", profile.Status)
	}
}
