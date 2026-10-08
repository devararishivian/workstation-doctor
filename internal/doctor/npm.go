package doctor

import (
	"context"
)

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
