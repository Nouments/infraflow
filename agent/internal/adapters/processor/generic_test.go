package processor

import (
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

func TestGenericStagesToolAndBootstrapArtifactsWithoutExecutingThem(t *testing.T) {
	processor := Generic{}
	for _, artifact := range []protocol.Artifact{
		{Type: "ansible_playbook", Path: "lab/ansible/site.yml"},
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
