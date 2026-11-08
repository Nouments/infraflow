package generator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

const maxManifestBytes = 1 << 20

var (
	ErrArtifactHashMismatch = errors.New("artifact output hash mismatch")
	ErrArtifactTooLarge     = errors.New("artifact exceeds maximum artifact size")
)

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
		if published.Status != "" && published.Status != "succeeded" {
			return nil, fmt.Errorf("manifest for %q is not marked succeeded", entry.Name())
		}
		if !protocol.IsSHA256(published.InputHash) {
			return nil, fmt.Errorf("manifest for %q has an invalid input hash", entry.Name())
		}
		if published.SiteID != "" && (!protocol.ValidSiteName(published.SiteID) || len(published.SiteID) > 128) {
			return nil, fmt.Errorf("manifest for %q has an invalid site id", entry.Name())
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, fmt.Errorf("manifest for %q contains trailing JSON", entry.Name())
		}
		for _, artifact := range published.Artifacts {
			if !protocol.ValidArtifactPath(entry.Name(), artifact) {
				return nil, fmt.Errorf("manifest for %q contains unsafe artifact path %q", entry.Name(), artifact.Path)
			}
			if !protocol.IsSHA256(artifact.OutputHash) || !protocol.IsSHA256(artifact.InputHash) || artifact.InputHash != published.InputHash {
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
			if artifactInfo.Size() > protocol.MaxArtifactBytes {
				return nil, fmt.Errorf("%w of %d bytes: %q", ErrArtifactTooLarge, protocol.MaxArtifactBytes, artifact.Path)
			}
			artifactFile, err := safefs.OpenReadOnly(root, artifact.Path)
			if err != nil {
				return nil, fmt.Errorf("open artifact %q: %w", artifact.Path, err)
			}
			digest := sha256.New()
			readBytes, readErr := io.Copy(digest, io.LimitReader(artifactFile, protocol.MaxArtifactBytes+1))
			closeErr := artifactFile.Close()
			if readErr != nil {
				return nil, fmt.Errorf("read artifact %q: %w", artifact.Path, readErr)
			}
			if closeErr != nil {
				return nil, fmt.Errorf("close artifact %q: %w", artifact.Path, closeErr)
			}
			if readBytes > protocol.MaxArtifactBytes {
				return nil, fmt.Errorf("%w of %d bytes: %q", ErrArtifactTooLarge, protocol.MaxArtifactBytes, artifact.Path)
			}
			if hex.EncodeToString(digest.Sum(nil)) != artifact.OutputHash {
				return nil, fmt.Errorf("%w: %q", ErrArtifactHashMismatch, artifact.Path)
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
		file, err := safefs.OpenReadOnly(root, artifact.Path)
		if err != nil {
			return nil, Artifact{}, fmt.Errorf("open artifact %q: %w", artifact.Path, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, protocol.MaxArtifactBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, Artifact{}, fmt.Errorf("read artifact %q: %w", artifact.Path, readErr)
		}
		if closeErr != nil {
			return nil, Artifact{}, fmt.Errorf("close artifact %q: %w", artifact.Path, closeErr)
		}
		if len(data) > protocol.MaxArtifactBytes {
			return nil, Artifact{}, fmt.Errorf("%w of %d bytes: %q", ErrArtifactTooLarge, protocol.MaxArtifactBytes, artifact.Path)
		}
		if protocol.SHA256(data) != artifact.OutputHash {
			return nil, Artifact{}, fmt.Errorf("%w: %q changed after catalog validation", ErrArtifactHashMismatch, artifact.Path)
		}
		return data, artifact, nil
	}
	return nil, Artifact{}, os.ErrNotExist
}
