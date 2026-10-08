package doctor

// BuiltinDefinitions registers only providers implemented in the current stage.
func BuiltinDefinitions() []Definition {
	return []Definition{
		{Integration: Integration{ID: "pi", Name: "Pi", Description: "Coding agent installations in the selected environment.", References: []string{"https://github.com/earendil-works/pi"}, SupportedScopes: []string{"user"}}, Discover: discoverPi, Checks: []CheckDefinition{{ID: "pi", Name: "Pi version", Question: "Is a supported update available?", Order: 1, Evaluate: checkPiInstance}}},
		{Integration: Integration{ID: "opencode", Name: "OpenCode", Description: "OpenCode installations and evidenced update sources.", References: []string{"https://github.com/anomalyco/opencode"}, SupportedScopes: []string{"user"}}, Discover: discoverOpenCode, Checks: []CheckDefinition{{ID: "opencode", Name: "OpenCode version", Question: "Is a supported update available?", Order: 5, Evaluate: checkOpenCodeInstance}}},
		{Integration: Integration{ID: "tokenjuice", Name: "Tokenjuice", Description: "Output compactor installations and evidenced update sources.", References: []string{"https://github.com/vincentkoc/tokenjuice"}, SupportedScopes: []string{"user"}}, Discover: discoverTokenjuice, Checks: []CheckDefinition{{ID: "tokenjuice", Name: "Tokenjuice version", Question: "Is a supported update available?", Order: 6, Evaluate: checkTokenjuiceInstance}}},
	}
}
