package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ExecutableCandidates resolves explicit locations before documented roots and PATH.
func ExecutableCandidates(ctx context.Context, host *Host, scope Scope, integrationID, name string, defaults []string) Discovery {
	discovery := Discovery{Availability: AvailabilityAbsent}
	if err := ctx.Err(); err != nil {
		discovery.Availability = AvailabilityUndetermined
		discovery.Diagnostics = []string{"Discovery was canceled."}
		return discovery
	}
	if host == nil || !validIdentifier(integrationID, DefaultLimits().MaxIdentifierBytes) || !validIdentifier(name, DefaultLimits().MaxIdentifierBytes) {
		discovery.Availability = AvailabilityUndetermined
		discovery.Diagnostics = []string{"Executable discovery input is invalid."}
		return discovery
	}
	var candidates []string
	if paths, explicit := scope.Locations[integrationID]; explicit {
		if len(paths) == 0 {
			discovery.Availability = AvailabilityUndetermined
			discovery.Diagnostics = []string{"Explicit executable locations are empty."}
			return discovery
		}
		for _, path := range paths {
			if !filepath.IsAbs(path) {
				discovery.Availability = AvailabilityUndetermined
				discovery.Diagnostics = []string{"Explicit executable location is not absolute."}
				return discovery
			}
			candidates = append(candidates, path)
		}
	} else {
		for _, root := range defaults {
			if filepath.IsAbs(root) {
				candidates = append(candidates, filepath.Join(root, name))
				continue
			}
			if root == name {
				candidates = append(candidates, root)
				continue
			}
			candidates = append(candidates, filepath.Join(root, name))
		}
		if found, ok := findExecutable(host, name); ok {
			candidates = append(candidates, found)
		}
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			discovery.Availability = AvailabilityUndetermined
			discovery.Diagnostics = append(discovery.Diagnostics, "Discovery was canceled.")
			break
		}
		path, err := filepath.Abs(candidate)
		if err != nil {
			discovery.Availability = AvailabilityUndetermined
			discovery.Diagnostics = append(discovery.Diagnostics, "Executable path could not be resolved.")
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			if _, explicit := scope.Locations[integrationID]; explicit {
				discovery.Availability = AvailabilityUndetermined
				discovery.Diagnostics = append(discovery.Diagnostics, "An explicit executable location is unavailable.")
				continue
			}
			if !errors.Is(err, os.ErrNotExist) {
				discovery.Availability = AvailabilityUndetermined
				discovery.Diagnostics = append(discovery.Diagnostics, "An executable candidate could not be inspected.")
			}
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			discovery.Availability = AvailabilityUndetermined
			discovery.Diagnostics = append(discovery.Diagnostics, "An executable candidate is not a runnable file.")
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			discovery.Availability = AvailabilityUndetermined
			discovery.Diagnostics = append(discovery.Diagnostics, "An executable candidate could not be resolved.")
			continue
		}
		canonical := filepath.Clean(resolved)
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		id, err := CanonicalInstanceID(integrationID, "user", path)
		if err != nil {
			discovery.Availability = AvailabilityUndetermined
			discovery.Diagnostics = append(discovery.Diagnostics, "Executable identity could not be established.")
			continue
		}
		active := samePath(path, findExecutablePath(host, name))
		discovery.Instances = append(discovery.Instances, Instance{ID: id, IntegrationID: integrationID, Scope: "user", DiscoverySource: "explicit or documented executable candidate", ObservedAt: hostNow(host), Availability: AvailabilityPresent, Active: active, Executable: Fact{State: EvidenceKnown, Label: "Executable", Value: path, Source: "filesystem candidate"}, ResolvedPath: Fact{State: EvidenceKnown, Label: "Resolved executable", Value: canonical, Source: "filesystem symlink resolution"}, Root: Fact{State: EvidenceKnown, Label: "Installation root", Value: filepath.Dir(filepath.Dir(canonical)), Source: "candidate path"}})
	}
	slices.SortFunc(discovery.Instances, func(a, b Instance) int { return strings.Compare(a.Executable.Value, b.Executable.Value) })
	if len(discovery.Instances) > 0 {
		discovery.Availability = AvailabilityPresent
	} else if len(discovery.Diagnostics) > 0 {
		discovery.Availability = AvailabilityUndetermined
	}
	return discovery
}

func lookPath(pathList, name string) (string, error) {
	if !filepath.IsAbs(name) && filepath.Base(name) != name {
		return "", errors.New("executable name must be a base name")
	}
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}

func findExecutable(host *Host, name string) (string, bool) {
	if host == nil {
		return "", false
	}
	if path, err := lookPath(host.Path, name); err == nil {
		if !filepath.IsAbs(path) {
			for _, dir := range filepath.SplitList(host.Path) {
				if dir == "" {
					continue
				}
				candidate := filepath.Join(dir, name)
				if samePath(candidate, path) {
					return filepath.Clean(candidate), true
				}
			}
			return "", false
		}
		return filepath.Clean(path), true
	}
	for _, dir := range filepath.SplitList(host.Path) {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return filepath.Clean(path), true
		}
	}
	return "", false
}

func findExecutablePath(host *Host, name string) string {
	path, _ := findExecutable(host, name)
	return path
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, e1 := filepath.Abs(a)
	bb, e2 := filepath.Abs(b)
	return e1 == nil && e2 == nil && filepath.Clean(aa) == filepath.Clean(bb)
}

func hostNow(host *Host) time.Time {
	if host != nil && host.Now != nil {
		return host.Now()
	}
	return time.Now()
}
