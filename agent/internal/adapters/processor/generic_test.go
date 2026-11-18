package processor

import (
	"encoding/json"
	"strings"
	"testing"

	"infraflow/pkg/protocol"
)

func TestGenericProcessesSupportedArtifacts(t *testing.T) {
	processor := Generic{}
	for _, test := range []struct {
		artifact protocol.Artifact
		data     string
	}{
		{artifact: protocol.Artifact{Type: "inventory", Path: "lab/inventory.json"}, data: `{"site":"lab","devices":[]}`},
		{artifact: protocol.Artifact{Type: "topology", Path: "lab/topology.json"}, data: `{"site":"lab","nodes":[],"edges":[]}`},
	} {
		result := processor.Process(test.artifact, strings.NewReader(test.data))
		if result.Status != protocol.StatusCompleted {
			t.Errorf("expected %s to complete, got %#v", test.artifact.Type, result)
		}
	}
}

func TestGenericRejectsMalformedOrInconsistentInventoryAndTopology(t *testing.T) {
	tests := []struct {
		name     string
		artifact protocol.Artifact
		data     string
	}{
		{"inventory malformed", protocol.Artifact{Type: "inventory", Path: "lab/inventory.json"}, `{`},
		{"inventory wrong site", protocol.Artifact{Type: "inventory", Path: "lab/inventory.json"}, `{"site":"other","devices":[]}`},
		{"inventory blank device", protocol.Artifact{Type: "inventory", Path: "lab/inventory.json"}, `{"site":"lab","devices":[{"name":" "}]}`},
		{"inventory duplicate device", protocol.Artifact{Type: "inventory", Path: "lab/inventory.json"}, `{"site":"lab","devices":[{"name":"R1"},{"name":"R1"}]}`},
		{"topology malformed", protocol.Artifact{Type: "topology", Path: "lab/topology.json"}, `{`},
		{"topology wrong site", protocol.Artifact{Type: "topology", Path: "lab/topology.json"}, `{"site":"other","nodes":[],"edges":[]}`},
		{"topology blank node", protocol.Artifact{Type: "topology", Path: "lab/topology.json"}, `{"site":"lab","nodes":[{"name":" "}],"edges":[]}`},
		{"topology unknown source", protocol.Artifact{Type: "topology", Path: "lab/topology.json"}, `{"site":"lab","nodes":[{"name":"R1"}],"edges":[{"a":{"device":"missing"},"b":{"device":"R1"}}]}`},
		{"topology unknown destination", protocol.Artifact{Type: "topology", Path: "lab/topology.json"}, `{"site":"lab","nodes":[{"name":"R1"}],"edges":[{"a":{"device":"R1"},"b":{"device":"missing"}}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := (Generic{}).Process(test.artifact, strings.NewReader(test.data))
			if result.Status != protocol.StatusFailed {
				t.Fatalf("invalid artifact was accepted: %#v", result)
			}
		})
	}
}

func TestGenericValidatesSiteManifestAndGenerationReport(t *testing.T) {
	hash := strings.Repeat("a", 64)
	manifest, err := json.Marshal(protocol.SiteManifest{
		SchemaVersion: 1, SiteID: "lab", SiteName: "lab", Status: "succeeded",
		GeneratorVersion: "test", TemplateVersion: "test", InputHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := json.Marshal(protocol.GenerationReport{
		SchemaVersion: 1, Status: "succeeded", InputHash: hash,
		Sites: []protocol.SiteGenerationSummary{{SiteName: "lab", Status: "succeeded"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		artifact protocol.Artifact
		data     string
	}{
		{protocol.Artifact{Type: "site_manifest", Path: "lab/site-manifest.json"}, string(manifest)},
		{protocol.Artifact{Type: "generation_report", Path: "lab/generation-report.json"}, string(report)},
	} {
		if result := (Generic{}).Process(test.artifact, strings.NewReader(test.data)); result.Status != protocol.StatusCompleted {
			t.Errorf("%s rejected a valid document: %#v", test.artifact.Type, result)
		}
	}

	invalidManifest, err := json.Marshal(protocol.SiteManifest{
		SchemaVersion: 1, SiteID: "lab", SiteName: "lab", Status: "succeeded",
		GeneratorVersion: "test", TemplateVersion: "test", InputHash: hash,
		Artifacts: []protocol.SiteArtifact{{Type: "inventory", Path: "../outside.json", SHA256: hash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{
		string(invalidManifest),
		string(manifest) + ` {}`,
		`{"schema_version":1,"unexpected":true}`,
	} {
		result := (Generic{}).Process(
			protocol.Artifact{Type: "site_manifest", Path: "lab/site-manifest.json"},
			strings.NewReader(data),
		)
		if result.Status != protocol.StatusFailed {
			t.Errorf("invalid site manifest was accepted: %#v", result)
		}
	}

	invalidReport := `{"schema_version":1,"status":"failed","input_hash":"` + hash + `","sites":[{"site_name":"lab","status":"failed"}]}`
	if result := (Generic{}).Process(
		protocol.Artifact{Type: "generation_report", Path: "lab/generation-report.json"},
		strings.NewReader(invalidReport),
	); result.Status != protocol.StatusFailed {
		t.Fatalf("failed generation report was accepted: %#v", result)
	}
}

func TestGenericStagesToolAndBootstrapArtifactsWithoutExecutingThem(t *testing.T) {
	processor := Generic{}
	for _, artifact := range []protocol.Artifact{
		{Type: "native_device_config", Path: "lab/native/device-r1.cfg"},
		{Type: "native_manifest", Path: "lab/native/native-manifest.json"},
		{Type: "terraform_main", Path: "lab/terraform/main.tf"},
		{Type: "bootstrap_dhcp", Path: "lab/bootstrap/dhcp/config.json"},
		{Type: "bootstrap_tftp", Path: "lab/bootstrap/tftp/config.json"},
	} {
		result := processor.Process(artifact, strings.NewReader("verified bytes"))
		if result.Status != protocol.StatusCompleted || !strings.Contains(result.Message, "remains explicit") {
			t.Errorf("artifact %s was not staged with explicit-execution status: %#v", artifact.Type, result)
		}
	}
}

func TestGenericRejectsInvalidArtifactsAndBlocksUnknownTypes(t *testing.T) {
	processor := Generic{}
	invalidTopology := processor.Process(protocol.Artifact{Type: "topology", Path: "lab/topology.json"}, strings.NewReader(`{"site":"lab","nodes":[],"edges":[{"a":{"device":"missing"},"b":{"device":"missing"}}]}`))
	if invalidTopology.Status != protocol.StatusFailed {
		t.Fatalf("expected invalid topology to fail: %#v", invalidTopology)
	}
	unknown := processor.Process(protocol.Artifact{Type: "device-config", Path: "lab/inventory.json"}, strings.NewReader("{}"))
	if unknown.Status != protocol.StatusBlocked {
		t.Fatalf("unknown artifact type must be blocked: %#v", unknown)
	}
}
