package domain

import (
	"strings"
	"testing"
)

func TestTemplateRegistryResolveReturnsCompleteMetadata(t *testing.T) {
	registry := TemplateRegistry{Entries: map[string]ProviderTemplate{
		"example:router:1": {
			ID:       "example:router:1",
			Vendor:   "example-vendor",
			Family:   "router",
			Model:    "router-one",
			Version:  "1",
			Hash:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Evidence: "repository manifest produced by an audited test",
		},
	}}

	template, err := registry.Resolve(Device{
		Vendor: "EXAMPLE-VENDOR",
		Family: "router",
		Model:  "router-one",
	}, "1")
	if err != nil {
		t.Fatal(err)
	}
	if template.ID != "example:router:1" || template.Hash != registry.Entries[template.ID].Hash {
		t.Fatalf("unexpected resolved template: %#v", template)
	}
}

func TestTemplateRegistryResolveRejectsIncompleteOrMismatchedMetadata(t *testing.T) {
	tests := []struct {
		name     string
		registry TemplateRegistry
		device   Device
		version  string
	}{
		{
			name: "missing hash",
			registry: TemplateRegistry{Entries: map[string]ProviderTemplate{
				"example:router:1": {ID: "example:router:1", Vendor: "example-vendor", Family: "router", Model: "router-one", Version: "1"},
			}},
			device:  Device{Vendor: "example-vendor", Family: "router", Model: "router-one"},
			version: "1",
		},
		{
			name: "unknown version",
			registry: TemplateRegistry{Entries: map[string]ProviderTemplate{
				"example:router:1": {ID: "example:router:1", Vendor: "example-vendor", Family: "router", Model: "router-one", Version: "1", Hash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
			}},
			device:  Device{Vendor: "example-vendor", Family: "router", Model: "router-one"},
			version: "2",
		},
		{
			name: "mismatched context",
			registry: TemplateRegistry{Entries: map[string]ProviderTemplate{
				"example:router:1": {ID: "example:router:1", Vendor: "example-vendor", Family: "router", Model: "router-one", Version: "1", Hash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
			}},
			device:  Device{Vendor: "example-vendor", Family: "router", Model: "router-two"},
			version: "1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.registry.Resolve(test.device, test.version); err == nil || !strings.Contains(err.Error(), "template") {
				t.Fatalf("expected template resolution error, got %v", err)
			}
		})
	}
}
