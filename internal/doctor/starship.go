package doctor

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type starshipVersionChecker struct{}

func (c *starshipVersionChecker) Name() string { return "starship" }

func (c *starshipVersionChecker) Category() Category { return CategoryTool }

func (c *starshipVersionChecker) Check(ctx context.Context) Result {
	return checkStarship(ctx)
}

func checkStarship(ctx context.Context) Result {
	if !commandExists("starship") {
		return unknown("starship", "-", "-", "starship binary is not in PATH")
	}
	out, err := execOut(ctx, 15*time.Second, "", "starship", "--version")
	if err != nil {
		return unknown("starship", "-", "-", "cannot read starship --version")
	}
	inst := ""
	if line, _, _ := strings.Cut(out, "\n"); line != "" {
		if f := strings.Fields(line); len(f) >= 2 {
			inst = f[1]
		}
	}
	if inst == "" {
		return unknown("starship", "-", "-", "cannot read starship --version")
	}
	info, err := execOut(ctx, 60*time.Second, "", "brew", "info", "--json=v2", "starship")
	if err != nil {
		return unknown("starship", inst, "-", "cannot read brew info (offline?)")
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
		return unknown("starship", inst, "-", "cannot read brew info (offline?)")
	}
	return evaluateStarshipVersion(inst, latest)
}

func evaluateStarshipVersion(inst, latest string) Result {
	if inst == latest {
		return ok("starship", inst, latest, "brew")
	}
	return needUpdate("starship", inst, latest, "brew upgrade starship", "brew upgrade starship", "brew formula is outdated")
}
