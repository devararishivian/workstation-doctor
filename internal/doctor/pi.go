package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type piVersionChecker struct{}

func (c *piVersionChecker) Name() string { return "pi" }

func (c *piVersionChecker) Category() Category { return CategoryTool }

func (c *piVersionChecker) Check(ctx context.Context) Result {
	return checkPi(ctx)
}

func checkPi(ctx context.Context) Result {
	if !commandExists("pi") {
		return unknown("pi", "-", "-", "pi binary is not in PATH")
	}
	inst, err := execOut(ctx, 15*time.Second, "", "pi", "--version")
	if err != nil || inst == "" {
		return unknown("pi", "-", "-", "cannot read pi --version")
	}
	latest, err := npmLatest(ctx, "@earendil-works/pi-coding-agent")
	if err != nil || latest == "" {
		return unknown("pi", inst, "-", "cannot read npm registry (offline?)")
	}
	if inst == latest {
		return ok("pi", inst, latest, "npm")
	}
	pkg := "@earendil-works/pi-coding-agent@" + latest
	cmd := "npm i -g " + pkg
	return needUpdate("pi", inst, latest, cmd, cmd, "npm package is outdated")
}

type piConfigValidChecker struct{}

func (c *piConfigValidChecker) Name() string { return "pi-config-valid" }

func (c *piConfigValidChecker) Category() Category { return CategoryConfig }

func (c *piConfigValidChecker) Check(_ context.Context) Result {
	h, err := userHome()
	if err != nil {
		return unknown("pi-config-valid", "-", "-", "cannot determine home directory")
	}
	return evaluatePiConfigValid(filepath.Join(h, ".pi", "agent"))
}

func evaluatePiConfigValid(agentDir string) Result {
	settings := filepath.Join(agentDir, "settings.json")
	mcpAdapter := filepath.Join(agentDir, "mcp-adapter.json")
	mcpLegacy := filepath.Join(agentDir, "mcp.json")
	cache := filepath.Join(agentDir, "mcp-cache.json")
	problems := []string{}

	settingsRaw, err := os.ReadFile(settings)
	if err != nil {
		problems = append(problems, "settings.json is missing")
	} else {
		var v any
		if json.Unmarshal(settingsRaw, &v) != nil {
			problems = append(problems, "settings.json has invalid JSON")
		}
	}

	mcpFilename := "mcp-adapter.json"
	mcpRaw, err := os.ReadFile(mcpAdapter)
	if err != nil && os.IsNotExist(err) {
		if legacyRaw, lErr := os.ReadFile(mcpLegacy); lErr == nil {
			mcpFilename = "mcp.json"
			mcpRaw = legacyRaw
			err = nil
		}
	}

	var mcpParsed struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err != nil {
		problems = append(problems, "mcp-adapter.json is missing")
	} else if json.Unmarshal(mcpRaw, &mcpParsed) != nil {
		problems = append(problems, mcpFilename+" has invalid JSON")
	}

	if _, err := os.Stat(cache); err != nil {
		problems = append(problems, "mcp-cache.json is missing")
	}

	if len(problems) > 0 {
		return unknown("pi-config-valid", "invalid", "valid", "configuration problem: "+strings.Join(problems, "; "))
	}
	return ok("pi-config-valid", "valid", "valid",
		fmt.Sprintf("%d MCP servers, cache is present", len(mcpParsed.MCPServers)))
}

type piConfigVersionChecker struct{}

func (c *piConfigVersionChecker) Name() string { return "pi-config-version" }

func (c *piConfigVersionChecker) Category() Category { return CategoryConfig }

func (c *piConfigVersionChecker) Check(ctx context.Context) Result {
	if !commandExists("pi") {
		return unknown("pi-config-version", "-", "-", "pi binary is not in PATH")
	}
	inst, err := execOut(ctx, 15*time.Second, "", "pi", "--version")
	if err != nil || inst == "" {
		return unknown("pi-config-version", "-", "-", "cannot read pi --version")
	}
	h, err := userHome()
	if err != nil {
		return unknown("pi-config-version", "-", "-", "cannot determine home directory")
	}
	settings := filepath.Join(h, ".pi", "agent", "settings.json")
	raw, err := os.ReadFile(settings)
	if err != nil {
		return unknown("pi-config-version", "-", inst, "settings.json is missing")
	}
	var v struct {
		LastChangelogVersion string `json:"lastChangelogVersion"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return unknown("pi-config-version", "-", inst, "settings.json has invalid JSON")
	}
	return evaluatePiConfigVersion(v.LastChangelogVersion, inst)
}

func evaluatePiConfigVersion(lastChangelogVersion, installedVersion string) Result {
	if lastChangelogVersion == "" {
		return needUpdate("pi-config-version", "unknown", installedVersion,
			"pi /settings", "pi /reload", "lastChangelogVersion is missing in settings.json")
	}
	if lastChangelogVersion == installedVersion {
		return ok("pi-config-version", lastChangelogVersion, installedVersion, "config is in sync with Pi version")
	}
	return needUpdate("pi-config-version", lastChangelogVersion, installedVersion,
		"pi /settings", "pi /reload", "config version is older than installed Pi version")
}

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
