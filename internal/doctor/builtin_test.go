package doctor

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestBuiltinToolFindings(t *testing.T) {
	t.Parallel()
	defs := BuiltinDefinitions()
	if err := ValidateDefinitions(defs); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"pi", "opencode", "tokenjuice"} {
		t.Run(id, func(t *testing.T) {
			found := false
			for _, d := range defs {
				if d.Integration.ID == id {
					found = true
					if len(d.Checks) == 0 || d.Checks[0].ID != id || len(d.Integration.References) == 0 {
						t.Fatalf("definition=%+v", d)
					}
				}
			}
			if !found {
				t.Fatalf("missing %s", id)
			}
		})
	}
}

func TestToolRegistryTenChecks(t *testing.T) {
	t.Parallel()
	defs := BuiltinDefinitions()
	if err := ValidateDefinitions(defs); err != nil {
		t.Fatal(err)
	}
	engine, err := NewAuditEngine(defs, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	host := &Host{OS: "linux", Home: t.TempDir(), Path: t.TempDir(), Env: map[string]string{}}
	host.RunRead = func(context.Context, Command) (CommandResult, error) {
		t.Error("empty audit executed a process")
		return CommandResult{}, errors.New("forbidden")
	}
	host.Fetch = func(context.Context, string) ([]byte, error) {
		t.Error("empty audit requested metadata")
		return nil, errors.New("forbidden")
	}
	report := engine.Audit(t.Context(), host, Scope{})
	ids := []string{}
	for _, check := range report.Checks {
		ids = append(ids, check.ID)
	}
	want := []string{"pi", "herdr", "ghostty", "starship", "opencode", "tokenjuice", "serena", "gortex", "ghostty-config-valid", "ghostty-config-version", "herdr-config-valid", "herdr-config-version", "pi-config-valid", "pi-config-version", "brew-outdated", "npm-outdated-g", "pi-packages", "superpowers"}
	if !slices.Equal(ids, want) || len(report.Integrations) != 12 || len(report.Findings) != 18 {
		t.Fatalf("registry=%v integrations=%d findings=%d", ids, len(report.Integrations), len(report.Findings))
	}
	for _, f := range report.Findings {
		if f.Outcome != OutcomeNotApplicable {
			t.Fatalf("absent tool=%+v", f)
		}
		assertNoAutomatic(t, f)
	}
}
