package doctor

import (
	"context"
)

func discoverStarship(ctx context.Context, host *Host, scope Scope) Discovery {
	return discoverNativeTool(ctx, host, scope, "starship", nil)
}

func checkStarshipInstance(ctx context.Context, host *Host, _ Scope, instance Instance) []Finding {
	return checkGithubTool(ctx, host, instance, "starship", "starship/starship")
}
