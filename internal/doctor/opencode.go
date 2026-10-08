package doctor

import (
	"context"
	"path/filepath"
)

func discoverOpenCode(ctx context.Context, host *Host, scope Scope) Discovery {
	var defaults []string
	if host != nil {
		defaults = []string{filepath.Join(host.Home, "bin"), filepath.Join(host.Home, ".opencode", "bin")}
		for _, key := range []string{"OPENCODE_INSTALL_DIR", "XDG_BIN_DIR"} {
			if root := host.Env[key]; root != "" {
				if _, explicit := scope.Locations["opencode"]; !explicit {
					scope = cloneScope(scope)
					scope.Locations["opencode"] = []string{filepath.Join(root, "opencode")}
				}
				break
			}
		}
	}
	return attachBrewEvidence(ctx, host, scope, discoverNpmTool(ctx, host, scope, "opencode", "opencode-ai", defaults), "opencode")
}

func checkOpenCodeInstance(ctx context.Context, host *Host, scope Scope, instance Instance) []Finding {
	return checkNpmTool(ctx, host, scope, instance, "opencode", "opencode-ai", "https://github.com/anomalyco/opencode")
}
