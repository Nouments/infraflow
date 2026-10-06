package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAdminPasswordCreatesProtectedRetrievalScript(t *testing.T) {
	root := t.TempDir()
	passwordPath := filepath.Join(root, "private", "password")
	scriptPath := filepath.Join(root, "private", "get-password.sh")
	if err := WriteAdminPassword("generated-secret", passwordPath, scriptPath); err != nil {
		t.Fatal(err)
	}
	password, err := os.ReadFile(passwordPath)
	if err != nil || string(password) != "generated-secret\n" {
		t.Fatalf("unexpected password file: %q, %v", password, err)
	}
	script, err := os.ReadFile(scriptPath)
	if err != nil || !strings.Contains(string(script), "cat --") || !strings.Contains(string(script), "password") {
		t.Fatalf("unexpected retrieval script: %q, %v", script, err)
	}
	if info, err := os.Stat(passwordPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("password permissions are not private: %v", err)
	}
	if info, err := os.Stat(scriptPath); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("script permissions are not private: %v", err)
	}
}
