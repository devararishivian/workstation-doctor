package doctor

import (
	"context"
	"errors"
	"testing"
)

func TestOpenCodeManagerSelection(t *testing.T) {
	t.Parallel()
	host, _ := syntheticNpmTool(t, "opencode", "opencode-ai")
	d := discoverOpenCode(t.Context(), host, Scope{})
	if len(d.Instances) != 1 || d.Instances[0].Provenance.Package != "opencode-ai" {
		t.Fatalf("discovery=%+v", d)
	}
	assertNpmProposal(t, host, checkOpenCodeInstance(t.Context(), host, Scope{}, d.Instances[0])[0], "opencode-ai@1.2.4")
	i := d.Instances[0]
	i.Provenance = Provenance{State: EvidenceUnavailable}
	assertNoAutomatic(t, checkOpenCodeInstance(t.Context(), host, Scope{}, i)[0])
	host.Fetch = func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }
	f := checkOpenCodeInstance(t.Context(), host, Scope{}, i)[0]
	if f.Outcome != OutcomeUnknown || len(f.Evidence) == 0 || f.Evidence[0].Value != "1.2.3" {
		t.Fatalf("offline lost installed evidence: %+v", f)
	}
}
