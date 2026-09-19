package main

import (
	"errors"
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
