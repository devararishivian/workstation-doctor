package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBrewInventoryFreshness(t *testing.T) {
	t.Parallel()
	host, _ := syntheticBrewTool(t, "starship", "starship", "homebrew/core")
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
