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
	"slices"
	"sort"
	"strings"

	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

const maxManifestBytes = 1 << 20

var (
	ErrArtifactHashMismatch = errors.New("artifact output hash mismatch")
	ErrArtifactTooLarge     = errors.New("artifact exceeds maximum artifact size")
)

func Catalog(root string) ([]Artifact, error) {
	root, lock, err := acquireGenerationReadLock(root)
	if err != nil {
		return nil, err
	}
	artifacts, catalogErr := catalogArtifacts(root)
	if err := lock.release(); err != nil {
		return nil, errors.Join(catalogErr, fmt.Errorf("release artifact reader lock: %w", err))
	}
	return artifacts, catalogErr
}

func catalogArtifacts(root string) ([]Artifact, error) {
	root, entries, err := artifactRootEntries(root)
	if err != nil {
		return nil, err
	}
	var artifacts []Artifact
	seenPaths := make(map[string]struct{})
	for _, entry := range entries {
		siteArtifacts, err := siteCatalog(root, entry, true)
		if err != nil {
			return nil, err
		}
		for _, artifact := range siteArtifacts {
			if _, exists := seenPaths[artifact.Path]; exists {
				return nil, fmt.Errorf("duplicate artifact path %q", artifact.Path)
			}
			seenPaths[artifact.Path] = struct{}{}
			artifacts = append(artifacts, artifact)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

// artifactRootEntries runs the root-level checks every reader needs: an
// interrupted or in-progress publication makes the whole root unreadable,
// and a symlinked site directory is refused.
func artifactRootEntries(root string) (string, []string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve artifact root: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", nil, fmt.Errorf("read artifact root: %w", err)
	}
	var sites []string
	for _, entry := range entries {
		if entry.Name() == generationJournalName {
			return "", nil, fmt.Errorf("%w: %q", ErrRecoveryRequired, filepath.Join(root, entry.Name()))
		}
		if strings.HasPrefix(entry.Name(), ".infraflow-publish-") {
			return "", nil, fmt.Errorf("%w: %q", ErrRecoveryRequired, filepath.Join(root, entry.Name()))
		}
		if entry.Name() == generationLockName || strings.HasPrefix(entry.Name(), generationReadLockPrefix) {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, entry.Name()))
		if err != nil {
			return "", nil, fmt.Errorf("inspect artifact entry %q: %w", entry.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", nil, fmt.Errorf("artifact root contains symlink %q", entry.Name())
		}
		if info.IsDir() {
			sites = append(sites, entry.Name())
		}
	}
	return root, sites, nil
}

// siteCatalog validates one site manifest and the files it lists. With
// verifyHashes it also hashes every listed file; single-artifact readers pass
// false and verify the one file they return instead.
func siteCatalog(root, site string, verifyHashes bool) ([]Artifact, error) {
	entryPath := filepath.Join(root, site)
	manifestPath := filepath.Join(entryPath, "manifest.json")
	manifestInfo, err := os.Lstat(manifestPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect manifest for %q: %w", site, err)
	}
	if !manifestInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("manifest for %q must be a regular file", site)
	}
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("open manifest for %q: %w", site, err)
	}
	data, err := io.ReadAll(io.LimitReader(manifestFile, maxManifestBytes+1))
	closeErr := manifestFile.Close()
	if err != nil {
		return nil, fmt.Errorf("read manifest for %q: %w", site, err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close manifest for %q: %w", site, closeErr)
	}
	if len(data) > maxManifestBytes {
		return nil, fmt.Errorf("manifest for %q exceeds %d bytes", site, maxManifestBytes)
	}
	var published Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&published); err != nil {
		return nil, fmt.Errorf("decode manifest for %q: %w", site, err)
	}
	if published.Status != "" && published.Status != "succeeded" {
		return nil, fmt.Errorf("manifest for %q is not marked succeeded", site)
	}
	if !protocol.IsSHA256(published.InputHash) {
		return nil, fmt.Errorf("manifest for %q has an invalid input hash", site)
	}
	if published.SiteID != "" && (!protocol.ValidSiteName(published.SiteID) || len(published.SiteID) > 128) {
		return nil, fmt.Errorf("manifest for %q has an invalid site id", site)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("manifest for %q contains trailing JSON", site)
	}
	seenPaths := make(map[string]struct{}, len(published.Artifacts))
	artifacts := make([]Artifact, 0, len(published.Artifacts))
	for _, artifact := range published.Artifacts {
		if !protocol.ValidArtifactPath(site, artifact) {
			return nil, fmt.Errorf("manifest for %q contains unsafe artifact path %q", site, artifact.Path)
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
		if verifyHashes {
			if err := verifyArtifactFile(root, artifact); err != nil {
				return nil, err
			}
		}
		seenPaths[artifact.Path] = struct{}{}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func verifyArtifactFile(root string, artifact Artifact) error {
	artifactFile, err := safefs.OpenReadOnly(root, artifact.Path)
	if err != nil {
		return fmt.Errorf("open artifact %q: %w", artifact.Path, err)
	}
	digest := sha256.New()
	readBytes, readErr := io.Copy(digest, io.LimitReader(artifactFile, protocol.MaxArtifactBytes+1))
	closeErr := artifactFile.Close()
	if readErr != nil {
		return fmt.Errorf("read artifact %q: %w", artifact.Path, readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close artifact %q: %w", artifact.Path, closeErr)
	}
	if readBytes > protocol.MaxArtifactBytes {
		return fmt.Errorf("%w of %d bytes: %q", ErrArtifactTooLarge, protocol.MaxArtifactBytes, artifact.Path)
	}
	if hex.EncodeToString(digest.Sum(nil)) != artifact.OutputHash {
		return fmt.Errorf("%w: %q", ErrArtifactHashMismatch, artifact.Path)
	}
	return nil
}

func ReadArtifact(root, artifactPath string) ([]byte, Artifact, error) {
	root, lock, err := acquireGenerationReadLock(root)
	if err != nil {
		return nil, Artifact{}, err
	}
	data, artifact, readErr := readArtifactWithLock(root, artifactPath)
	if err := lock.release(); err != nil {
		return nil, Artifact{}, errors.Join(readErr, fmt.Errorf("release artifact reader lock: %w", err))
	}
	return data, artifact, readErr
}

// readArtifactWithLock validates only the manifest of the artifact's own site
// (plus the root-level recovery checks) and then the SHA-256 of the bytes it
// returns. Re-hashing every other site on each read made one request cost a
// full catalog pass (PRV-19 profile, 2026-11-22).
func readArtifactWithLock(root, artifactPath string) ([]byte, Artifact, error) {
	root, sites, err := artifactRootEntries(root)
	if err != nil {
		return nil, Artifact{}, err
	}
	site, _, found := strings.Cut(artifactPath, "/")
	if !found || !slices.Contains(sites, site) {
		return nil, Artifact{}, os.ErrNotExist
	}
	artifacts, err := siteCatalog(root, site, false)
	if err != nil {
		return nil, Artifact{}, err
	}
	for _, artifact := range artifacts {
		if artifact.Path != artifactPath {
			continue
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
