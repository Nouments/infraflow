package generator

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

const (
	generationLockName       = ".infraflow-generation.lock"
	generationReadLockPrefix = ".infraflow-generation-reader-"
	generationJournalName    = ".infraflow-publish-journal.json"
	maxManagedFileSize       = 32 << 20
	maxPublicationJournal    = 1 << 20
)

// ErrRecoveryRequired marks output roots that readers and writers must not use
// until the retained publication journal and backups have been recovered.
var ErrRecoveryRequired = errors.New("publication recovery required")

func recoveryRequiredError(reason string) error {
	return fmt.Errorf("%w: %s", ErrRecoveryRequired, reason)
}

type generationLock struct {
	path  string
	token string
}

func acquireGenerationLock(root string) (generationLock, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return generationLock{}, fmt.Errorf("create generation lock token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	lock, err := createGenerationLockFile(filepath.Join(root, generationLockName), token)
	if err != nil {
		return generationLock{}, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return generationLock{}, errors.Join(fmt.Errorf("inspect generation readers: %w", err), lock.release())
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), generationReadLockPrefix) {
			return generationLock{}, errors.Join(
				fmt.Errorf("output directory is in use by an artifact reader"),
				lock.release(),
			)
		}
	}
	return lock, nil
}

func acquireGenerationReadLock(root string) (string, generationLock, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", generationLock{}, fmt.Errorf("resolve artifact root: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", generationLock{}, fmt.Errorf("inspect artifact root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return "", generationLock{}, fmt.Errorf("artifact root must be a real directory")
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", generationLock{}, fmt.Errorf("create artifact reader lock token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	lockPath := filepath.Join(root, generationReadLockPrefix+token+".lock")
	lock, err := createGenerationLockFile(lockPath, token)
	if err != nil {
		return "", generationLock{}, err
	}
	_, err = os.Lstat(filepath.Join(root, generationLockName))
	if err == nil {
		return "", generationLock{}, errors.Join(
			fmt.Errorf("generation is in progress for %q", root),
			lock.release(),
		)
	}
	if !os.IsNotExist(err) {
		if os.IsPermission(err) {
			return "", generationLock{}, errors.Join(
				fmt.Errorf("generation is in progress for %q; cannot inspect its lock: %w", root, err),
				lock.release(),
			)
		}
		return "", generationLock{}, errors.Join(
			fmt.Errorf("inspect generation lock: %w", err),
			lock.release(),
		)
	}
	return root, lock, nil
}

func withOutputGenerationLock(outputDirectory string, operation func(string) ([]Artifact, error)) ([]Artifact, error) {
	if strings.TrimSpace(outputDirectory) == "" {
		return nil, fmt.Errorf("output directory is required")
	}
	root, err := filepath.Abs(outputDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("output directory must be a real directory")
	}
	lock, err := acquireGenerationLock(root)
	if err != nil {
		return nil, err
	}
	if err := recoverInterruptedPublication(root); err != nil {
		return nil, errors.Join(err, lock.release())
	}
	artifacts, operationErr := operation(root)
	if err := lock.release(); err != nil {
		return nil, errors.Join(operationErr, fmt.Errorf("release generation lock: %w", err))
	}
	return artifacts, operationErr
}

func createGenerationLockFile(lockPath, token string) (generationLock, error) {
	file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return generationLock{}, fmt.Errorf("output directory is locked by another or interrupted operation")
		}
		return generationLock{}, fmt.Errorf("create generation lock: %w", err)
	}
	lock := generationLock{path: lockPath, token: token}
	if written, err := io.WriteString(file, token+"\n"); err != nil || written != len(token)+1 {
		if err == nil {
			err = io.ErrShortWrite
		}
		return generationLock{}, errors.Join(fmt.Errorf("write generation lock: %w", err), cleanupGenerationLockFile(file, lockPath))
	}
	if err := file.Sync(); err != nil {
		return generationLock{}, errors.Join(fmt.Errorf("sync generation lock: %w", err), cleanupGenerationLockFile(file, lockPath))
	}
	if err := file.Close(); err != nil {
		removeErr := os.Remove(lockPath)
		if os.IsNotExist(removeErr) {
			removeErr = nil
		} else if removeErr != nil {
			removeErr = fmt.Errorf("remove generation lock after close failure: %w", removeErr)
		}
		return generationLock{}, errors.Join(fmt.Errorf("close generation lock: %w", err), removeErr)
	}
	return lock, nil
}

