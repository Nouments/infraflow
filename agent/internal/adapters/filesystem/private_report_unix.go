//go:build !windows

package filesystem

import (
	"fmt"
	"os"
)

func securePrivateReport(path string) error {
	return os.Chmod(path, 0o600)
}

func verifyPrivateReport(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("report permissions are %04o, want 0600", info.Mode().Perm())
	}
	return nil
}
