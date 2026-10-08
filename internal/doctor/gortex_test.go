package doctor

import (
	"context"
	"errors"
	"testing"
)

func TestGortexPlanModeSafety(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"0.64.7", "0.1.0", "development"} {
		t.Run(version, func(t *testing.T) {
			host, _ := syntheticBrewTool(t, "gortex", "gortex", "homebrew/core")
			d := discoverGortex(t.Context(), host, Scope{})
			if len(d.Instances) != 1 {
				t.Fatalf("discovery=%+v", d)
			}
			i := d.Instances[0]
			i.Version.Value = version
			host.RunRead = func(context.Context, Command) (CommandResult, error) {
				t.Error("inspection ran a Gortex preview with unverified startup safety")
				return CommandResult{}, errors.New("forbidden")
			}
			host.Fetch = func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }
			f := checkGortexInstance(t.Context(), host, Scope{}, i)[0]
			if f.Outcome != OutcomeUnknown || f.Evidence[0].Value != version {
				t.Fatalf("offline=%+v", f)
			}
			assertNoAutomatic(t, f)
		})
	}
}
