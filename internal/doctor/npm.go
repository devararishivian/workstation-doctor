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

func discoverNpm(ctx context.Context, host *Host, scope Scope) Discovery {
	return discoverManagerPackages(ctx, host, scope, "npm", NpmInventory)
}

func checkNpmGlobal(ctx context.Context, host *Host, scope Scope, instance Instance) []Finding {
	selected := instanceManagerHost(host, instance)
	inventory, err := NpmInventory(ctx, selected, scope)
	key := FindingKey{IntegrationID: "npm", CheckID: "npm-outdated-g", InstanceID: instance.ID}
	base := Finding{Key: key, Outcome: OutcomeUnknown, Question: "Are selected global packages up to date?", Evidence: []Fact{instance.Version, {State: EvidenceKnown, Label: "Inventory freshness", Source: "local global package metadata", Note: inventory.FreshnessNote, ObservedAt: inventory.ObservedAt}}, Explanation: "Global inventory is unavailable or incomplete."}
	if ctx.Err() != nil {
		base.Outcome = OutcomeCanceled
		return []Finding{base}
	}
	if err != nil {
		return []Finding{base}
	}
	if instance.Provenance.Package == "" {
		base.Outcome = OutcomeNotApplicable
		base.Explanation = "No global packages were found in the selected root candidate."
		return []Finding{base}
	}
	for _, item := range inventory.Items {
		if item.Package != instance.Provenance.Package || !samePath(item.Root, instance.Root.Value) {
			continue
		}
		if item.RequestedPin != "" {
			base.Explanation = "A requested pin prevents automatic changes. Global currentness is not established."
			return []Finding{base}
		}
		findings := checkNpmTool(ctx, selected, scope, instance, "npm-outdated-g", item.Package, "https://docs.npmjs.com/cli/v11/commands/npm-outdated")
		for n := range findings {
			findings[n].Key = key
			for a := range findings[n].Actions {
				findings[n].Actions[a].Key = key
			}
			findings[n].Evidence = append(findings[n].Evidence, base.Evidence[1])
		}
		return findings
	}
	return []Finding{base}
}
