package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ResourceSource retains allowlisted source intent, never whole configuration.
type ResourceSource struct {
	Kind, Identity, Path, RequestedRef, InstalledRevision, Manager, Scope string
	Pinned                                                                bool
	Evidence                                                              Fact
}

// DeclaredResources reads user and explicitly selected project Pi declarations.
func DeclaredResources(ctx context.Context, h *Host, s Scope) ([]ResourceSource, error) {
	paths, e := ResolvePiConfiguration(h, s)
	if e != nil {
		return nil, e
	}
	var sources []ResourceSource
	for _, p := range paths {
		if filepath.Base(p.Value) != "settings.json" {
			continue
		}
		raw, e := ReadBounded(ctx, p.Value, DefaultLimits().MaxFileBytes)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return sources, errors.New("resource declarations are unreadable")
		}
		var config struct{ Packages []json.RawMessage }
		if json.Unmarshal(raw, &config) != nil || len(config.Packages) > DefaultLimits().MaxFiles {
			return sources, errors.New("resource declaration format is unsupported")
		}
		label := "user"
		if s.ProjectDir != "" && samePath(filepath.Dir(p.Value), filepath.Join(s.ProjectDir, ".pi")) {
			label = s.ProjectDir
		}
		for _, entry := range config.Packages {
			if len(sources) >= DefaultLimits().MaxFiles {
				return sources, errors.New("resource declarations exceed limits")
			}
			var value string
			if json.Unmarshal(entry, &value) != nil {
				var object struct{ Source string }
				if json.Unmarshal(entry, &object) != nil {
					return sources, errors.New("resource declaration is invalid")
				}
				value = object.Source
			}
			sources = append(sources, parseResourceSource(value, filepath.Dir(p.Value), label))
		}
	}
	return sources, nil
}

func parseResourceSource(value, base, scope string) ResourceSource {
	r := ResourceSource{Kind: "unsupported", Identity: "unsupported declaration", Scope: scope, Evidence: Fact{State: EvidenceUnsupported, Label: "Requested source", Source: "declared resource"}}
	if !validText(value, DefaultLimits().MaxPathBytes) || value == "" {
		return r
	}
	switch {
	case strings.HasPrefix(value, "npm:"):
		name := strings.TrimPrefix(value, "npm:")
		ref := ""
		if n := strings.LastIndex(name, "@"); n > 0 {
			name, ref = name[:n], name[n+1:]
		}
		if !validPackageName(name) {
			return r
		}
		r.Kind = "npm"
		r.Identity = name
		r.Manager = "pi"
		r.RequestedRef = ref
		r.Pinned = ref != ""
		r.Path = filepath.Join(base, "npm", "node_modules", filepath.FromSlash(name))
	case strings.HasPrefix(value, "git:") || strings.HasPrefix(value, "https://github.com/"):
		name := strings.TrimPrefix(strings.TrimPrefix(value, "git:"), "https://")
		ref := ""
		if n := strings.LastIndex(name, "@"); n > 0 {
			name, ref = name[:n], name[n+1:]
		}
		name = strings.TrimSuffix(name, ".git")
		parts := strings.Split(name, "/")
		if len(parts) != 3 || parts[0] != "github.com" || !publicGitComponent(parts[1]) || !publicGitComponent(parts[2]) || !validText(ref, 256) {
			return r
		}
		r.Kind = "git"
		r.Identity = name
		r.RequestedRef = ref
		r.Pinned = ref != ""
		r.Manager = "pi"
		r.Path = filepath.Join(base, "git", filepath.FromSlash(name))
	case filepath.IsAbs(value) || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../"):
		r.Kind = "local"
		r.Path = value
		if !filepath.IsAbs(value) {
			r.Path = filepath.Join(base, value)
		}
		r.Identity = filepath.Clean(r.Path)
	}
	if r.Kind != "unsupported" {
		r.Evidence = Fact{State: EvidenceKnown, Label: "Requested source kind", Value: r.Kind, Source: "declared resource", Note: "Declaration does not prove installation or active loading."}
	}
	return r
}