func cleanupGenerationLockFile(file *os.File, lockPath string) error {
	closeErr := file.Close()
	removeErr := os.Remove(lockPath)
	if os.IsNotExist(removeErr) {
		removeErr = nil
	} else if removeErr != nil {
		removeErr = fmt.Errorf("remove generation lock during cleanup: %w", removeErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close generation lock during cleanup: %w", closeErr)
	}
	return errors.Join(closeErr, removeErr)
}

func (lock generationLock) release() error {
	info, err := os.Lstat(lock.path)
	if err != nil {
		return fmt.Errorf("inspect generation lock: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("generation lock was replaced with an unsafe file")
	}
	data, err := os.ReadFile(lock.path)
	if err != nil {
		return fmt.Errorf("read generation lock: %w", err)
	}
	if string(data) != lock.token+"\n" {
		return fmt.Errorf("generation lock ownership changed")
	}
	if err := os.Remove(lock.path); err != nil {
		return fmt.Errorf("remove generation lock: %w", err)
	}
	return nil
}

type previousOutput struct {
	managed                 map[string]struct{}
	hasSuccessfulGeneration bool
}

func inspectManagedOutput(root string) (previousOutput, error) {
	artifacts, err := catalogArtifacts(root)
	if err != nil {
		return previousOutput{}, err
	}
	managed := make(map[string]struct{}, len(artifacts)+4)
	artifactsBySite := make(map[string][]Artifact)
	catalogSites := make(map[string]struct{})
	for _, artifact := range artifacts {
		site, _, _ := strings.Cut(artifact.Path, "/")
		managed[artifact.Path] = struct{}{}
		artifactsBySite[site] = append(artifactsBySite[site], artifact)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return previousOutput{}, fmt.Errorf("read output directory: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == generationLockName || strings.HasPrefix(entry.Name(), generationReadLockPrefix) {
			continue
		}
		entryPath := filepath.Join(root, entry.Name())
		info, err := os.Lstat(entryPath)
		if err != nil {
			return previousOutput{}, fmt.Errorf("inspect output entry %q: %w", entry.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return previousOutput{}, fmt.Errorf("output directory contains symlink %q", entry.Name())
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
			return previousOutput{}, fmt.Errorf("inspect site catalog %q: %w", entry.Name(), err)
		}
		if !protocol.ValidSiteName(entry.Name()) || manifestInfo.Mode()&os.ModeSymlink != 0 || !manifestInfo.Mode().IsRegular() {
			return previousOutput{}, fmt.Errorf("site catalog %q is not a safe regular manifest", entry.Name())
		}
		catalogSites[entry.Name()] = struct{}{}
		managed[filepath.ToSlash(filepath.Join(entry.Name(), "manifest.json"))] = struct{}{}
	}

	globalPath := filepath.Join(root, "manifest.json")
	reportPath := filepath.Join(root, "generation-report.json")
	globalInfo, globalErr := os.Lstat(globalPath)
	reportInfo, reportErr := os.Lstat(reportPath)
	globalExists := globalErr == nil
	reportExists := reportErr == nil
	if globalErr != nil && !os.IsNotExist(globalErr) {
		return previousOutput{}, fmt.Errorf("inspect global manifest: %w", globalErr)
	}
	if reportErr != nil && !os.IsNotExist(reportErr) {
		return previousOutput{}, fmt.Errorf("inspect global generation report: %w", reportErr)
	}
	if globalExists != reportExists {
		return previousOutput{}, fmt.Errorf("global manifest and generation report must either both exist or both be absent")
	}
	if !globalExists {
		return previousOutput{
			managed:                 managed,
			hasSuccessfulGeneration: len(artifacts) > 0,
		}, nil
	}
	if globalInfo.Mode()&os.ModeSymlink != 0 || !globalInfo.Mode().IsRegular() ||
		reportInfo.Mode()&os.ModeSymlink != 0 || !reportInfo.Mode().IsRegular() {
		return previousOutput{}, fmt.Errorf("global manifest and report must be regular files")
	}

	globalBytes, _, err := readManagedFile(root, "manifest.json")
	if err != nil {
		return previousOutput{}, err
	}
	reportBytes, _, err := readManagedFile(root, "generation-report.json")
	if err != nil {
		return previousOutput{}, err
	}
	var global GlobalManifest
	if err := decodeStrictJSON(globalBytes, &global); err != nil {
		return previousOutput{}, fmt.Errorf("decode global manifest: %w", err)
	}
	var report protocol.GenerationReport
	if err := decodeStrictJSON(reportBytes, &report); err != nil {
		return previousOutput{}, fmt.Errorf("decode global generation report: %w", err)
	}
	if global.SchemaVersion != 1 || global.GenerationReport != "generation-report.json" ||
		!protocol.IsSHA256(global.InputHash) ||
		(global.Status != "succeeded" && global.Status != "partial_failure") ||
		report.SchemaVersion != 1 || report.Status != global.Status || report.InputHash != global.InputHash ||
		!reflect.DeepEqual(global.Sites, report.Sites) || len(global.Sites) == 0 {
		return previousOutput{}, fmt.Errorf("global manifest and generation report are inconsistent")
	}

	seenSites := make(map[string]struct{}, len(global.Sites))
	seenIDs := make(map[string]struct{}, len(global.Sites))
	hasFailedSite := false
	hasSuccessfulSite := false
	for _, summary := range global.Sites {
		if !protocol.ValidSiteName(summary.SiteName) || !protocol.ValidSiteName(summary.SiteID) ||
			len(summary.SiteID) > 128 || !protocol.IsSHA256(summary.SiteManifestSHA256) {
			return previousOutput{}, fmt.Errorf("global manifest contains invalid site metadata")
		}
		if _, exists := seenSites[summary.SiteName]; exists {
			return previousOutput{}, fmt.Errorf("global manifest contains duplicate site %q", summary.SiteName)
		}
		if _, exists := seenIDs[summary.SiteID]; exists {
			return previousOutput{}, fmt.Errorf("global manifest contains duplicate site id %q", summary.SiteID)
		}
		seenSites[summary.SiteName] = struct{}{}
		seenIDs[summary.SiteID] = struct{}{}
		expectedSiteManifest := filepath.ToSlash(filepath.Join(summary.SiteName, "site-manifest.json"))
		expectedSiteReport := filepath.ToSlash(filepath.Join(summary.SiteName, "generation-report.json"))
		if summary.SiteManifestPath != expectedSiteManifest || summary.GenerationReportPath != expectedSiteReport ||
			(summary.Status != "succeeded" && summary.Status != "failed") {
			return previousOutput{}, fmt.Errorf("global manifest contains invalid paths or status for site %q", summary.SiteName)
		}

		siteManifestBytes, _, err := readManagedFile(root, summary.SiteManifestPath)
		if err != nil {
			return previousOutput{}, fmt.Errorf("read site manifest for %q: %w", summary.SiteName, err)
		}
		if protocol.SHA256(siteManifestBytes) != summary.SiteManifestSHA256 {
			return previousOutput{}, fmt.Errorf("site manifest hash mismatch for %q", summary.SiteName)
		}
		var siteManifest protocol.SiteManifest
		if err := decodeStrictJSON(siteManifestBytes, &siteManifest); err != nil {
			return previousOutput{}, fmt.Errorf("decode site manifest for %q: %w", summary.SiteName, err)
		}
		siteReportBytes, _, err := readManagedFile(root, summary.GenerationReportPath)
		if err != nil {
			return previousOutput{}, fmt.Errorf("read site report for %q: %w", summary.SiteName, err)
		}
		var siteReport protocol.GenerationReport
		if err := decodeStrictJSON(siteReportBytes, &siteReport); err != nil {
			return previousOutput{}, fmt.Errorf("decode site report for %q: %w", summary.SiteName, err)
		}
		if siteManifest.SchemaVersion != 1 || siteManifest.SiteID != summary.SiteID ||
			siteManifest.SiteName != summary.SiteName || siteManifest.Status != summary.Status ||
			!protocol.IsSHA256(siteManifest.InputHash) || !reflect.DeepEqual(siteManifest.Errors, summary.Errors) ||
			siteReport.SchemaVersion != 1 ||
			siteReport.InputHash != siteManifest.InputHash || siteReport.Status != summary.Status ||
			len(siteReport.Sites) != 1 || !summaryMatchesSiteReport(summary, siteReport.Sites[0]) {
			return previousOutput{}, fmt.Errorf("site manifest and report are inconsistent for %q", summary.SiteName)
		}

		managed[summary.SiteManifestPath] = struct{}{}
		managed[summary.GenerationReportPath] = struct{}{}
		siteCatalogPath := filepath.ToSlash(filepath.Join(summary.SiteName, "manifest.json"))
		_, catalogErr := os.Lstat(filepath.Join(root, filepath.FromSlash(siteCatalogPath)))
		hasCatalog := catalogErr == nil
		if catalogErr != nil && !os.IsNotExist(catalogErr) {
			return previousOutput{}, fmt.Errorf("inspect site catalog for %q: %w", summary.SiteName, catalogErr)
		}
		if summary.Status == "succeeded" {
			hasSuccessfulSite = true
			siteCatalog, err := readCatalogManifestFromDisk(root, summary.SiteName)
			if !hasCatalog || siteCatalog.SiteID != summary.SiteID || siteCatalog.Status != "succeeded" ||
				err != nil || siteCatalog.InputHash != siteManifest.InputHash ||
				summary.ArtifactCount != len(artifactsBySite[summary.SiteName]) ||
				!siteDetailsMatchCatalog(root, summary.SiteName, siteManifest, summary.SiteManifestSHA256, artifactsBySite[summary.SiteName]) {
				return previousOutput{}, fmt.Errorf("site catalog does not match site metadata for %q", summary.SiteName)
			}
			managed[siteCatalogPath] = struct{}{}
		} else {
			hasFailedSite = true
			if hasCatalog || summary.ArtifactCount != 0 || len(siteManifest.Artifacts) != 0 {
				return previousOutput{}, fmt.Errorf("failed site %q has published artifacts", summary.SiteName)
			}
		}
	}
	for site := range catalogSites {
		if _, listed := seenSites[site]; !listed {
			return previousOutput{}, fmt.Errorf("site catalog %q is not listed in the global manifest", site)
		}
	}
	if (global.Status == "partial_failure") != hasFailedSite {
		return previousOutput{}, fmt.Errorf("global status does not match site results")
	}
	managed["manifest.json"] = struct{}{}
	managed["generation-report.json"] = struct{}{}
	return previousOutput{
		managed:                 managed,
		hasSuccessfulGeneration: hasSuccessfulSite,
	}, nil
}

func summaryMatchesSiteReport(summary, report protocol.SiteGenerationSummary) bool {
	return summary.SiteID == report.SiteID && summary.SiteName == report.SiteName &&
		summary.Status == report.Status && summary.ArtifactCount == report.ArtifactCount &&
		summary.SiteManifestPath == report.SiteManifestPath &&
		summary.GenerationReportPath == report.GenerationReportPath &&
		reflect.DeepEqual(summary.Errors, report.Errors)
}

func siteDetailsMatchCatalog(root, site string, manifest protocol.SiteManifest, siteManifestHash string, artifacts []Artifact) bool {
	catalogByPath := make(map[string]Artifact, len(artifacts))
	for _, artifact := range artifacts {
		catalogByPath[artifact.Path] = artifact
	}
	siteManifestPath := filepath.ToSlash(filepath.Join(site, "site-manifest.json"))
	siteManifestArtifact, exists := catalogByPath[siteManifestPath]
	if !exists || siteManifestArtifact.Type != "site_manifest" || siteManifestArtifact.OutputHash != siteManifestHash {
		return false
	}
	details := make(map[string]struct{}, len(manifest.Artifacts))
	for _, detail := range manifest.Artifacts {
		if !safePublicationPath(detail.Path) || !strings.HasPrefix(detail.Path, site+"/") {
			return false
		}
		if _, duplicate := details[detail.Path]; duplicate {
			return false
		}
		artifact, exists := catalogByPath[detail.Path]
		if !exists || artifact.Type != detail.Type || artifact.OutputHash != detail.SHA256 || detail.SizeBytes < 0 {
			return false
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(detail.Path)))
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != detail.SizeBytes {
			return false
		}
		details[detail.Path] = struct{}{}
	}
	return len(details)+1 == len(artifacts)
}

func readCatalogManifestFromDisk(root, site string) (Manifest, error) {
	data, _, err := readManagedFile(root, filepath.ToSlash(filepath.Join(site, "manifest.json")))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := decodeStrictJSON(data, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func readManagedFile(root, relativePath string) ([]byte, os.FileMode, error) {
	if !safePublicationPath(relativePath) {
		return nil, 0, fmt.Errorf("unsafe managed path %q", relativePath)
	}
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		return nil, 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("managed path %q must be a regular file", relativePath)
	}
	if info.Size() > maxManagedFileSize {
		return nil, 0, fmt.Errorf("managed file %q exceeds size limit", relativePath)
	}
	file, err := safefs.OpenReadOnly(root, relativePath)
	if err != nil {
		return nil, 0, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxManagedFileSize+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, 0, readErr
	}
	if closeErr != nil {
		return nil, 0, closeErr
	}
	if len(data) > maxManagedFileSize {
		return nil, 0, fmt.Errorf("managed file %q exceeds size limit", relativePath)
	}
	return data, info.Mode().Perm(), nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("contains trailing JSON")
	}
	return nil
}

type publicationPhase int

const (
	publicationPhaseArtifacts publicationPhase = iota
	publicationPhaseSiteCatalog
	publicationPhaseGlobalReport
	publicationPhaseGlobalManifest
)

type publicationFile struct {
	pendingFile
	phase publicationPhase
}

type publicationHooks struct {
	beforeWrite        func(string) error
	beforeStage        func(string, string) error
	beforeSync         func(string) error
	beforeReplace      func(string) error
	afterWrite         func(string)
	beforeRemove       func(string) error
	beforeJournalWrite func(committed bool) error
}

type publicationSnapshot struct {
	path      string
	existed   bool
	mode      os.FileMode
	oldSHA256 string
	newSHA256 string
}

type publicationJournal struct {
	SchemaVersion      int                       `json:"schema_version"`
	BackupDirectory    string                    `json:"backup_directory"`
	Committed          bool                      `json:"committed"`
	Entries            []publicationJournalEntry `json:"entries"`
	CreatedDirectories []string                  `json:"created_directories"`
}

type publicationJournalEntry struct {
	Path      string `json:"path"`
	Existed   bool   `json:"existed"`
	Mode      uint32 `json:"mode"`
	OldSHA256 string `json:"old_sha256,omitempty"`
	NewSHA256 string `json:"new_sha256,omitempty"`
}

func publishSiteFiles(
	ctx context.Context,
	root string,
	files []publicationFile,
	stalePaths []string,
	managed map[string]struct{},
	hooks publicationHooks,
) (retErr error) {
	if ctx == nil {
		return fmt.Errorf("publication context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve publication root: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect publication root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("publication root must be a real directory")
	}
	if err := recoverInterruptedPublication(root); err != nil {
		return err
	}

	files = append([]publicationFile(nil), files...)
	sort.Slice(files, func(i, j int) bool {
		if files[i].phase != files[j].phase {
			return files[i].phase < files[j].phase
		}
		return files[i].path < files[j].path
	})
	writePaths := make(map[string]struct{}, len(files))
	var missingDirectories []string
	for _, file := range files {
		if file.phase < publicationPhaseArtifacts || file.phase > publicationPhaseGlobalManifest {
			return fmt.Errorf("invalid publication phase for %q", file.path)
		}
		if !safePublicationPath(file.path) {
			return fmt.Errorf("refusing to publish unsafe artifact path %q", file.path)
		}
		if len(file.data) > protocol.MaxArtifactBytes {
			return fmt.Errorf("generated file %q exceeds size limit", file.path)
		}
		if _, exists := writePaths[file.path]; exists {
			return fmt.Errorf("refusing to publish duplicate artifact path %q", file.path)
		}
		writePaths[file.path] = struct{}{}
		exists, missingDirs, err := inspectPublicationTarget(root, file.path)
		if err != nil {
			return err
		}
		if exists {
			if _, owned := managed[file.path]; !owned {
				return fmt.Errorf("refusing to replace unmanaged file %q", file.path)
			}
		}
		missingDirectories = append(missingDirectories, missingDirs...)
	}

	staleSet := make(map[string]struct{}, len(stalePaths))
	for _, stale := range stalePaths {
		if !safePublicationPath(stale) {
			return fmt.Errorf("refusing to remove unsafe managed path %q", stale)
		}
		if _, owned := managed[stale]; !owned {
			return fmt.Errorf("refusing to remove unmanaged path %q", stale)
		}
		if _, written := writePaths[stale]; written {
			return fmt.Errorf("path %q cannot be both written and removed", stale)
		}
		if _, duplicate := staleSet[stale]; duplicate {
			return fmt.Errorf("duplicate stale path %q", stale)
		}
		staleSet[stale] = struct{}{}
		exists, _, err := inspectPublicationTarget(root, stale)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("managed artifact %q disappeared before publication", stale)
		}
	}

	invalidateSet := make(map[string]struct{})
	for managedPath := range managed {
		if managedPath == "manifest.json" || managedPath == "generation-report.json" ||
			strings.HasSuffix(managedPath, "/manifest.json") {
			if !safePublicationPath(managedPath) {
				return fmt.Errorf("unsafe previous manifest path %q", managedPath)
			}
			invalidateSet[managedPath] = struct{}{}
		}
	}
	invalidate := make([]string, 0, len(invalidateSet))
	for managedPath := range invalidateSet {
		invalidate = append(invalidate, managedPath)
	}
	sort.Strings(invalidate)

	affected := make(map[string]struct{}, len(files)+len(staleSet)+len(invalidate))
	for managedPath := range writePaths {
		affected[managedPath] = struct{}{}
	}
	for managedPath := range staleSet {
		affected[managedPath] = struct{}{}
	}
	for _, managedPath := range invalidate {
		affected[managedPath] = struct{}{}
	}
	affectedPaths := make([]string, 0, len(affected))
	for managedPath := range affected {
		affectedPaths = append(affectedPaths, managedPath)
	}
	sort.Strings(affectedPaths)

	if _, err := os.Lstat(filepath.Join(root, generationJournalName)); err == nil {
		return recoveryRequiredError("a prior publication journal exists before publishing")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect publication journal: %w", err)
	}
	backupRoot, err := os.MkdirTemp(root, ".infraflow-publish-")
	if err != nil {
		return fmt.Errorf("create publication backup: %w", err)
	}
	journalAttempted := false
	defer func() {
		if journalAttempted {
			return
		}
		if err := removePublicationBackup(root, backupRoot); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("clean publication backup %q: %w", backupRoot, err))
		}
	}()

	newHashes := make(map[string]string, len(files))
	for _, file := range files {
		newHashes[file.path] = protocol.SHA256(file.data)
	}
	snapshots := make([]publicationSnapshot, 0, len(affectedPaths))
	for _, managedPath := range affectedPaths {
		exists, _, err := inspectPublicationTarget(root, managedPath)
		if err != nil {
			return err
		}
		snapshot := publicationSnapshot{path: managedPath, existed: exists}
		if exists {
			data, mode, err := readManagedFile(root, managedPath)
			if err != nil {
				return fmt.Errorf("backup managed file %q: %w", managedPath, err)
			}
			snapshot.mode = mode
			snapshot.oldSHA256 = protocol.SHA256(data)
			if err := safefs.AtomicWrite(backupRoot, managedPath, data, mode); err != nil {
				return fmt.Errorf("save rollback copy for %q: %w", managedPath, err)
			}
		}
		snapshot.newSHA256 = newHashes[managedPath]
		snapshots = append(snapshots, snapshot)
	}
	journal, err := makePublicationJournal(root, filepath.Base(backupRoot), snapshots, missingDirectories)
	if err != nil {
		return err
	}
	if hooks.beforeJournalWrite != nil {
		if err := hooks.beforeJournalWrite(false); err != nil {
			return fmt.Errorf("write publication journal: %w", err)
		}
	}
	if err := writePublicationJournal(root, journal); err != nil {
		return fmt.Errorf("write publication journal: %w", err)
	}
	journalAttempted = true
	if err := ctx.Err(); err != nil {
		return rollbackPublication(root, err)
	}

	remove := func(relativePath string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hooks.beforeRemove != nil {
			if err := hooks.beforeRemove(relativePath); err != nil {
				return err
			}
		}
		return removeManagedPublicationFile(root, relativePath)
	}
	for _, relativePath := range invalidate {
		if _, exists := affected[relativePath]; exists {
			if err := remove(relativePath); err != nil {
				return rollbackPublication(root, fmt.Errorf("invalidate previous manifest %q: %w", relativePath, err))
			}
		}
	}
	for _, relativePath := range stalePaths {
		if _, alreadyInvalidated := invalidateSet[relativePath]; alreadyInvalidated {
			continue
		}
		if err := remove(relativePath); err != nil {
			return rollbackPublication(root, fmt.Errorf("remove obsolete artifact %q: %w", relativePath, err))
		}
	}
	for phase := publicationPhaseArtifacts; phase <= publicationPhaseGlobalManifest; phase++ {
		for _, file := range files {
			if file.phase != phase {
				continue
			}
			if err := ctx.Err(); err != nil {
				return rollbackPublication(root, err)
			}
			if hooks.beforeWrite != nil {
				if err := hooks.beforeWrite(file.path); err != nil {
					return rollbackPublication(root, fmt.Errorf("publish artifact %q: %w", file.path, err))
				}
			}
			atomicHooks := safefs.AtomicWriteHooks{}
			if hooks.beforeSync != nil {
				atomicHooks.BeforeSync = func() error {
					return hooks.beforeSync(file.path)
				}
			}
			if hooks.beforeReplace != nil {
				atomicHooks.BeforeReplace = func() error {
					return hooks.beforeReplace(file.path)
				}
			}
			if err := safefs.AtomicWriteWithHooks(root, file.path, file.data, 0o644, atomicHooks); err != nil {
				return rollbackPublication(root, fmt.Errorf("publish artifact %q: %w", file.path, err))
			}
			if hooks.afterWrite != nil {
				hooks.afterWrite(file.path)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return rollbackPublication(root, err)
	}
	journal.Committed = true
	if hooks.beforeJournalWrite != nil {
		if err := hooks.beforeJournalWrite(true); err != nil {
			return rollbackPublication(root, fmt.Errorf("commit publication journal: %w", err))
		}
	}
	if err := writePublicationJournal(root, journal); err != nil {
		return rollbackPublication(root, fmt.Errorf("commit publication journal: %w", err))
	}
	if err := cleanupPublicationJournal(root, journal); err != nil {
		return fmt.Errorf("%w: generation committed but recovery journal cleanup is incomplete; catalog reads remain blocked until recovery: %w",
			ErrRecoveryRequired, err)
	}
	return nil
}

func inspectPublicationTarget(root, relativePath string) (bool, []string, error) {
	if !safePublicationPath(relativePath) {
		return false, nil, fmt.Errorf("unsafe publication path %q", relativePath)
	}
	target := filepath.Join(root, filepath.FromSlash(relativePath))
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return false, nil, fmt.Errorf("publication path %q escapes output root", relativePath)
	}
	parts := strings.Split(relativePath, "/")
	current := root
	missing := make([]string, 0, len(parts)-1)
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if index < len(parts)-1 {
				missing = append(missing, current)
			}
			continue
		}
		if err != nil {
			return false, nil, fmt.Errorf("inspect publication path %q: %w", relativePath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, nil, fmt.Errorf("publication path %q contains a symlink", relativePath)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return false, nil, fmt.Errorf("publication parent %q is not a directory", current)
		}
		if index == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return false, nil, fmt.Errorf("publication target %q is not a regular file", relativePath)
			}
			return true, missing, nil
		}
	}
	return false, missing, nil
}

