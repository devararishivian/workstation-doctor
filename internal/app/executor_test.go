package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"
)

func TestRunCommandStepDirectArgv(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "out.txt")
	step := doctor.CommandStep{
		Label: "Touch file",
		Command: doctor.Command{
			Executable: "/bin/sh", // We check RunCommandStep rejects shell or executes direct argv
			Args:       []string{"-c", "echo hello > " + outFile},
			Dir:        tmpDir,
		},
	}
	// RunCommandStep must reject shell interpreters
	_, err := RunCommandStep(t.Context(), step)
	if err == nil || !strings.Contains(err.Error(), "shell interpretation") {
		t.Fatalf("expected rejection of shell interpreter, got %v", err)
	}
}

func TestRunCommandStepValidCommand(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not found")
	}
	step := doctor.CommandStep{
		Label: "Run true",
		Command: doctor.Command{
			Executable: truePath,
			Dir:        t.TempDir(),
		},
	}
	res, err := RunCommandStep(t.Context(), step)
	if err != nil {
		t.Fatalf("RunCommandStep failed: %v", err)
	}
	if res.Outcome != store.StepOutcomeCompleted || res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("unexpected step result: %+v", res)
	}
}

func TestRunCommandStepCancellationProcessGroup(t *testing.T) {
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found")
	}
	step := doctor.CommandStep{
		Label: "Sleep",
		Command: doctor.Command{
			Executable: sleepPath,
			Args:       []string{"5"},
			Dir:        t.TempDir(),
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	res, err := RunCommandStep(ctx, step)
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Fatalf("command did not terminate promptly on cancel: took %v", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context deadline error, got %v", err)
	}
	if res.Outcome != store.StepOutcomeCanceled {
		t.Fatalf("expected StepOutcomeCanceled, got %v", res.Outcome)
	}
}

var _ = os.DevNull
