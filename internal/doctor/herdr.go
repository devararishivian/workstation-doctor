package doctor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type herdrVersionChecker struct{}

func (c *herdrVersionChecker) Name() string { return "herdr" }

func (c *herdrVersionChecker) Category() Category { return CategoryTool }

func (c *herdrVersionChecker) Check(ctx context.Context) Result {
	return checkHerdr(ctx)
}

func checkHerdr(ctx context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr", "-", "-", "herdr binary is not in PATH")
	}
	out, err := execOut(ctx, 15*time.Second, "", "herdr", "--version")
	if err != nil {
		return unknown("herdr", "-", "-", "cannot read herdr --version")
	}
	inst := ""
	if f := strings.Fields(out); len(f) >= 2 {
		inst = f[1]
	}
	if inst == "" {
		return unknown("herdr", "-", "-", "cannot read herdr --version")
	}
	info, err := execOut(ctx, 60*time.Second, "", "brew", "info", "--json=v2", "herdr")
	if err != nil {
		return unknown("herdr", inst, "-", "cannot read brew info (offline?)")
	}
	var v struct {
		Formulae []struct {
			Versions struct {
				Stable string `json:"stable"`
			} `json:"versions"`
		} `json:"formulae"`
	}
	latest := ""
	if json.Unmarshal([]byte(info), &v) == nil && len(v.Formulae) > 0 {
		latest = v.Formulae[0].Versions.Stable
	}
	if latest == "" {
		return unknown("herdr", inst, "-", "cannot read brew info (offline?)")
	}
	if inst == latest {
		return ok("herdr", inst, latest, "brew")
	}
	return needUpdate("herdr", inst, latest, "brew upgrade herdr", "brew upgrade herdr", "brew formula is outdated")
}

func checkHerdrIntegrations(ctx context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr-integr", "-", "-", "herdr is not installed")
	}
	out, err := execOut(ctx, 30*time.Second, "", "herdr", "integration", "status")
	if err != nil || strings.TrimSpace(out) == "" {
		return unknown("herdr-integr", "-", "-", "cannot read integration status")
	}
	need := []string{}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "pi:") && !strings.Contains(line, "current") {
			need = append(need, "pi")
		}
		if strings.HasPrefix(line, "opencode:") && !strings.Contains(line, "current") {
			need = append(need, "opencode")
		}
	}
	if len(need) == 0 {
		return ok("herdr-integr", "pi,opencode", "current", "agent state hooks are in sync")
	}
	targets := strings.Join(need, " ")
	cmds := make([]string, 0, len(need))
	for _, t := range need {
		cmds = append(cmds, "herdr integration install "+t)
	}
	return needUpdate("herdr-integr", "stale", "current",
		strings.Join(cmds, "\n  "),
		strings.Join(cmds, " && "),
		"stale hooks: "+targets)
}

type herdrConfigValidChecker struct{}

func (c *herdrConfigValidChecker) Name() string { return "herdr-config-valid" }

func (c *herdrConfigValidChecker) Category() Category { return CategoryConfig }

func (c *herdrConfigValidChecker) Check(ctx context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr-config-valid", "-", "-", "herdr is not installed")
	}
	out, err := execOut(ctx, 15*time.Second, "", "herdr", "config", "check")
	if err != nil {
		return unknown("herdr-config-valid", "invalid", "valid", "herdr config check failed: "+err.Error())
	}
	if strings.Contains(strings.ToLower(out), "ok") {
		return ok("herdr-config-valid", "valid", "valid", "herdr config is valid")
	}
	return unknown("herdr-config-valid", "check", "valid", out)
}

type herdrConfigVersionChecker struct{}

func (c *herdrConfigVersionChecker) Name() string { return "herdr-config-version" }

func (c *herdrConfigVersionChecker) Category() Category { return CategoryConfig }

func (c *herdrConfigVersionChecker) Check(_ context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr-config-version", "-", "-", "herdr is not installed")
	}
	path, err := herdrConfigPath()
	if err != nil {
		return unknown("herdr-config-version", "-", "-", "cannot determine herdr config path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ok("herdr-config-version", "default", "default", "default configuration is active")
		}
		return unknown("herdr-config-version", "-", "-", "cannot read config.toml: "+err.Error())
	}
	content := string(raw)
	missing := []string{}
	for _, section := range []string{"[ui]", "[theme]", "[[keys.command]]"} {
		if !strings.Contains(content, section) {
			missing = append(missing, section)
		}
	}
	if len(missing) > 0 {
		return needUpdate("herdr-config-version", "legacy", "current",
			"herdr config check", "herdr config check",
			"missing modern sections: "+strings.Join(missing, ", "))
	}
	return ok("herdr-config-version", "current", "current", "config.toml structure is current")
}

func herdrConfigPath() (string, error) {
	if cfg := os.Getenv("HERDR_CONFIG_PATH"); cfg != "" {
		return cfg, nil
	}
	h, err := userHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".config", "herdr", "config.toml"), nil
}

type herdrIntegrationsChecker struct{}

func (c *herdrIntegrationsChecker) Name() string { return "herdr-integr" }

func (c *herdrIntegrationsChecker) Category() Category { return CategorySystem }

func (c *herdrIntegrationsChecker) Check(ctx context.Context) Result {
	return checkHerdrIntegrations(ctx)
}
