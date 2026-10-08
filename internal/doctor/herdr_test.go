package doctor

import (
	"context"
	"errors"
	"testing"
)

func TestHerdrInstallSources(t *testing.T) {
	t.Parallel()
	host, _ := syntheticBrewTool(t, "herdr", "herdr", "homebrew/core")
	d := discoverHerdr(t.Context(), host, Scope{})
	if len(d.Instances) != 1 || d.Instances[0].Provenance.Manager != "brew" || d.Instances[0].Version.Value != "1.2.3" {
		t.Fatalf("discovery=%+v", d)
	}
	host.Fetch = func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }
	f := checkHerdrInstance(t.Context(), host, Scope{}, d.Instances[0])[0]
	if f.Outcome != OutcomeUnknown || f.Evidence[0].Value != "1.2.3" {
		t.Fatalf("offline=%+v", f)
	}
	assertNoAutomatic(t, f)
}
