package generator

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"infraflow/internal/adapters/config"
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
	if _, err := Catalog(root); !errors.Is(err, ErrArtifactHashMismatch) {
		t.Fatal("expected catalog to reject an artifact whose content does not match its manifest hash")
	}
	if _, _, err := ReadArtifact(root, "lab/inventory.json"); err == nil {
		t.Fatal("expected hash mismatch to be rejected when reading artifact")
	}
}

func TestCatalogRejectsOversizedArtifact(t *testing.T) {
	infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := Generate(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(root, "lab", "inventory.json")
	if err := os.Truncate(artifactPath, (32<<20)+1); err != nil {
		t.Fatal(err)
	}
	if _, err := Catalog(root); !errors.Is(err, ErrArtifactTooLarge) {
		t.Fatalf("expected oversized artifact rejection, got %v", err)
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

func TestCatalogRejectsUnknownTrailingAndDuplicateManifestData(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]byte) ([]byte, error)
	}{
		{
			name: "unknown field",
			mutate: func(data []byte) ([]byte, error) {
				value := strings.TrimSpace(string(data))
				return []byte(strings.TrimSuffix(value, "}") + `,"unexpected":true}`), nil
			},
		},
		{
			name: "trailing document",
			mutate: func(data []byte) ([]byte, error) {
				return append(data, []byte(` {}`)...), nil
			},
		},
		{
			name: "duplicate artifact path",
			mutate: func(data []byte) ([]byte, error) {
				var manifest Manifest
				if err := json.Unmarshal(data, &manifest); err != nil {
					return nil, err
				}
				manifest.Artifacts = append(manifest.Artifacts, manifest.Artifacts[0])
				return json.Marshal(manifest)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
			data, err = test.mutate(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Catalog(root); err == nil {
				t.Fatal("expected malformed manifest to be rejected")
			}
		})
	}
}

func TestCatalogRejectsSymlinkedArtifactAndManifest(t *testing.T) {
	for _, target := range []string{"inventory.json", "manifest.json"} {
		t.Run(target, func(t *testing.T) {
			infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if _, err := Generate(infrastructure, root); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "lab", target)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := Catalog(root); err == nil {
				t.Fatal("expected catalog to reject symlink")
			}
		})
	}
}
