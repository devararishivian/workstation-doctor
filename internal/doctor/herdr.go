package doctor

import (
	"context"
)

func discoverHerdr(ctx context.Context, host *Host, scope Scope) Discovery {
	return discoverNativeTool(ctx, host, scope, "herdr", nil)
}

func checkHerdrInstance(ctx context.Context, host *Host, _ Scope, instance Instance) []Finding {
	return checkGithubTool(ctx, host, instance, "herdr", "herdrdev/herdr")
}
