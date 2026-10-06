package credentials

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func WriteAdminPassword(password, passwordPath, scriptPath string) error {
	if password == "" || passwordPath == "" || scriptPath == "" {
		return fmt.Errorf("administrator password and output paths are required")
	}
	absPasswordPath, err := filepath.Abs(passwordPath)
	if err != nil {
		return fmt.Errorf("resolve administrator password path: %w", err)
	}
	absScriptPath, err := filepath.Abs(scriptPath)
	if err != nil {
		return fmt.Errorf("resolve administrator password script path: %w", err)
	}
	if err := ensureParent(absPasswordPath); err != nil {
		return err
	}
	if err := ensureParent(absScriptPath); err != nil {
		return err
	}
	if err := os.WriteFile(absPasswordPath, []byte(password+"\n"), 0o600); err != nil {
		return fmt.Errorf("write administrator password: %w", err)
	}
	if err := os.Chmod(absPasswordPath, 0o600); err != nil {
		return fmt.Errorf("restrict administrator password: %w", err)
	}
	script := "#!/bin/sh\nset -eu\numask 077\ncat -- " + shellQuote(absPasswordPath) + "\n"
	if err := os.WriteFile(absScriptPath, []byte(script), 0o700); err != nil {
		return fmt.Errorf("write administrator password script: %w", err)
	}
	if err := os.Chmod(absScriptPath, 0o700); err != nil {
		return fmt.Errorf("restrict administrator password script: %w", err)
	}
	return nil
}

func ensureParent(path string) error {
	parent := filepath.Dir(path)
	if parent == "." {
		return nil
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create credential directory: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
