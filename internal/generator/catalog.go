package generator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"infraflow/pkg/protocol"
)

const maxManifestBytes = 1 << 20

func Catalog(root string) ([]Artifact, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact root: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read artifact root: %w", err)
	}

	var artifacts []Artifact
	seenPaths := make(map[string]struct{})
	for _, entry := range entries {
		entryPath := filepath.Join(root, entry.Name())
		info, err := os.Lstat(entryPath)
		if err != nil {
			return nil, fmt.Errorf("inspect artifact entry %q: %w", entry.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("artifact root contains symlink %q", entry.Name())
		}
		if !info.IsDir() {
			continue
		}
		manifestPath := filepath.Join(entryPath, "manifest.json")
		manifestInfo, err := os.Lstat(manifestPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect manifest for %q: %w", entry.Name(), err)
		}
		if !manifestInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("manifest for %q must be a regular file", entry.Name())
		}
		manifestFile, err := os.Open(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("open manifest for %q: %w", entry.Name(), err)
		}
		data, err := io.ReadAll(io.LimitReader(manifestFile, maxManifestBytes+1))
		closeErr := manifestFile.Close()
		if err != nil {
			return nil, fmt.Errorf("read manifest for %q: %w", entry.Name(), err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close manifest for %q: %w", entry.Name(), closeErr)
		}
		if len(data) > maxManifestBytes {
			return nil, fmt.Errorf("manifest for %q exceeds %d bytes", entry.Name(), maxManifestBytes)
		}
		var published Manifest
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&published); err != nil {
			return nil, fmt.Errorf("decode manifest for %q: %w", entry.Name(), err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, fmt.Errorf("manifest for %q contains trailing JSON", entry.Name())
		}
		for _, artifact := range published.Artifacts {
			if !protocol.ValidArtifactPath(entry.Name(), artifact) {
				return nil, fmt.Errorf("manifest for %q contains unsafe artifact path %q", entry.Name(), artifact.Path)
			}
			if !protocol.IsSHA256(artifact.OutputHash) || !protocol.IsSHA256(artifact.InputHash) {
				return nil, fmt.Errorf("artifact %q has invalid hash metadata", artifact.Path)
			}
			if _, exists := seenPaths[artifact.Path]; exists {
				return nil, fmt.Errorf("duplicate artifact path %q", artifact.Path)
			}
			artifactPath := filepath.Join(root, filepath.FromSlash(artifact.Path))
			artifactInfo, err := os.Lstat(artifactPath)
			if err != nil {
				return nil, fmt.Errorf("inspect artifact %q: %w", artifact.Path, err)
			}
			if !artifactInfo.Mode().IsRegular() {
				return nil, fmt.Errorf("artifact %q must be a regular file", artifact.Path)
			}
			seenPaths[artifact.Path] = struct{}{}
			artifacts = append(artifacts, artifact)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func ReadArtifact(root, artifactPath string) ([]byte, Artifact, error) {
	artifacts, err := Catalog(root)
	if err != nil {
		return nil, Artifact{}, err
	}
	for _, artifact := range artifacts {
		if artifact.Path != artifactPath {
			continue
		}
		root, err := filepath.Abs(root)
		if err != nil {
			return nil, Artifact{}, fmt.Errorf("resolve artifact root: %w", err)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifact.Path)))
		if err != nil {
			return nil, Artifact{}, fmt.Errorf("read artifact %q: %w", artifact.Path, err)
		}
		if protocol.SHA256(data) != artifact.OutputHash {
			return nil, Artifact{}, fmt.Errorf("artifact %q changed after catalog validation", artifact.Path)
		}
		return data, artifact, nil
	}
	return nil, Artifact{}, os.ErrNotExist
}
