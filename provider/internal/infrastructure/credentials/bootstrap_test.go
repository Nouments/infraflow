package credentials

import (
	"os"
	"path/filepath"
	"runtime"
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
	passwordInfo, err := os.Stat(passwordPath)
	if err != nil {
		t.Fatalf("password permissions are not private: %v", err)
	}
	scriptInfo, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("script permissions are not private: %v", err)
	}
	if runtime.GOOS != "windows" {
		if got := passwordInfo.Mode().Perm(); got != 0o600 {
			t.Fatalf("password permissions are %04o, want 0600", got)
		}
		if got := scriptInfo.Mode().Perm(); got != 0o700 {
			t.Fatalf("script permissions are %04o, want 0700", got)
		}
	}
}