func safePublicationPath(relativePath string) bool {
	nativePath := filepath.FromSlash(relativePath)
	if relativePath == "" || strings.Contains(relativePath, "\\") || path.IsAbs(relativePath) ||
		filepath.IsAbs(nativePath) || filepath.VolumeName(nativePath) != "" ||
		path.Clean(relativePath) != relativePath {
		return false
	}
	for _, part := range strings.Split(relativePath, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func removeManagedPublicationFile(root, relativePath string) error {
	exists, _, err := inspectPublicationTarget(root, relativePath)
	if err != nil {
		return err
	}
	if !exists {
		return os.ErrNotExist
	}
	return os.Remove(filepath.Join(root, filepath.FromSlash(relativePath)))
}

func removeCreatedDirectories(root string, directories []string) error {
	unique := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		unique[directory] = struct{}{}
	}
	ordered := make([]string, 0, len(unique))
	for directory := range unique {
		ordered = append(ordered, directory)
	}
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	var cleanupErrors []error
	for _, directory := range ordered {
		relative, err := filepath.Rel(root, directory)
		if err != nil || relative == "." || relative == ".." ||
			strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("directory %q is outside publication root", directory))
			continue
		}
		info, err := os.Lstat(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect directory %q: %w", directory, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("directory %q changed to an unsafe entry", directory))
			continue
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("read directory %q: %w", directory, err))
			continue
		}
		if len(entries) != 0 {
			continue
		}
		if err := os.Remove(directory); err != nil && !os.IsNotExist(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove empty directory %q: %w", directory, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func removePublicationBackup(root, backupRoot string) error {
	if filepath.Dir(backupRoot) != root || !strings.HasPrefix(filepath.Base(backupRoot), ".infraflow-publish-") {
		return fmt.Errorf("refusing to remove unexpected backup path")
	}
	info, err := os.Lstat(backupRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("publication backup is not a real directory")
	}
	return os.RemoveAll(backupRoot)
}

