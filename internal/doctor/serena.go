package doctor

import (
	"context"
	"encoding/json"
	"path/filepath"
)

func discoverSerena(ctx context.Context, host *Host, scope Scope) Discovery {
	if paths, ok := scope.Locations["serena"]; ok && len(paths) == 1 && (filepath.Base(paths[0]) == "mcp.json" || filepath.Base(paths[0]) == "mcp-adapter.json") {
		return discoverSerenaDeclaration(ctx, host, paths[0])
	}
	d := discoverNativeTool(ctx, host, scope, "serena", nil)
	if host == nil {
		return d
	}
	inventory, err := UvInventory(ctx, host, scope)
	if err != nil {
		return d
	}
	for n := range d.Instances {
		i := &d.Instances[n]
		for _, item := range inventory.Items {
			if item.Package != "serena-agent" {
				continue
			}
			for _, alias := range item.Executables {
				resolved, e := filepath.EvalSymlinks(alias)
				if e != nil || !samePath(resolved, i.ResolvedPath.Value) {
					continue
				}
				i.Root = Fact{State: EvidenceKnown, Label: "Tool environment", Value: item.Root, Source: "uv receipt entrypoint", ObservedAt: inventory.ObservedAt}
				evidence := *i
				evidence.Executable.Value = alias
				i.Provenance = MatchProvenance(evidence, inventory)
				if item.InstalledVersion != "" {
					i.Version = Fact{State: EvidenceKnown, Label: "Installed version", Value: item.InstalledVersion, Source: "Python distribution METADATA", ObservedAt: inventory.ObservedAt}
				}
			}
		}
	}
	return d
}

func checkSerenaInstance(ctx context.Context, host *Host, _ Scope, instance Instance) []Finding {
	key := FindingKey{IntegrationID: "serena", CheckID: "serena", InstanceID: instance.ID}
	f := Finding{Key: key, Outcome: OutcomeUnknown, Question: "Is a supported update available?", Explanation: "Persistent installation and requested source are separate from a launcher declaration. Automatic source migration is unsupported.", Evidence: []Fact{instance.Version}, References: []PublicReference{{Kind: "documentation", Label: "Official project", URL: "https://github.com/oraios/serena"}}, Actions: []ActionProposal{{ID: "manual-maintenance", Key: key, Mode: ActionManual, Label: "Review Serena update source", Reason: "Inspect requested source and pins before updating."}}}
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
		return []Finding{f}
	}
	if instance.Version.State != EvidenceKnown || instance.Provenance.State != EvidenceKnown || instance.Provenance.Manager != "uv" || host == nil || host.Fetch == nil {
		return []Finding{f}
	}
	raw, err := host.Fetch(ctx, "https://pypi.org/pypi/serena-agent/json")
	if err != nil || len(raw) > int(DefaultLimits().MaxHTTPBytes) {
		if ctx.Err() != nil {
			f.Outcome = OutcomeCanceled
		}
		return []Finding{f}
	}
	var metadata struct{ Info struct{ Version string } }
	if json.Unmarshal(raw, &metadata) != nil {
		return []Finding{f}
	}
	// Only the shared semantic subset is ordered; general PEP 440 versions stay unknown.
	order, err := CompareVersions(instance.Version.Value, metadata.Info.Version, "semver")
	if err != nil {
		return []Finding{f}
	}
	f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "PyPI release", Value: metadata.Info.Version, Source: "official PyPI metadata", Note: "Not proof of availability for a Git or pinned uv source.", ObservedAt: hostNow(host)})
	if order < 0 {
		f.Outcome = OutcomeAttention
	} else {
		f.Outcome = OutcomeOK
	}
	return []Finding{f}
}
