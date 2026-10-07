package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"workstation-doctor/internal/doctor"

	"github.com/urfave/cli/v3"
)

func okResults() []doctor.Result {
	return []doctor.Result{
		{Component: "pi", Installed: "0.85.1", Latest: "0.85.1", Status: doctor.StatusOK, Note: "npm"},
	}
}

// Program output must carry no escape codes, so pipes stay clean.
func TestLegacyMaintenanceBlocked(t *testing.T) {
	for _, name := range []string{"interactive", "auto yes", "direct flow"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			t.Setenv("PATH", dir)
			t.Setenv("HERDR_CONFIG_PATH", filepath.Join(dir, "config.toml"))
			db := filepath.Join(dir, "history.db")
			marker := filepath.Join(dir, "executed")
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Prevent any legacy command from launching in the RED run.
			var output strings.Builder
			var err error
			if name == "direct flow" {
				err = runFixFlow(ctx, newOutputLogger(&output), db, []doctor.Result{
					{Component: "fixture", Status: doctor.StatusUpdate, Fix: "printf executed > " + marker},
				}, 0, true)
			} else {
				cmd := &cli.Command{
					Writer:         &output,
					ExitErrHandler: func(context.Context, *cli.Command, error) {}, // Inspect errors without exiting the test process.
					Flags:          []cli.Flag{&cli.StringFlag{Name: "db", Value: db}},
					Action:         func(_ context.Context, cmd *cli.Command) error { return doFix(ctx, cmd, name == "auto yes") },
				}
				err = cmd.Run(context.Background(), []string{"doctor"})
			}
			if err == nil || err.Error() != "Automatic maintenance is unavailable during the architecture migration." {
				t.Errorf("maintenance was not blocked: %v", err)
			}
			for _, path := range []string{db, marker} {
				if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("blocked maintenance touched %s: %v", path, statErr)
				}
			}
		})
	}
}

func TestOutputLoggerIsPlain(t *testing.T) {
	var b strings.Builder
	out := newOutputLogger(&b)
	out.Info().Msg("hello")
	if strings.Contains(b.String(), "\x1b") {
		t.Fatalf("output contains escape codes:\n%q", b.String())
	}
}

func TestRenderCheckOK(t *testing.T) {
	var b strings.Builder
	out := newOutputLogger(&b)
	if err := renderCheck(out, okResults(), 7); err != nil {
		t.Fatalf("renderCheck: %v", err)
	}
	for _, want := range []string{"COMPONENT", "Saved run: #7"} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("output misses %q:\n%s", want, b.String())
		}
	}
}

func TestRenderCheckUpdateExitsOne(t *testing.T) {
	var b strings.Builder
	out := newOutputLogger(&b)
	results := []doctor.Result{
		{Component: "pi", Installed: "0.85.0", Latest: "0.85.1", Status: doctor.StatusUpdate, Note: "npm"},
	}
	err := renderCheck(out, results, 0)
	var ec cli.ExitCoder
	if !errors.As(err, &ec) || ec.ExitCode() != 1 {
		t.Fatalf("err = %v, want exit code 1", err)
	}
}