func makePublicationJournal(root, backupDirectory string, snapshots []publicationSnapshot, directories []string) (publicationJournal, error) {
	journal := publicationJournal{
		SchemaVersion:   1,
		BackupDirectory: backupDirectory,
		Entries:         make([]publicationJournalEntry, 0, len(snapshots)),
	}
	for _, snapshot := range snapshots {
		entry := publicationJournalEntry{
			Path:      snapshot.path,
			Existed:   snapshot.existed,
			Mode:      uint32(snapshot.mode.Perm()),
			NewSHA256: snapshot.newSHA256,
		}
		if snapshot.existed {
			entry.OldSHA256 = snapshot.oldSHA256
		}
		journal.Entries = append(journal.Entries, entry)
	}
	seenDirectories := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		relative, err := filepath.Rel(root, directory)
		if err != nil || relative == "." || relative == ".." ||
			strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return publicationJournal{}, fmt.Errorf("publication directory %q is outside output root", directory)
		}
		relative = filepath.ToSlash(relative)
		if !safePublicationPath(relative) {
			return publicationJournal{}, fmt.Errorf("unsafe publication directory %q", relative)
		}
		if _, exists := seenDirectories[relative]; !exists {
			seenDirectories[relative] = struct{}{}
			journal.CreatedDirectories = append(journal.CreatedDirectories, relative)
		}
	}
	sort.Strings(journal.CreatedDirectories)
	if err := validatePublicationJournal(journal); err != nil {
		return publicationJournal{}, err
	}
	return journal, nil
}

