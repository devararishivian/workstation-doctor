package doctor

import "context"

func discoverTokenjuice(ctx context.Context, host *Host, scope Scope) Discovery {
	return attachBrewEvidence(ctx, host, scope, discoverNpmTool(ctx, host, scope, "tokenjuice", "tokenjuice", nil), "tokenjuice")
}

func checkTokenjuiceInstance(ctx context.Context, host *Host, scope Scope, instance Instance) []Finding {
	return checkNpmTool(ctx, host, scope, instance, "tokenjuice", "tokenjuice", "https://github.com/vincentkoc/tokenjuice")
}
