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

func TestRegistryAcceptsSyntheticCheck(t *testing.T) {
	t.Parallel()
	definition := Definition{
		Integration: Integration{ID: "fixture-extra", Name: "Fixture", Description: "A synthetic registered check."},
		Discover: func(context.Context, *Host, Scope) Discovery {
			return Discovery{Availability: AvailabilityPresent, Instances: []Instance{{ID: "instance", Availability: AvailabilityPresent}}}
		},
		Checks: []CheckDefinition{{ID: "fixture-extra", Name: "Synthetic check", Question: "Does dynamic registration work?", Order: 1, Evaluate: func(context.Context, *Host, Scope, Instance) []Finding {
			return []Finding{{Outcome: OutcomeOK, Explanation: "Registered check ran."}}
		}}},
	}
	engine, err := NewAuditEngine([]Definition{definition}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	report := engine.Audit(t.Context(), testHost(t), Scope{})
	if len(report.Checks) != 1 || report.Checks[0].ID != "fixture-extra" || len(report.Findings) != 1 || report.Findings[0].Outcome != OutcomeOK {
		t.Fatalf("synthetic registration was not dispatched generically: %+v", report)
	}
}

func TestBuiltinRegistryAllChecks(t *testing.T) {
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
	want := []string{"pi", "herdr", "ghostty", "starship", "opencode", "tokenjuice", "serena", "gortex", "ghostty-config-valid", "ghostty-config-version", "herdr-config-valid", "herdr-config-version", "pi-config-valid", "pi-config-version", "brew-outdated", "npm-outdated-g", "pi-packages", "superpowers", "herdr-integr", "herdr-plugins", "skills"}
	checks := map[string]bool{}
	for _, definition := range defs {
		for _, check := range definition.Checks {
			checks[check.ID] = true
		}
	}
	for _, id := range want {
		if !checks[id] {
			t.Fatalf("missing expected check %q", id)
		}
	}
	if len(checks) != len(want) || !slices.Equal(ids, want) || len(report.Integrations) != 15 || len(report.Findings) != 21 {
		t.Fatalf("registry=%v integrations=%d findings=%d", ids, len(report.Integrations), len(report.Findings))
	}
	for _, f := range report.Findings {
		if f.Outcome != OutcomeNotApplicable {
			t.Fatalf("absent tool=%+v", f)
		}
		assertNoAutomatic(t, f)
	}
}
