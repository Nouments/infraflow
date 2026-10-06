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
	if !ValidArtifactPath("lab", Artifact{Type: "ansible_inventory", Path: "lab/ansible/inventory.yml"}) || !ValidArtifactPath("lab", Artifact{Type: "ansible_playbook", Path: "lab/ansible/site.yml"}) {
		t.Fatal("expected valid Ansible artifact paths")
	}
	if !ValidArtifactPath("lab", Artifact{Type: "terraform_locals", Path: "lab/terraform/locals.tf"}) || !ValidArtifactPath("lab", Artifact{Type: "terraform_tfvars_example", Path: "lab/terraform/terraform.tfvars.example"}) {
		t.Fatal("expected valid Terraform artifact paths")
	}
	if !ValidArtifactPath("lab", Artifact{Type: "bootstrap_dhcp", Path: "lab/bootstrap/dhcp/config.json"}) || !ValidArtifactPath("lab", Artifact{Type: "bootstrap_ipxe_script", Path: "lab/bootstrap/pxe/ipxe/bootstrap.ipxe"}) {
		t.Fatal("expected valid bootstrap artifact paths")
	}
}
