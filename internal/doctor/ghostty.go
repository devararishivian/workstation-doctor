package doctor

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"
)

type ghosttyVersionChecker struct{}

func (c *ghosttyVersionChecker) Name() string { return "ghostty" }

func (c *ghosttyVersionChecker) Category() Category { return CategoryTool }

func (c *ghosttyVersionChecker) Check(ctx context.Context) Result {
	return checkGhostty(ctx)
}

const ghosttyBin = "/Applications/Ghostty.app/Contents/MacOS/ghostty"

func checkGhostty(ctx context.Context) Result {
	if _, err := os.Stat(ghosttyBin); err != nil {
		return unknown("ghostty", "-", "-", "Ghostty.app was not found")
	}
	out, err := execOut(ctx, 15*time.Second, "", ghosttyBin, "--version")
	if err != nil {
		return unknown("ghostty", "-", "-", "cannot read ghostty --version")
	}
	inst := ""
	if line, _, _ := strings.Cut(out, "\n"); line != "" {
		if f := strings.Fields(line); len(f) >= 2 {
			inst = f[len(f)-1]
		}
	}
	if inst == "" {
		return unknown("ghostty", "-", "-", "cannot read ghostty --version")
	}
	info, err := execOut(ctx, 60*time.Second, "", "brew", "info", "--cask", "--json=v2", "ghostty")
	if err != nil {
		return unknown("ghostty", inst, "-", "cannot read brew cask info (offline?)")
	}
	var v struct {
		Casks []struct {
			Version string `json:"version"`
		} `json:"casks"`
	}
	latest := ""
	if json.Unmarshal([]byte(info), &v) == nil && len(v.Casks) > 0 {
		latest, _, _ = strings.Cut(v.Casks[0].Version, ",")
	}
	if latest == "" {
		return unknown("ghostty", inst, "-", "cannot read brew cask info (offline?)")
	}
	if inst == latest {
		return ok("ghostty", inst, latest, "brew cask")
	}
	return needUpdate("ghostty", inst, latest,
		"brew upgrade --cask ghostty", "brew upgrade --cask ghostty", "cask is outdated")
}

type ghosttyConfigValidChecker struct{}

func (c *ghosttyConfigValidChecker) Name() string { return "ghostty-config-valid" }

func (c *ghosttyConfigValidChecker) Category() Category { return CategoryConfig }

func (c *ghosttyConfigValidChecker) Check(ctx context.Context) Result {
	if _, err := os.Stat(ghosttyBin); err != nil {
		return unknown("ghostty-config-valid", "-", "-", "Ghostty.app was not found")
	}
	out, err := execOut(ctx, 15*time.Second, "", ghosttyBin, "+show-config", "--changes-only")
	if err != nil && out == "" {
		return unknown("ghostty-config-valid", "invalid", "valid", "configuration failed validation, inspect it manually")
	}
	return ok("ghostty-config-valid", "valid", "valid", "configuration is valid")
}

type ghosttyConfigVersionChecker struct{}

func (c *ghosttyConfigVersionChecker) Name() string { return "ghostty-config-version" }

func (c *ghosttyConfigVersionChecker) Category() Category { return CategoryConfig }

func (c *ghosttyConfigVersionChecker) Check(ctx context.Context) Result {
	if _, err := os.Stat(ghosttyBin); err != nil {
		return unknown("ghostty-config-version", "-", "-", "Ghostty.app was not found")
	}
	out, err := execOut(ctx, 15*time.Second, "", ghosttyBin, "+show-config", "--changes-only")
	if err != nil && out == "" {
		return unknown("ghostty-config-version", "-", "-", "cannot read ghostty configuration")
	}
	lower := strings.ToLower(out)
	if strings.Contains(lower, "deprecated") || strings.Contains(lower, "obsolete") {
		return needUpdate("ghostty-config-version", "deprecated options", "current",
			"ghostty +show-config --changes-only", "ghostty +show-config --changes-only",
			"configuration contains deprecated options")
	}
	return ok("ghostty-config-version", "current", "current", "configuration options are current")
}
