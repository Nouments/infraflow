package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func BuildFakeCommand(directory, name string) (string, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve fake command source")
	}
	source := filepath.Join(filepath.Dir(currentFile), "testdata", "fake-command.go")
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	output := filepath.Join(directory, name)
	command := exec.Command("go", "build", "-o", output, source)
	command.Env = os.Environ()
	if data, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build fake command: %w\n%s", err, data)
	}
	return output, nil
}