func validatePublicationJournal(journal publicationJournal) error {
	if journal.SchemaVersion != 1 || !safePublicationPath(journal.BackupDirectory) ||
		strings.Contains(journal.BackupDirectory, "/") ||
		!strings.HasPrefix(journal.BackupDirectory, ".infraflow-publish-") ||
		len(journal.Entries) == 0 {
		return fmt.Errorf("publication journal has invalid metadata")
	}
	seenPaths := make(map[string]struct{}, len(journal.Entries))
	for _, entry := range journal.Entries {
		if !safePublicationPath(entry.Path) || entry.Mode&^uint32(0o777) != 0 {
			return fmt.Errorf("publication journal contains an unsafe artifact path or mode")
		}
		if _, exists := seenPaths[entry.Path]; exists {
			return fmt.Errorf("publication journal contains duplicate path %q", entry.Path)
		}
		seenPaths[entry.Path] = struct{}{}
		if entry.Existed {
			if !protocol.IsSHA256(entry.OldSHA256) {
				return fmt.Errorf("publication journal has an invalid previous hash for %q", entry.Path)
			}
		} else if entry.OldSHA256 != "" || entry.Mode != 0 {
			return fmt.Errorf("publication journal has previous-file metadata for new path %q", entry.Path)
		}
		if entry.NewSHA256 != "" && !protocol.IsSHA256(entry.NewSHA256) {
			return fmt.Errorf("publication journal has an invalid new hash for %q", entry.Path)
		}
	}
	seenDirectories := make(map[string]struct{}, len(journal.CreatedDirectories))
	for _, directory := range journal.CreatedDirectories {
		if !safePublicationPath(directory) {
			return fmt.Errorf("publication journal contains an unsafe directory path")
		}
		if _, exists := seenDirectories[directory]; exists {
			return fmt.Errorf("publication journal contains duplicate directory %q", directory)
		}
		seenDirectories[directory] = struct{}{}
	}
	return nil
}

