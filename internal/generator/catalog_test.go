package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"infraflow/internal/config"
	"infraflow/pkg/protocol"
)

func TestCatalogOnlyListsVerifiedManifestArtifacts(t *testing.T) {
	infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := Generate(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	artifacts, err := Catalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 || artifacts[0].Path != "lab/inventory.json" || artifacts[1].Path != "lab/topology.json" {
		t.Fatalf("unexpected catalog: %#v", artifacts)
	}
	data, artifact, err := ReadArtifact(root, artifacts[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if protocol.SHA256(data) != artifact.OutputHash {
		t.Fatal("read artifact did not match the catalog hash")
	}
	if _, _, err := ReadArtifact(root, "../outside"); !os.IsNotExist(err) {
		t.Fatalf("expected unpublished path rejection, got %v", err)
	}
}

func TestCatalogRejectsTamperedArtifact(t *testing.T) {
	infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := Generate(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lab", "inventory.json"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadArtifact(root, "lab/inventory.json"); err == nil {
		t.Fatal("expected hash mismatch to be rejected when reading artifact")
	}
}

func TestCatalogRejectsUnsafeManifestPath(t *testing.T) {
	infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := Generate(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "lab", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var published Manifest
	if err := json.Unmarshal(data, &published); err != nil {
		t.Fatal(err)
	}
	published.Artifacts[0].Path = "../outside"
	data, err = json.Marshal(published)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Catalog(root); err == nil {
		t.Fatal("expected unsafe artifact path to be rejected")
	}
}
