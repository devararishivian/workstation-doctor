package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"
)

// RunCommandStep executes one direct-argv command step with process group isolation
// and returns a bounded StepResult.
func RunCommandStep(ctx context.Context, step doctor.CommandStep) (store.StepResult, error) {
	startedAt := time.Now().UTC()
	result := store.StepResult{
		StartedAt: startedAt,
	}

	cmdDef := step.Command
	base := filepath.Base(cmdDef.Executable)
	switch base {
	case "sh", "bash", "dash", "zsh", "fish", "sudo", "doas":
		result.FinishedAt = time.Now().UTC()
		result.Outcome = store.StepOutcomeFailed
		result.SafeError = "shell interpretation and privilege escalation are unsupported"
		return result, errors.New("shell interpretation and privilege escalation are unsupported")
	}

	if !filepath.IsAbs(cmdDef.Executable) {
		result.FinishedAt = time.Now().UTC()
		result.Outcome = store.StepOutcomeFailed
		result.SafeError = "executable path must be absolute"
		return result, errors.New("executable path must be absolute")
	}

	const maxOutputBytes = 16 << 10 // 16 KiB
	var stdout, stderr bytes.Buffer

	cmd := exec.Command(cmdDef.Executable, cmdDef.Args...)
	cmd.Dir = cmdDef.Dir
	if len(cmdDef.Env) > 0 {
		for k, v := range cmdDef.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	setupProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		result.FinishedAt = time.Now().UTC()
		result.Outcome = store.StepOutcomeFailed
		result.SafeError = fmt.Sprintf("start command: %v", err)
		return result, fmt.Errorf("start command: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var waitErr error
	select {
	case <-ctx.Done():
		terminateProcessGroup(cmd)
		// Wait for process to fully exit
		<-done
		result.FinishedAt = time.Now().UTC()
		result.Outcome = store.StepOutcomeCanceled
		result.SafeError = "command canceled or timed out"
		return result, fmt.Errorf("command execution canceled: %w", ctx.Err())
	case waitErr = <-done:
	}

	result.FinishedAt = time.Now().UTC()
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		result.ExitCode = &code
	}

	// Capture output up to limit
	outBytes := stdout.Bytes()
	if len(outBytes) > maxOutputBytes {
		outBytes = outBytes[:maxOutputBytes]
		result.OutputTruncated = true
	}
	result.SafeOutput = strings.ToValidUTF8(string(outBytes), "")

	if waitErr != nil {
		result.Outcome = store.StepOutcomeFailed
		errBytes := stderr.Bytes()
		if len(errBytes) > 1024 {
			errBytes = errBytes[:1024]
		}
		result.SafeError = strings.ToValidUTF8(string(errBytes), "")
		if result.SafeError == "" {
			result.SafeError = waitErr.Error()
		}
		return result, fmt.Errorf("run command: %w", waitErr)
	}

	result.Outcome = store.StepOutcomeCompleted
	return result, nil
}