func publicGitComponent(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > DefaultLimits().MaxIdentifierBytes {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func sourceInstance(h *Host, id string, r ResourceSource) Instance {
	location := r.Path
	if location == "" {
		location = filepath.Join(h.Home, "unsupported-resource")
	}
	if !filepath.IsAbs(location) || !validText(location, DefaultLimits().MaxPathBytes) {
		return Instance{}
	}
	if canonical, e := filepath.EvalSymlinks(location); e == nil {
		location = canonical
	}
	identity := fmt.Sprintf("%s:%x", id, sha256.Sum256([]byte(r.Scope+"\x00"+filepath.Clean(location)+"\x00"+r.Identity)))
	i := Instance{ID: identity, IntegrationID: id, Scope: r.Scope, Availability: AvailabilityPresent, DiscoverySource: "declared resource", ObservedAt: hostNow(h), Root: Fact{State: EvidenceKnown, Label: "Declared resource root", Value: r.Path, Source: "scope-relative declaration", ObservedAt: hostNow(h)}, Provenance: Provenance{State: EvidenceUnavailable}, Configuration: []Fact{{State: EvidenceKnown, Label: "Source kind", Value: r.Kind, Source: "declared resource"}, {State: EvidenceKnown, Label: "Source identity", Value: r.Identity, Source: "validated declaration"}, {State: EvidenceKnown, Label: "Requested ref", Value: r.RequestedRef, Source: "declared resource"}}}
	if r.Pinned {
		i.Configuration = append(i.Configuration, Fact{State: EvidenceKnown, Label: "Source constraint", Value: "pinned or ref constrained", Source: "declared resource"})
	}
	return i
}

func instanceSource(i Instance) ResourceSource {
	r := ResourceSource{Path: i.Root.Value, Scope: i.Scope}
	for _, f := range i.Configuration {
		switch f.Label {
		case "Source kind":
			r.Kind = f.Value
		case "Source identity":
			r.Identity = f.Value
		case "Requested ref":
			r.RequestedRef = f.Value
		case "Source constraint":
			r.Pinned = true
		}
	}
	return r
}

func resourceDiscovery(ctx context.Context, h *Host, s Scope, id string, superpowers bool) Discovery {
	d := Discovery{Availability: AvailabilityAbsent}
	if h == nil {
		return Discovery{Availability: AvailabilityUndetermined}
	}
	var sources []ResourceSource
	var e error
	roots, explicit := s.Locations[id]
	if explicit {
		if len(roots) == 0 || len(roots) > DefaultLimits().MaxFiles {
			return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Explicit resource locations are invalid."}}
		}
		sources = nil
		for _, p := range roots {
			if !filepath.IsAbs(p) {
				return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Explicit resource location is not absolute."}}
			}
			sources = append(sources, ResourceSource{Kind: "local", Identity: p, Path: p, Scope: "user"})
		}
	} else {
		sources, e = DeclaredResources(ctx, h, s)
		if e != nil {
			d.Availability = AvailabilityUndetermined
			d.Diagnostics = []string{"Resource declaration coverage is incomplete."}
		}
	}
	seen := map[string]bool{}
	for _, r := range sources {
		if superpowers && !explicit && !strings.HasSuffix(r.Identity, "/superpowers") && filepath.Base(r.Path) != "superpowers" {
			continue
		}
		i := sourceInstance(h, id, r)
		if i.ID == "" || seen[i.ID] {
			continue
		}
		seen[i.ID] = true
		d.Instances = append(d.Instances, i)
	}
	slices.SortFunc(d.Instances, func(a, b Instance) int { return strings.Compare(a.ID, b.ID) })
	if len(d.Instances) > 0 && e == nil {
		d.Availability = AvailabilityPresent
	}
	return d
}

func discoverPiPackages(c context.Context, h *Host, s Scope) Discovery {
	return resourceDiscovery(c, h, s, "pi-packages", false)
}

func discoverSuperpowers(c context.Context, h *Host, s Scope) Discovery {
	return resourceDiscovery(c, h, s, "superpowers", true)
}

func checkPiPackageSources(c context.Context, h *Host, _ Scope, i Instance) []Finding {
	return evaluateResource(c, h, i, "pi-packages")
}

func checkSuperpowersSource(c context.Context, h *Host, _ Scope, i Instance) []Finding {
	return evaluateResource(c, h, i, "superpowers")
}

func evaluateResource(ctx context.Context, h *Host, i Instance, check string) []Finding {
	f := Finding{Key: FindingKey{IntegrationID: i.IntegrationID, CheckID: check, InstanceID: i.ID}, Outcome: OutcomeUnknown, Question: "Does the declared resource have an established update?", Explanation: "Declared scope and requested source are retained. No safe automatic resource updater is established.", Evidence: []Fact{i.Root}}
	r := instanceSource(i)
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
		return []Finding{f}
	}
	if r.Kind == "git" {
		return []Finding{inspectGitResource(ctx, h, i, r, f)}
	}
	if r.Kind == "local" {
		f.Explanation = "Local resources are not manager update targets. Currentness is not established."
		return []Finding{f}
	}
	if r.Kind == "npm" {
		if r.Pinned {
			f.Outcome = OutcomeNotApplicable
			f.Explanation = "A requested npm version constraint is intentional; automatic advancement is disabled."
			return []Finding{f}
		}
		raw, e := ReadBounded(ctx, filepath.Join(r.Path, "package.json"), DefaultLimits().MaxFileBytes)
		if e != nil {
			return []Finding{f}
		}
		var metadata struct{ Name, Version string }
		if json.Unmarshal(raw, &metadata) != nil || metadata.Name != r.Identity || !validText(metadata.Version, 256) {
			return []Finding{f}
		}
		f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Installed package version", Value: metadata.Version, Source: "scope-local package metadata", ObservedAt: hostNow(h)})
		if h == nil || h.Fetch == nil {
			return []Finding{f}
		}
		raw, e = h.Fetch(ctx, "https://registry.npmjs.org/"+url.PathEscape(r.Identity)+"/latest")
		if e != nil || len(raw) > int(DefaultLimits().MaxHTTPBytes) {
			return []Finding{f}
		}
		var remote struct{ Version string }
		if json.Unmarshal(raw, &remote) != nil {
			return []Finding{f}
		}
		order, e := CompareVersions(metadata.Version, remote.Version, "semver")
		if e == nil {
			if order < 0 {
				f.Outcome = OutcomeAttention
				f.Explanation = fmt.Sprintf("A newer upstream release (%s) is available for package %s (installed: %s).", remote.Version, r.Identity, metadata.Version)
				f.References = append(f.References, PublicReference{Kind: "package", Label: "npm package", URL: "https://www.npmjs.com/package/" + r.Identity})
				f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Package identity", Value: r.Identity, Source: "declared resource", ObservedAt: hostNow(h)})
				if piExe, ok := findExecutable(h, "pi"); ok {
					f.Actions = []ActionProposal{
						{
							ID:                  "update-pi-pkg-" + sanitizeActionID(r.Identity),
							Key:                 f.Key,
							Mode:                ActionAutomatic,
							Label:               fmt.Sprintf("Update %s to %s", r.Identity, remote.Version),
							Reason:              fmt.Sprintf("Package %s is outdated (%s -> %s)", r.Identity, metadata.Version, remote.Version),
							TargetIDs:           []string{i.ID},
							TargetVersion:       Fact{State: EvidenceKnown, Label: "Target version", Value: remote.Version},
							Preconditions:       []Fact{i.Root, {State: EvidenceKnown, Label: "Installed package version", Value: metadata.Version}},
							VerificationCheckID: "pi-packages",
							Steps: []CommandStep{
								{
									Label: fmt.Sprintf("Run pi install npm:%s", r.Identity),
									Command: Command{
										Executable: piExe,
										Args:       []string{"install", "npm:" + r.Identity},
									},
								},
							},
							SideEffects: []string{
								"Installs the updated extension package into Pi settings and node_modules.",
							},
						},
					}
				}
			} else {
				f.Outcome = OutcomeOK
				f.Explanation = fmt.Sprintf("Installed package %s (%s) matches or exceeds upstream release (%s).", r.Identity, metadata.Version, remote.Version)
			}
			f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Upstream package release", Value: remote.Version, Source: "public npm registry", Note: "Not a proven manager update decision.", ObservedAt: hostNow(h)})
		}
	}
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
	}
	return []Finding{f}
}

func sanitizeActionID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == '@' || r == '/' || r == '.' {
			b.WriteRune('-')
		}
	}
	res := strings.Trim(b.String(), "-")
	if len(res) > 48 {
		res = res[:48]
	}
	if res == "" {
		return "item"
	}
	return res
}