func writePublicationJournal(root string, journal publicationJournal) error {
	if err := validatePublicationJournal(journal); err != nil {
		return err
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return fmt.Errorf("encode publication journal: %w", err)
	}
	if len(data) > maxPublicationJournal {
		return fmt.Errorf("publication journal exceeds size limit")
	}
	if err := safefs.AtomicWrite(root, generationJournalName, data, 0o600); err != nil {
		return fmt.Errorf("persist publication journal: %w", err)
	}
	return nil
}

func readPublicationJournal(root string) (publicationJournal, error) {
	info, err := os.Lstat(filepath.Join(root, generationJournalName))
	if err != nil {
		return publicationJournal{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxPublicationJournal {
		return publicationJournal{}, fmt.Errorf("publication journal must be a bounded regular file")
	}
	data, _, err := readManagedFile(root, generationJournalName)
	if err != nil {
		return publicationJournal{}, fmt.Errorf("read publication journal: %w", err)
	}
	var journal publicationJournal
	if err := decodeStrictJSON(data, &journal); err != nil {
		return publicationJournal{}, fmt.Errorf("decode publication journal: %w", err)
	}
	if err := validatePublicationJournal(journal); err != nil {
		return publicationJournal{}, err
	}
	return journal, nil
}

func recoverInterruptedPublication(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("inspect publication recovery state: %w", err)
	}
	var backupDirectories []string
	for _, entry := range entries {
		if entry.Name() == generationJournalName || !strings.HasPrefix(entry.Name(), ".infraflow-publish-") {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, entry.Name()))
		if err != nil {
			return fmt.Errorf("inspect publication backup %q: %w", entry.Name(), err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("publication backup %q is not a real directory", entry.Name())
		}
		backupDirectories = append(backupDirectories, entry.Name())
	}
	journalInfo, err := os.Lstat(filepath.Join(root, generationJournalName))
	if os.IsNotExist(err) {
		if len(backupDirectories) != 0 {
			return recoveryRequiredError("publication backup exists without a journal")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect publication journal: %w", err)
	}
	if journalInfo.Mode()&os.ModeSymlink != 0 || !journalInfo.Mode().IsRegular() {
		return recoveryRequiredError("publication journal must be a regular file")
	}
	journal, err := readPublicationJournal(root)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryRequired, err)
	}
	if journal.Committed {
		if len(backupDirectories) > 1 ||
			(len(backupDirectories) == 1 && backupDirectories[0] != journal.BackupDirectory) {
			return recoveryRequiredError("journal and backup directory do not match")
		}
		if err := cleanupPublicationJournal(root, journal); err != nil {
			return fmt.Errorf("%w: committed publication cleanup failed: %v", ErrRecoveryRequired, err)
		}
		return nil
	}
	if len(backupDirectories) > 1 ||
		(len(backupDirectories) == 1 && backupDirectories[0] != journal.BackupDirectory) {
		return recoveryRequiredError("journal and backup directory do not match")
	}
	backupRoot := filepath.Join(root, journal.BackupDirectory)
	if err := restorePublicationJournal(root, backupRoot, journal); err != nil {
		return fmt.Errorf("%w: restore interrupted publication; recovery data retained: %w", ErrRecoveryRequired, err)
	}
	if err := removeCreatedDirectoriesFromJournal(root, journal.CreatedDirectories); err != nil {
		return fmt.Errorf("%w: clean interrupted publication directories: %w", ErrRecoveryRequired, err)
	}
	if err := cleanupPublicationJournal(root, journal); err != nil {
		return fmt.Errorf("%w: interrupted publication cleanup failed: %w", ErrRecoveryRequired, err)
	}
	return nil
}

