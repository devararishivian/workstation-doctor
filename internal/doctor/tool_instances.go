package doctor

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

// discoverNpmTool keeps executable presence separate from global package evidence.
func discoverNpmTool(ctx context.Context, host *Host, scope Scope, id, pkg string, defaults []string) Discovery {
	d := ExecutableCandidates(ctx, host, scope, id, id, defaults)
	if host == nil {
		return d
	}
	inventory, err := NpmInventory(ctx, host, scope)
	for n := range d.Instances {
		i := &d.Instances[n]
		i.Provenance = Provenance{State: EvidenceUnavailable}
		i.Version = Fact{State: EvidenceUnavailable, Label: "Installed version", Source: "local package metadata", Note: "No supported local version evidence was found.", ObservedAt: hostNow(host)}
		// Parent directories alone do not establish an installation root.
		i.Root = Fact{State: EvidenceUnavailable, Label: "Installation root", Source: "executable discovery", ObservedAt: hostNow(host)}
		if err != nil {
			i.Diagnostics = append(i.Diagnostics, "Global package inventory is unavailable or incomplete.")
			continue
		}
		for _, item := range inventory.Items {
			if item.Package != pkg {
				continue
			}
			for _, exe := range item.Executables {
				resolved, e := filepath.EvalSymlinks(exe)
				if e != nil || !samePath(resolved, i.ResolvedPath.Value) {
					continue
				}
				i.Root = Fact{State: EvidenceKnown, Label: "Installation root", Value: item.Root, Source: "global package metadata", ObservedAt: inventory.ObservedAt}
				// Match the inventory alias rather than confusing a second alias with another installation.
				evidenceInstance := *i
				evidenceInstance.Executable.Value = exe
				i.Provenance = MatchProvenance(evidenceInstance, inventory)
				i.Version = Fact{State: EvidenceKnown, Label: "Installed version", Value: item.InstalledVersion, Source: "global package.json", ObservedAt: inventory.ObservedAt}
			}
		}
	}
	return d
}

func checkNpmTool(ctx context.Context, host *Host, scope Scope, instance Instance, id, pkg, reference string) []Finding {
	key := FindingKey{IntegrationID: id, CheckID: id, InstanceID: instance.ID}
	f := Finding{Key: key, Outcome: OutcomeUnknown, Question: "Is a supported update available?", Explanation: "Automatic maintenance requires established manager ownership and supported version evidence.", Evidence: []Fact{instance.Version}, References: []PublicReference{{Kind: "documentation", Label: "Official project", URL: reference}}}
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
		return []Finding{f}
	}
	if instance.Version.State != EvidenceKnown || host == nil || host.Fetch == nil {
		return []Finding{f}
	}
	raw, err := host.Fetch(ctx, "https://registry.npmjs.org/"+url.PathEscape(pkg)+"/latest")
	if err != nil || len(raw) > int(DefaultLimits().MaxHTTPBytes) {
		f.Explanation = "Upstream lookup is unavailable. Installed version evidence is retained."
		if ctx.Err() != nil {
			f.Outcome = OutcomeCanceled
		}
		return []Finding{f}
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &metadata) != nil {
		return []Finding{f}
	}
	order, err := CompareVersions(instance.Version.Value, metadata.Version, "semver")
	if err != nil {
		return []Finding{f}
	}
	candidate := Fact{State: EvidenceKnown, Label: "Upstream latest", Value: metadata.Version, Source: "official npm registry latest tag", Note: "Upstream release, not a Homebrew or other manager decision.", ObservedAt: hostNow(host)}
	f.Evidence = append(f.Evidence, candidate)
	if order >= 0 {
		f.Outcome = OutcomeOK
		f.Explanation = "The installed version is equal to or newer than the observed upstream release."
		return []Finding{f}
	}
	f.Outcome = OutcomeAttention
	f.Explanation = "A newer upstream release exists. The update method depends on installation ownership."
	p := instance.Provenance
	if p.State != EvidenceKnown || p.Manager != "npm" || p.Package != pkg || !filepath.IsAbs(p.Root) || !validPackageName(pkg) || semver.Prerelease(canonicalSemver(instance.Version.Value)) != "" || semver.Prerelease(canonicalSemver(metadata.Version)) != "" {
		return []Finding{f}
	}
	manager, ok := findExecutable(host, "npm")
	if !ok {
		return []Finding{f}
	}
	inventory, err := NpmInventory(ctx, host, scope)
	if err != nil || !samePath(inventory.Executable, manager) {
		return []Finding{f}
	}
	matched := false
	for _, item := range inventory.Items {
		if item.Package == pkg && samePath(item.Root, p.Root) && item.RequestedPin == "" {
			for _, exe := range item.Executables {
				resolved, e := filepath.EvalSymlinks(exe)
				if e == nil && samePath(resolved, instance.ResolvedPath.Value) {
					matched = true
				}
			}
		}
	}
	if !matched {
		return []Finding{f}
	}
	prefix := filepath.Dir(filepath.Dir(inventory.Root))
	target, err := CanonicalInstanceID("npm-package", "user", p.Root)
	if err != nil {
		return []Finding{f}
	}
	f.Actions = []ActionProposal{{ID: "update-package", Key: key, Mode: ActionAutomatic, Label: "Update selected global package", Reason: f.Explanation, TargetIDs: []string{target}, TargetVersion: candidate, Preconditions: []Fact{instance.Executable, instance.ResolvedPath, instance.Version, instance.Root, {State: EvidenceKnown, Label: "Manager executable", Value: manager, Source: "global inventory"}}, VerificationCheckID: id, Steps: []CommandStep{{Label: "Install the selected package version in its proven prefix", Command: Command{Executable: manager, Args: []string{"install", "--global", "--prefix", prefix, pkg + "@" + strings.TrimPrefix(metadata.Version, "v")}}}}, SideEffects: []string{"Downloads packages and dependencies into the selected global prefix.", "Runs package lifecycle scripts and changes executable links.", "Updates npm cache and can require manual permission repair; no elevation is requested."}}}
	return []Finding{f}
}
