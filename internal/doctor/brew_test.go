package doctor

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrewInventoryFreshness(t *testing.T) {
	t.Parallel()
	host, _ := syntheticBrewTool(t, "starship", "starship", "homebrew/core")
	host.RunRead = nil // Test receipt-only fallback without brew CLI
	d := discoverBrew(t.Context(), host, Scope{})
	if len(d.Instances) != 1 || d.Instances[0].Provenance.Package != "homebrew/core/starship" {
		t.Fatalf("discovery=%+v", d)
	}
	host.Fetch = func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }
	f := checkBrewInventory(t.Context(), host, Scope{}, d.Instances[0])[0]
	if f.Outcome != OutcomeUnknown || len(f.Evidence) < 2 || !strings.Contains(f.Evidence[1].Note, "not refreshed") {
		t.Fatalf("freshness=%+v", f)
	}
	assertNoAutomatic(t, f)
	host.Fetch = func(context.Context, string) ([]byte, error) {
		return []byte(`{"name":"starship","versions":{"stable":"1.2.4"}}`), nil
	}
	f = checkBrewInventory(t.Context(), host, Scope{}, d.Instances[0])[0]
	assertNoAutomatic(t, f)
	if f.Outcome == OutcomeOK {
		t.Fatal("published metadata was treated as an established local manager decision")
	}
}

func TestBrewOutdatedCliInspection(t *testing.T) {
	t.Parallel()
	host, exe := syntheticBrewTool(t, "starship", "starship", "homebrew/core")
	d := discoverBrew(t.Context(), host, Scope{})
	if len(d.Instances) != 1 {
		t.Fatalf("discovery=%+v", d)
	}

	brewExe := filepath.Join(filepath.Dir(exe), "brew")

	// Scenario 1: Formula is current
	host.RunRead = func(_ context.Context, cmd Command) (CommandResult, error) {
		if cmd.Executable == brewExe && len(cmd.Args) == 2 && cmd.Args[0] == "outdated" && cmd.Args[1] == "--json=v2" {
			return CommandResult{Stdout: []byte(`{"formulae":[],"casks":[]}`)}, nil
		}
		return CommandResult{}, errors.New("unexpected command")
	}

	f := checkBrewInventory(t.Context(), host, Scope{}, d.Instances[0])[0]
	if f.Outcome != OutcomeOK {
		t.Fatalf("expected OutcomeOK when brew outdated reports empty, got %+v", f)
	}
	if !strings.Contains(f.Explanation, "current according to local Homebrew metadata") {
		t.Fatalf("unexpected explanation: %s", f.Explanation)
	}

	// Scenario 2: Formula is outdated
	host.inventory = &auditInventory{} // reset cache
	host.RunRead = func(_ context.Context, cmd Command) (CommandResult, error) {
		if cmd.Executable == brewExe && len(cmd.Args) == 2 && cmd.Args[0] == "outdated" && cmd.Args[1] == "--json=v2" {
			return CommandResult{Stdout: []byte(`{"formulae":[{"name":"starship","current_version":"1.27.0"}],"casks":[]}`)}, nil
		}
		return CommandResult{}, errors.New("unexpected command")
	}

	f = checkBrewInventory(t.Context(), host, Scope{}, d.Instances[0])[0]
	if f.Outcome != OutcomeAttention {
		t.Fatalf("expected OutcomeAttention when brew outdated reports outdated formula, got %+v", f)
	}
	if len(f.Actions) != 1 || f.Actions[0].Mode != ActionManual {
		t.Fatalf("expected manual upgrade action, got %+v", f.Actions)
	}
}
