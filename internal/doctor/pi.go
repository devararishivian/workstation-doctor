package doctor

import (
	"context"
	"path/filepath"
)

func discoverPi(ctx context.Context, host *Host, scope Scope) Discovery {
	d := discoverNpmTool(ctx, host, scope, "pi", "@earendil-works/pi-coding-agent", nil)
	if host == nil {
		return d
	}
	agent := host.Env["PI_CODING_AGENT_DIR"]
	if agent == "" {
		agent = filepath.Join(host.Home, ".pi", "agent")
	}
	state := EvidenceKnown
	if !filepath.IsAbs(agent) {
		state = EvidenceUnsupported
		agent = ""
	}
	for n := range d.Instances {
		d.Instances[n].Configuration = []Fact{{State: state, Label: "Declared agent directory", Value: agent, Source: "PI_CODING_AGENT_DIR or Pi default", ObservedAt: hostNow(host)}}
	}
	return d
}

func checkPiInstance(ctx context.Context, host *Host, scope Scope, instance Instance) []Finding {
	return checkNpmTool(ctx, host, scope, instance, "pi", "@earendil-works/pi-coding-agent", "https://github.com/earendil-works/pi")
}
