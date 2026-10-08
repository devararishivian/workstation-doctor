package doctor

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
)

// discoverManagerPackages gives each package its own inspectable identity.
// An empty inventory retains a manager-scope instance rather than inventing health.
func discoverManagerPackages(ctx context.Context, host *Host, scope Scope, id string, read func(context.Context, *Host, Scope) (Inventory, error)) Discovery {
	d := ExecutableCandidates(ctx, host, scope, id, id, nil)
	if host == nil {
		return d
	}
	var instances []Instance
	for _, manager := range d.Instances {
		selected := selectedManagerHost(host, manager.Executable.Value)
		inventory, err := read(ctx, selected, scope)
		if err != nil {
			d.Availability = AvailabilityUndetermined
			d.Diagnostics = append(d.Diagnostics, "Manager inventory coverage is incomplete.")
		}
		if len(inventory.Items) == 0 {
			manager.Root = Fact{State: EvidenceKnown, Label: "Selected manager inventory root", Value: inventory.Root, Source: "manager location and explicit prefix candidate", Note: inventory.FreshnessNote, ObservedAt: inventory.ObservedAt}
			manager.Provenance = Provenance{State: EvidenceUnavailable}
			manager.Version = Fact{State: EvidenceUnavailable, Label: "Package version", Note: "No package observation is available."}
			instances = append(instances, manager)
			continue
		}
		for _, item := range inventory.Items {
			identity, e := CanonicalInstanceID(id, "user", item.Root)
			if e != nil {
				d.Availability = AvailabilityUndetermined
				d.Diagnostics = append(d.Diagnostics, "Package identity could not be established.")
				continue
			}
			i := Instance{ID: identity, IntegrationID: id, Scope: "user", Availability: AvailabilityPresent, Active: manager.Active, DiscoverySource: "selected global inventory package", ObservedAt: inventory.ObservedAt, Root: Fact{State: EvidenceKnown, Label: "Package root", Value: item.Root, Source: "global manager inventory", ObservedAt: inventory.ObservedAt}, Version: Fact{State: EvidenceKnown, Label: "Installed version", Value: item.InstalledVersion, Source: "global manager inventory", ObservedAt: inventory.ObservedAt}, Provenance: Provenance{State: EvidenceKnown, Manager: id, Package: item.Package, Root: item.Root, Channel: item.Channel}, InstalledAt: item.InstalledAt, Configuration: []Fact{{State: EvidenceKnown, Label: "Manager executable", Value: inventory.Executable, Source: "selected manager", ObservedAt: inventory.ObservedAt}, {State: EvidenceKnown, Label: "Inventory freshness", Source: "local manager metadata", Note: inventory.FreshnessNote, ObservedAt: inventory.ObservedAt}}}
			if len(item.Executables) > 0 {
				i.Executable = Fact{State: EvidenceKnown, Label: "Package executable", Value: item.Executables[0], Source: "manager entrypoint linkage", ObservedAt: inventory.ObservedAt}
				path, e := filepath.EvalSymlinks(item.Executables[0])
				if e == nil {
					i.ResolvedPath = Fact{State: EvidenceKnown, Label: "Resolved executable", Value: path, Source: "symlink resolution", ObservedAt: inventory.ObservedAt}
				}
			}
			instances = append(instances, i)
		}
	}
	slices.SortFunc(instances, func(a, b Instance) int { return strings.Compare(a.ID, b.ID) })
	d.Instances = instances
	return d
}

func selectedManagerHost(host *Host, executable string) *Host {
	clone := *host
	clone.Path = filepath.Dir(executable)
	return &clone
}

func instanceManagerHost(host *Host, instance Instance) *Host {
	if host == nil {
		return nil
	}
	for _, fact := range instance.Configuration {
		if fact.Label == "Manager executable" && fact.State == EvidenceKnown {
			return selectedManagerHost(host, fact.Value)
		}
	}
	return host
}