func rollbackPublication(root string, cause error) error {
	if err := recoverInterruptedPublication(root); err != nil {
		return errors.Join(cause, fmt.Errorf("%w: rollback failed; recovery data retained: %w", ErrRecoveryRequired, err))
	}
	return cause
}

type publicationRecoveryAction struct {
	entry   publicationJournalEntry
	restore []byte
	remove  bool
}

func restorePublicationJournal(root, backupRoot string, journal publicationJournal) error {
	actions := make([]publicationRecoveryAction, 0, len(journal.Entries))
	for _, entry := range journal.Entries {
		exists, _, err := inspectPublicationTarget(root, entry.Path)
		if err != nil {
			return fmt.Errorf("inspect recovery target %q: %w", entry.Path, err)
		}
		var currentHash string
		if exists {
			current, _, err := readManagedFile(root, entry.Path)
			if err != nil {
				return fmt.Errorf("read recovery target %q: %w", entry.Path, err)
			}
			currentHash = protocol.SHA256(current)
		}
		action := publicationRecoveryAction{entry: entry}
		if entry.Existed {
			if exists && currentHash == entry.OldSHA256 {
				actions = append(actions, action)
				continue
			}
			backupPath := filepath.ToSlash(filepath.Join(journal.BackupDirectory, entry.Path))
			backup, _, err := readManagedFile(root, backupPath)
			if err != nil {
				return fmt.Errorf("read rollback copy for %q: %w", entry.Path, err)
			}
			if protocol.SHA256(backup) != entry.OldSHA256 {
				return fmt.Errorf("rollback copy hash mismatch for %q", entry.Path)
			}
			if exists && (entry.NewSHA256 == "" || currentHash != entry.NewSHA256) {
				return fmt.Errorf("recovery target %q contains unexpected content", entry.Path)
			}
			action.restore = backup
			actions = append(actions, action)
			continue
		}
		if !exists {
			actions = append(actions, action)
			continue
		}
		if entry.NewSHA256 == "" || currentHash != entry.NewSHA256 {
			return fmt.Errorf("new recovery target %q contains unexpected content", entry.Path)
		}
		action.remove = true
		actions = append(actions, action)
	}

	for index := len(actions) - 1; index >= 0; index-- {
		action := actions[index]
		if action.restore != nil {
			if err := safefs.AtomicWrite(root, action.entry.Path, action.restore, os.FileMode(action.entry.Mode)); err != nil {
				return fmt.Errorf("restore %q: %w", action.entry.Path, err)
			}
		} else if action.remove {
			if err := removeManagedPublicationFile(root, action.entry.Path); err != nil {
				return fmt.Errorf("remove new artifact %q: %w", action.entry.Path, err)
			}
		}
	}
	return nil
}

func removeCreatedDirectoriesFromJournal(root string, relativeDirectories []string) error {
	directories := make([]string, 0, len(relativeDirectories))
	for _, relative := range relativeDirectories {
		if !safePublicationPath(relative) {
			return fmt.Errorf("refusing unsafe publication directory %q", relative)
		}
		directories = append(directories, filepath.Join(root, filepath.FromSlash(relative)))
	}
	return removeCreatedDirectories(root, directories)
}

func cleanupPublicationJournal(root string, journal publicationJournal) error {
	if err := validatePublicationJournal(journal); err != nil {
		return err
	}
	backupRoot := filepath.Join(root, journal.BackupDirectory)
	if err := removePublicationBackup(root, backupRoot); err != nil {
		return fmt.Errorf("remove publication backup: %w", err)
	}
	journalPath := filepath.Join(root, generationJournalName)
	info, err := os.Lstat(journalPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect publication journal before cleanup: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to remove unsafe publication journal")
	}
	if err := os.Remove(journalPath); err != nil {
		return fmt.Errorf("remove publication journal: %w", err)
	}
	return nil
}
