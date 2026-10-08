package doctor

import (
	"context"
	"testing"
)

func TestStarshipUpdateOwnership(t *testing.T) {
	t.Parallel()
	host, _ := syntheticBrewTool(t, "starship", "starship", "homebrew/core")
	d := discoverStarship(t.Context(), host, Scope{})
	if len(d.Instances) != 1 || d.Instances[0].Version.Value != "1.2.3" || d.Instances[0].InstalledAt == nil {
		t.Fatalf("discovery=%+v", d)
	}
	host.Fetch = func(context.Context, string) ([]byte, error) {
		return []byte(`{"tag_name":"v1.2.4","prerelease":false,"draft":false}`), nil
	}
	f := checkStarshipInstance(t.Context(), host, Scope{}, d.Instances[0])[0]
	if f.Outcome != OutcomeAttention {
		t.Fatalf("finding=%+v", f)
	}
	assertNoAutomatic(t, f)
	i := d.Instances[0]
	i.Provenance = Provenance{State: EvidenceUnavailable}
	assertNoAutomatic(t, checkStarshipInstance(t.Context(), host, Scope{}, i)[0])
}
