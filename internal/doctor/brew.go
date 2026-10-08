package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

func discoverBrew(ctx context.Context, host *Host, scope Scope) Discovery {
	return discoverManagerPackages(ctx, host, scope, "brew", BrewInventory)
}

func checkBrewInventory(ctx context.Context, host *Host, scope Scope, instance Instance) []Finding {
	selected := instanceManagerHost(host, instance)
	inventory, err := BrewInventory(ctx, selected, scope)
	key := FindingKey{IntegrationID: "brew", CheckID: "brew-outdated", InstanceID: instance.ID}
	f := Finding{Key: key, Outcome: OutcomeUnknown, Question: "Does the selected manager establish an update?", Explanation: "Local receipts do not establish update availability. Homebrew repository metadata was not refreshed. Cask inspection is unsupported.", Evidence: []Fact{instance.Version, {State: EvidenceUnavailable, Label: "Manager update availability", Source: "local Homebrew receipt", Note: inventory.FreshnessNote, ObservedAt: inventory.ObservedAt}}, References: []PublicReference{{Kind: "documentation", Label: "Official Homebrew documentation", URL: "https://docs.brew.sh/Manpage"}}}
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
		return []Finding{f}
	}
	if err != nil || instance.Provenance.Package == "" || selected == nil || selected.Fetch == nil {
		return []Finding{f}
	}
	if !strings.HasPrefix(instance.Provenance.Package, "homebrew/core/") {
		return []Finding{f}
	}
	name := strings.TrimPrefix(instance.Provenance.Package, "homebrew/core/")
	if !validFormulaName(name) {
		return []Finding{f}
	}
	raw, err := selected.Fetch(ctx, "https://formulae.brew.sh/api/formula/"+url.PathEscape(name)+".json")
	if err != nil || len(raw) > int(DefaultLimits().MaxHTTPBytes) {
		if ctx.Err() != nil {
			f.Outcome = OutcomeCanceled
		}
		return []Finding{f}
	}
	var metadata struct {
		Name     string
		Versions struct{ Stable string }
	}
	if json.Unmarshal(raw, &metadata) != nil || metadata.Name != name || !validText(metadata.Versions.Stable, DefaultLimits().MaxIdentifierBytes) || metadata.Versions.Stable == "" {
		return []Finding{f}
	}
	f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Published formula version", Value: metadata.Versions.Stable, Source: "official Homebrew formula API", Note: "Not an established update decision for the installed revision or architecture.", ObservedAt: hostNow(host)})
	return []Finding{f}
}
