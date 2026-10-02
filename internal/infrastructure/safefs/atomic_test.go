package safefs

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteCreatesAndReplacesRegularFile(t *testing.T) {
	root := t.TempDir()
	if err := AtomicWrite(root, "site/inventory.json", []byte("first"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(root, "site/inventory.json", []byte("second"), 0o640); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "site", "inventory.json"))
	if err != nil || string(data) != "second" {
		t.Fatalf("unexpected output %q, %v", data, err)
	}
}

func TestAtomicWriteRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, relativePath := range []string{"../outside", "site/../outside", "/absolute", `site\\file`} {
		if err := AtomicWrite(root, relativePath, []byte("unsafe"), 0o600); err == nil {
			t.Errorf("accepted unsafe path %q", relativePath)
		}
	}
}

func TestAtomicWriteRefusesSymlinkParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "site")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := AtomicWrite(root, "site/inventory.json", []byte("unsafe"), 0o600); err == nil {
		t.Fatal("expected symlink parent to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "inventory.json")); !os.IsNotExist(err) {
		t.Fatalf("write escaped root through symlink: %v", err)
	}
}

func TestAtomicWriteRefusesSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "state.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := AtomicWrite(root, "state.json", []byte("replace"), 0o600); err == nil {
		t.Fatal("expected symlink target to be rejected")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "keep" {
		t.Fatalf("symlink target changed: %q, %v", data, err)
	}
}

func TestAtomicWriteFromReaderVerifiesHashBeforeReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "inventory.json")
	if err := os.WriteFile(path, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("expected"))
	err := AtomicWriteFromReader(root, "inventory.json", strings.NewReader("tampered"), 0o600, hex.EncodeToString(digest[:]))
	if err == nil {
		t.Fatal("expected stream hash mismatch")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "previous" {
		t.Fatalf("failed stream replaced existing file: %q, %v", data, err)
	}
}
