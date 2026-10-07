package doctor

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func checkBrewOutdated(ctx context.Context) Result {
	if !commandExists("brew") {
		return unknown("brew-outdated", "-", "-", "brew is not installed")
	}
	out, err := execOut(ctx, 120*time.Second, "", "brew", "outdated")
	if err != nil {
		return unknown("brew-outdated", "-", "-", "cannot run brew outdated")
	}
	if strings.TrimSpace(out) == "" {
		return ok("brew-outdated", "0", "0", "all formulae and casks are current")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	names := strings.Join(lines, " ")
	return needUpdate("brew-outdated", fmt.Sprintf("%d packages", len(lines)), "-",
		"brew upgrade # outdated: "+names, "brew upgrade",
		"outdated: "+names)
}

type brewOutdatedChecker struct{}

func (c *brewOutdatedChecker) Name() string { return "brew-outdated" }

func (c *brewOutdatedChecker) Category() Category { return CategorySystem }

func (c *brewOutdatedChecker) Check(ctx context.Context) Result {
	return checkBrewOutdated(ctx)
}
