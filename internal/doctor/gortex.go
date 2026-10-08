package doctor

import (
	"context"
)

func discoverGortex(ctx context.Context, host *Host, scope Scope) Discovery {
	return discoverNativeTool(ctx, host, scope, "gortex", nil)
}

func checkGortexInstance(ctx context.Context, host *Host, _ Scope, instance Instance) []Finding {
	findings := checkGithubTool(ctx, host, instance, "gortex", "zzet/gortex")
	for n := range findings {
		findings[n].Explanation += " Native preview startup safety is not established; no upgrade command was run."
		for a := range findings[n].Actions {
			findings[n].Actions[a].SideEffects = []string{"Native upgrades can migrate agent configuration and restart the daemon or supervised service.", "Installer-based native updates can execute a shell pipeline; they remain manual-only."}
		}
	}
	return findings
}
