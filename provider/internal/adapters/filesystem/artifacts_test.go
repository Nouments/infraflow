package filesystem

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	configadapter "infraflow/internal/adapters/config"
	generator "infraflow/internal/adapters/generation"
	"infraflow/pkg/protocol"
)

func TestArtifactRepositoryOpenReturnsSnapshotAcrossPublication(t *testing.T) {
	root := t.TempDir()
	firstArtifacts := generateArtifactRepositoryFixture(t, root, "192.0.2.10", true)
	artifactPath := findArtifactByType(t, firstArtifacts, "generation_report").Path
	repository := NewArtifactRepository(root)

	reader, firstMetadata, err := repository.Open(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	secondArtifacts := generateArtifactRepositoryFixture(t, root, "192.0.2.20", true)
	secondMetadata := findArtifactByType(t, secondArtifacts, "generation_report")
	if secondMetadata.Path != artifactPath || secondMetadata.OutputHash == firstMetadata.OutputHash {
		t.Fatalf("fixture did not publish changed content at the same path: first=%#v second=%#v", firstMetadata, secondMetadata)
	}

	firstContent, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if protocol.SHA256(firstContent) != firstMetadata.OutputHash {
		t.Fatal("reader content does not match the metadata captured before publication")
	}

	secondReader, published, err := repository.Open(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	defer secondReader.Close()
	secondContent, err := io.ReadAll(secondReader)
	if err != nil {
		t.Fatal(err)
	}
	if published.OutputHash != secondMetadata.OutputHash || protocol.SHA256(secondContent) != published.OutputHash {
		t.Fatal("reader did not return content and metadata from the newly published generation")
	}
}

func TestArtifactRepositoryOpenRejectsReadsDuringPublication(t *testing.T) {
	root := t.TempDir()
	artifacts := generateArtifactRepositoryFixture(t, root, "192.0.2.10", true)
	artifactPath := findArtifactByType(t, artifacts, "generation_report").Path
	repository := NewArtifactRepository(root)

	lockPath := filepath.Join(root, ".infraflow-generation.lock")
	if err := os.WriteFile(lockPath, []byte("test-writer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "lab", "manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"status":`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.Open(artifactPath); err == nil || !strings.Contains(err.Error(), "generation is in progress") {
		t.Fatalf("Open during publication returned %v; want an explicit generation-in-progress error", err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}

	reader, _, err := repository.Open(artifactPath)
	if err != nil {
		t.Fatalf("Open failed after publication lock removal: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactRepositoryOpenRejectsRemovedArtifact(t *testing.T) {
	root := t.TempDir()
	artifacts := generateArtifactRepositoryFixture(t, root, "192.0.2.10", true)
	artifactPath := findArtifactByType(t, artifacts, "bootstrap_dhcp").Path
	repository := NewArtifactRepository(root)

	generateArtifactRepositoryFixture(t, root, "192.0.2.10", false)
	if _, _, err := repository.Open(artifactPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open of removed artifact returned %v; want os.ErrNotExist", err)
	}
}

func TestArtifactRepositoryOpenAllowsConcurrentReaders(t *testing.T) {
	root := t.TempDir()
	artifacts := generateArtifactRepositoryFixture(t, root, "192.0.2.10", true)
	artifact := findArtifactByType(t, artifacts, "generation_report")
	repository := NewArtifactRepository(root)

	const readerCount = 8
	start := make(chan struct{})
	ready := make(chan struct{}, readerCount)
	errorsByReader := make(chan error, readerCount)
	var readers sync.WaitGroup
	for range readerCount {
		readers.Add(1)
		go func() {
			defer readers.Done()
			ready <- struct{}{}
			<-start
			reader, metadata, err := repository.Open(artifact.Path)
			if err != nil {
				errorsByReader <- err
				return
			}
			content, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil {
				errorsByReader <- readErr
				return
			}
			if closeErr != nil {
				errorsByReader <- closeErr
				return
			}
			if metadata.OutputHash != artifact.OutputHash || protocol.SHA256(content) != metadata.OutputHash {
				errorsByReader <- fmt.Errorf("reader observed inconsistent metadata or content")
			}
		}()
	}
	for range readerCount {
		<-ready
	}
	close(start)
	readers.Wait()
	close(errorsByReader)
	for err := range errorsByReader {
		t.Errorf("concurrent artifact read failed: %v", err)
	}
}

func TestArtifactRepositoryOpenConcurrentWithGeneration(t *testing.T) {
	root := t.TempDir()
	firstArtifacts := generateArtifactRepositoryFixture(t, root, "192.0.2.10", true)
	artifact := findArtifactByType(t, firstArtifacts, "generation_report")
	repository := NewArtifactRepository(root)

	const readerCount = 8
	start := make(chan struct{})
	ready := make(chan struct{}, readerCount+1)
	errorsByReader := make(chan error, readerCount)
	var readers sync.WaitGroup
	for range readerCount {
		readers.Add(1)
		go func() {
			defer readers.Done()
			ready <- struct{}{}
			<-start
			reader, metadata, err := repository.Open(artifact.Path)
			if err != nil {
				if strings.Contains(err.Error(), "generation is in progress") {
					return
				}
				errorsByReader <- err
				return
			}
			content, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil {
				errorsByReader <- readErr
				return
			}
			if closeErr != nil {
				errorsByReader <- closeErr
				return
			}
			if protocol.SHA256(content) != metadata.OutputHash {
				errorsByReader <- fmt.Errorf("reader content hash does not match its catalog metadata")
			}
		}()
	}

	writerDone := make(chan error, 1)
	go func() {
		ready <- struct{}{}
		<-start
		_, err := generateArtifactRepositoryFixtureResult(root, "192.0.2.20", true)
		writerDone <- err
	}()
	for range readerCount + 1 {
		<-ready
	}
	close(start)
	readers.Wait()
	close(errorsByReader)
	for err := range errorsByReader {
		t.Errorf("concurrent artifact read failed: %v", err)
	}
	if err := <-writerDone; err != nil && !strings.Contains(err.Error(), "locked") && !strings.Contains(err.Error(), "in use") {
		t.Fatalf("concurrent generation failed unexpectedly: %v", err)
	}

	catalog, err := repository.Catalog()
	if err != nil {
		t.Fatalf("catalog rejected output after concurrent activity: %v", err)
	}
	for _, entry := range catalog {
		content, metadata, err := generator.ReadArtifact(root, entry.Path)
		if err != nil {
			t.Fatalf("read catalog artifact %q after concurrent activity: %v", entry.Path, err)
		}
		if protocol.SHA256(content) != metadata.OutputHash {
			t.Fatalf("catalog artifact %q has inconsistent content after concurrent activity", entry.Path)
		}
	}
}

func BenchmarkArtifactRepositoryOpen(b *testing.B) {
	root := b.TempDir()
	artifacts, err := generateArtifactRepositoryFixtureResult(root, "192.0.2.10", true)
	if err != nil {
		b.Fatal(err)
	}
	artifactPath := findArtifactByType(b, artifacts, "generation_report").Path
	repository := NewArtifactRepository(root)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		reader, _, err := repository.Open(artifactPath)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, reader); err != nil {
			_ = reader.Close()
			b.Fatal(err)
		}
		if err := reader.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func generateArtifactRepositoryFixture(t testing.TB, root, address string, enableDHCP bool) []protocol.Artifact {
	t.Helper()
	artifacts, err := generateArtifactRepositoryFixtureResult(root, address, enableDHCP)
	if err != nil {
		t.Fatal(err)
	}
	return artifacts
}

func generateArtifactRepositoryFixtureResult(root, address string, enableDHCP bool) ([]protocol.Artifact, error) {
	input := `sites:
  - name: lab
    bootstrap:
      network: 192.0.2.0/24
      gateway: 192.0.2.1
`
	if enableDHCP {
		input += `    services:
      dhcp: true
`
	}
	input += fmt.Sprintf(`    devices:
      - name: R1
        vendor: cisco
        model: ios-xe
        management:
          ipv4: %s
`, address)
	infrastructure, err := configadapter.Parse([]byte(input))
	if err != nil {
		return nil, fmt.Errorf("parse test infrastructure: %w", err)
	}
	artifacts, err := generator.GenerateAll(infrastructure, root)
	if err != nil {
		return nil, fmt.Errorf("generate test infrastructure: %w", err)
	}
	return artifacts, nil
}

func findArtifactByType(t testing.TB, artifacts []protocol.Artifact, artifactType string) protocol.Artifact {
	t.Helper()
	for _, artifact := range artifacts {
		if artifact.Type == artifactType {
			return artifact
		}
	}
	t.Fatalf("generated fixture has no %s artifact", artifactType)
	return protocol.Artifact{}
}
