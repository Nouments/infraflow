package protocol

import "testing"

func TestComputeReportIDIsStableAndOrderIndependent(t *testing.T) {
	first := AgentReport{AgentID: "agent-1", Artifacts: []ArtifactResult{
		{Path: "site/topology.json", OutputHash: "b", Status: "completed"},
		{Path: "site/inventory.json", OutputHash: "a", Status: "completed"},
	}}
	second := AgentReport{AgentID: "agent-1", Artifacts: []ArtifactResult{
		{Path: "site/inventory.json", OutputHash: "a", Status: "completed"},
		{Path: "site/topology.json", OutputHash: "b", Status: "completed"},
	}}
	if ComputeReportID(first) != ComputeReportID(second) {
		t.Fatal("report ID should not depend on artifact ordering")
	}
	second.Artifacts[0].Status = "failed"
	if ComputeReportID(first) == ComputeReportID(second) {
		t.Fatal("report ID should change when execution state changes")
	}
}

func TestArtifactValidationIsSharedAndRestrictive(t *testing.T) {
	artifact := Artifact{Type: "inventory", Path: "lab/inventory.json"}
	if !ValidArtifactPath("lab", artifact) || !ValidSiteName("lab-01") {
		t.Fatal("expected valid site artifact path")
	}
	for _, path := range []string{"../lab/inventory.json", "lab/../inventory.json", "lab/topology.json", "/lab/inventory.json", `lab\\inventory.json`} {
		artifact.Path = path
		if ValidArtifactPath("lab", artifact) {
			t.Errorf("accepted invalid artifact path %q", path)
		}
	}
	if !IsSHA256(SHA256([]byte("test"))) || IsSHA256("short") {
		t.Fatal("SHA-256 validation failed")
	}
}
