package doctor

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

type gortexVersionChecker struct{}

func (c *gortexVersionChecker) Name() string { return "gortex" }

func (c *gortexVersionChecker) Category() Category { return CategoryTool }

func (c *gortexVersionChecker) Check(ctx context.Context) Result {
	return checkGortex(ctx)
}

func checkGortex(ctx context.Context) Result {
	if !commandExists("gortex") {
		return unknown("gortex", "-", "-", "gortex binary is not in PATH")
	}
	out, err := execOut(ctx, 15*time.Second, "", "gortex", "version")
	if err != nil {
		return unknown("gortex", "-", "-", "cannot read gortex version")
	}
	inst := ""
	if line, _, _ := strings.Cut(out, "\n"); line != "" {
		if f := strings.Fields(line); len(f) >= 2 {
			inst = f[1]
		}
	}
	if inst == "" {
		return unknown("gortex", "-", "-", "cannot read gortex version")
	}
	// `gortex upgrade` without --run only prints the plan. It reports
	// "already the latest" when the version is current.
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, "gortex", "upgrade")
	upOut, err := cmd.CombinedOutput()
	if err != nil {
		return unknown("gortex", inst, "-", "cannot run gortex upgrade")
	}
	if strings.Contains(strings.ToLower(string(upOut)), "already the latest") {
		return ok("gortex", inst, inst, "self-update: latest")
	}
	return needUpdate("gortex", inst, "latest?",
		"gortex upgrade --run", "gortex upgrade --run",
		"update is available (inspect with: gortex upgrade)")
}
