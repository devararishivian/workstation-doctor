package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

type npmPackageChecker struct {
	label string
	pkg   string
}

func newNpmPackageChecker(label, pkg string) *npmPackageChecker {
	return &npmPackageChecker{label: label, pkg: pkg}
}

func (c *npmPackageChecker) Name() string { return c.label }

func (c *npmPackageChecker) Category() Category { return CategoryTool }

func (c *npmPackageChecker) Check(ctx context.Context) Result {
	return checkNpmPkg(ctx, c.label, c.pkg)
}

func npmLatest(ctx context.Context, pkg string) (string, error) {
	return execOut(ctx, 30*time.Second, "", "npm", "view", pkg, "version")
}

func npmGlobalInstalled(ctx context.Context, pkg string) (string, error) {
	out, err := execOut(ctx, 30*time.Second, "", "npm", "list", "-g", "--depth=0", "--json")
	if err != nil {
		return "", err
	}
	var v struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return "", fmt.Errorf("cannot parse npm list output: %w", err)
	}
	return v.Dependencies[pkg].Version, nil
}

func checkNpmPkg(ctx context.Context, label, pkg string) Result {
	if !commandExists("npm") {
		return unknown(label, "-", "-", "npm is not in PATH")
	}
	inst, err := npmGlobalInstalled(ctx, pkg)
	if err != nil {
		return unknown(label, "-", "-", "cannot run npm list")
	}
	if inst == "" {
		return unknown(label, "-", "-", "global npm package was not found")
	}
	latest, err := npmLatest(ctx, pkg)
	if err != nil || latest == "" {
		return unknown(label, inst, "-", "cannot read latest version (offline?)")
	}
	if inst == latest {
		return ok(label, inst, latest, "npm global")
	}
	cmd := fmt.Sprintf("npm i -g %s@%s", pkg, latest)
	return needUpdate(label, inst, latest, cmd, cmd, "global npm package is outdated")
}

func npmOutdatedKeys(ctx context.Context, dir string) (string, error) {
	out, err := execOut(ctx, 60*time.Second, dir, "npm", "outdated", "--json")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "", nil
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return "", fmt.Errorf("cannot parse npm outdated output: %w", err)
	}
	return strings.Join(slices.Sorted(maps.Keys(v)), " "), nil
}

func checkNpmOutdatedGlobal(ctx context.Context) Result {
	if !commandExists("npm") {
		return unknown("npm-outdated-g", "-", "-", "npm is not installed")
	}
	list, err := npmOutdatedKeys(ctx, "")
	if err != nil {
		return unknown("npm-outdated-g", "-", "-", "cannot run npm outdated")
	}
	if list == "" {
		return ok("npm-outdated-g", "0", "0", "all global packages are current")
	}
	return needUpdate("npm-outdated-g", list, "-",
		"npm update -g # outdated: "+list, "npm update -g",
		"global packages are outdated")
}

type npmOutdatedGlobalChecker struct{}

func (c *npmOutdatedGlobalChecker) Name() string { return "npm-outdated-g" }

func (c *npmOutdatedGlobalChecker) Category() Category { return CategorySystem }

func (c *npmOutdatedGlobalChecker) Check(ctx context.Context) Result {
	return checkNpmOutdatedGlobal(ctx)
}
