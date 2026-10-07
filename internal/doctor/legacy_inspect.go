package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// userHome returns the user home directory or an error when the
// operating system cannot determine it.
func userHome() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return h, nil
}

// execOut runs a command with a timeout and returns trimmed stdout.
// A non-zero exit that still produces stdout is usable output
// (for example `npm outdated` exits with 1 when packages are outdated).
// An execution failure without usable stdout returns an error.
func execOut(ctx context.Context, timeout time.Duration, dir, name string, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(c, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && trimmed != "" {
			return trimmed, nil
		}
		return "", fmt.Errorf("cannot run %s: %w", commandLine(name, args), err)
	}
	return trimmed, nil
}

func commandLine(name string, args []string) string {
	return strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
