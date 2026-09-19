package doctor

import (
	"strings"
	"testing"
)

// The table must stay free of escape codes when stdout is not a
// terminal, so scripts can pipe it. lipgloss handles that; this test
// locks the behavior in.
func TestFormatTableIsPlainWhenPiped(t *testing.T) {
	out := FormatTable([]Result{
		{Component: "pi", Installed: "0.85.1", Latest: "0.85.1", Status: StatusOK, Note: "npm"},
	})
	if strings.Contains(out, "\x1b") {
		t.Fatalf("table contains escape codes:\n%q", out)
	}
	for _, want := range []string{"COMPONENT", "INSTALLED", "LATEST", "STATUS", "NOTE", "Summary:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table misses %q:\n%s", want, out)
		}
	}
}

func TestFormatManualEmpty(t *testing.T) {
	if got := FormatManual(nil); !strings.Contains(got, "No action is needed") {
		t.Fatalf("unexpected manual output:\n%s", got)
	}
}

func TestFormatManualListsPending(t *testing.T) {
	got := FormatManual([]Result{
		{Component: "pi", Installed: "0.85.0", Latest: "0.85.1", Status: StatusUpdate, Note: "npm", Manual: "npm i -g x", Fix: "npm i -g x"},
	})
	if !strings.Contains(got, "1. npm i -g x") {
		t.Fatalf("unexpected manual output:\n%s", got)
	}
}
