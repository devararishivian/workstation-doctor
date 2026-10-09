package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"
)

func TestEntryNoWorkflowSubcommands(t *testing.T) {
	cmd := newRootCommand(func(context.Context, LaunchOptions) error { return nil }, func() bool { return true })
	if len(cmd.Commands) != 0 {
		t.Fatalf("expected 0 workflow subcommands, got %d", len(cmd.Commands))
	}
}

func TestEntryNonTTY(t *testing.T) {
	var stderr bytes.Buffer
	var called bool

	cmd := newRootCommand(func(context.Context, LaunchOptions) error {
		called = true
		return nil
	}, func() bool {
		return false // Non-interactive TTY
	})
	cmd.Root().ErrWriter = &stderr

	err := cmd.Run(t.Context(), []string{"workstation-doctor"})
	if err == nil {
		t.Fatal("expected error on non-TTY execution")
	}
	if called {
		t.Fatal("application started despite non-TTY terminal")
	}

	errOutput := stderr.String()
	if !strings.Contains(errOutput, "An interactive terminal is required.") && !strings.Contains(err.Error(), "An interactive terminal is required.") {
		t.Fatalf("expected exact error 'An interactive terminal is required.', got stderr: %q, err: %v", errOutput, err)
	}
	if strings.Contains(errOutput, "\x1b") {
		t.Fatalf("stderr contains escape codes: %q", errOutput)
	}
	if strings.Contains(errOutput, "1. Check status") {
		t.Fatalf("stderr contains legacy numbered menu: %q", errOutput)
	}
}

func TestStartupNoStorage(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "should-not-exist.db")

	var launched bool
	cmd := newRootCommand(func(_ context.Context, opts LaunchOptions) error {
		launched = true
		if opts.DBPath != dbPath {
			t.Fatalf("DBPath = %q, want %q", opts.DBPath, dbPath)
		}
		return nil
	}, func() bool { return true })

	err := cmd.Run(t.Context(), []string{"workstation-doctor", "--db", dbPath})
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}
	if !launched {
		t.Fatal("start callback was not invoked")
	}
}

func TestTargetAcceptanceMatrix(t *testing.T) {
	t.Run("valid locations and retention", func(t *testing.T) {
		var opts LaunchOptions
		cmd := newRootCommand(func(_ context.Context, o LaunchOptions) error {
			opts = o
			return nil
		}, func() bool { return true })

		absDB := filepath.Join(t.TempDir(), "db.sqlite")
		err := cmd.Run(t.Context(), []string{
			"workstation-doctor",
			"--db", absDB,
			"--project", "/tmp/proj",
			"--location", "pi=/tmp/pi",
			"--skill-root", "/tmp/skills",
			"--history-days", "60",
			"--history-limit", "5000",
			"--history-step-bytes", "8192",
			"--history-action-bytes", "32768",
			"--history-metadata-bytes", "4096",
		})
		if err != nil {
			t.Fatalf("expected valid flags to pass: %v", err)
		}
		if opts.ProjectDir != "/tmp/proj" || len(opts.Locations["pi"]) != 1 || opts.Locations["pi"][0] != "/tmp/pi" {
			t.Fatalf("locations parsing error: %+v", opts)
		}
		if len(opts.SkillRoots) != 1 || opts.SkillRoots[0] != "/tmp/skills" {
			t.Fatalf("skill roots error: %+v", opts)
		}
		if opts.HistoryPolicy.MaxAge != 60*24*time.Hour || opts.HistoryPolicy.MaxTerminal != 5000 {
			t.Fatalf("policy error: %+v", opts.HistoryPolicy)
		}
		if opts.HistoryPolicy.MaxStepOutput != 8192 || opts.HistoryPolicy.MaxActionOutput != 32768 {
			t.Fatalf("policy output error: %+v", opts.HistoryPolicy)
		}
	})

	t.Run("rejects relative db override", func(t *testing.T) {
		cmd := newRootCommand(func(context.Context, LaunchOptions) error { return nil }, func() bool { return true })
		err := cmd.Run(t.Context(), []string{"workstation-doctor", "--db", "relative.db"})
		if err == nil || !strings.Contains(err.Error(), "absolute") {
			t.Fatalf("expected rejection of relative db path, got %v", err)
		}
	})

	t.Run("rejects unknown location integration", func(t *testing.T) {
		cmd := newRootCommand(func(context.Context, LaunchOptions) error { return nil }, func() bool { return true })
		err := cmd.Run(t.Context(), []string{"workstation-doctor", "--location", "unknown-tool=/tmp/path"})
		if err == nil || !strings.Contains(err.Error(), "unknown integration") {
			t.Fatalf("expected rejection of unknown location, got %v", err)
		}
	})

	t.Run("rejects invalid step bytes greater than action bytes", func(t *testing.T) {
		cmd := newRootCommand(func(context.Context, LaunchOptions) error { return nil }, func() bool { return true })
		err := cmd.Run(t.Context(), []string{
			"workstation-doctor",
			"--history-step-bytes", "65536",
			"--history-action-bytes", "16384",
		})
		if err == nil || !strings.Contains(err.Error(), "history step bytes cannot exceed") {
			t.Fatalf("expected rejection of step bytes > action bytes, got %v", err)
		}
	})
}

func TestAllCheckIDsRegistered(t *testing.T) {
	defs := doctor.BuiltinDefinitions()
	var registeredChecks []string
	for _, def := range defs {
		for _, c := range def.Checks {
			registeredChecks = append(registeredChecks, c.ID)
		}
	}
	want := []string{
		"pi", "herdr", "ghostty", "starship", "opencode", "tokenjuice", "serena", "gortex",
		"ghostty-config-valid", "herdr-config-valid",
		"pi-config-valid", "brew-outdated", "npm-outdated-g", "pi-packages",
		"superpowers", "herdr-integr", "herdr-plugins", "skills",
	}
	if len(registeredChecks) != len(want) {
		t.Fatalf("registered checks count = %d, want %d: %v", len(registeredChecks), len(want), registeredChecks)
	}
	checkMap := map[string]bool{}
	for _, c := range registeredChecks {
		checkMap[c] = true
	}
	for _, expected := range want {
		if !checkMap[expected] {
			t.Fatalf("missing expected registered check %q", expected)
		}
	}
}

var _ = store.DefaultPolicy
