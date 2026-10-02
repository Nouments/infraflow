package safefs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func AtomicWrite(root, relativePath string, data []byte, mode fs.FileMode) error {
	return AtomicWriteFromReader(root, relativePath, bytes.NewReader(data), mode, "")
}

func AtomicWriteFromReader(root, relativePath string, source io.Reader, mode fs.FileMode, expectedHash string) error {
	parts, err := safeRelativeParts(relativePath)
	if strings.TrimSpace(root) == "" || err != nil {
		return fmt.Errorf("invalid relative output path")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve output root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return fmt.Errorf("create output root: %w", err)
	}

	parent := absRoot
	for _, part := range parts[:len(parts)-1] {
		parent = filepath.Join(parent, part)
		info, err := os.Lstat(parent)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink")
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}

	target := filepath.Join(parent, parts[len(parts)-1])
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to replace symlink")
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("output target must be a regular file")
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	temporary, err := os.CreateTemp(parent, ".infraflow-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	defer temporary.Close()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	digest := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, digest), source); err != nil {
		return err
	}
	if expectedHash != "" && hex.EncodeToString(digest.Sum(nil)) != expectedHash {
		return fmt.Errorf("output SHA-256 does not match expected hash")
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, target)
}

// OpenReadOnly validates every path component before opening a regular file.
// This prevents a read-only HTTP service from following a symlinked directory
// or file outside the configured workspace.
func OpenReadOnly(root, relativePath string) (*os.File, error) {
	parts, err := safeRelativeParts(relativePath)
	if strings.TrimSpace(root) == "" || err != nil {
		return nil, fmt.Errorf("invalid relative input path")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve input root: %w", err)
	}
	rootInfo, err := os.Lstat(absRoot)
	if err != nil {
		return nil, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("input root must be a real directory")
	}

	current := absRoot
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("input path contains an unsafe directory")
		}
	}
	target := filepath.Join(current, parts[len(parts)-1])
	info, err := os.Lstat(target)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("input target must be a regular file")
	}
	return os.Open(target)
}

func safeRelativeParts(relativePath string) ([]string, error) {
	if relativePath == "" || strings.Contains(relativePath, "\\") || path.IsAbs(relativePath) || path.Clean(relativePath) != relativePath {
		return nil, fmt.Errorf("unsafe relative path")
	}
	parts := strings.Split(relativePath, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("unsafe relative path")
		}
	}
	return parts, nil
}
