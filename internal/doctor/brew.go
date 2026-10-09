package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

type brewOutdatedSnapshot struct {
	available bool
	err       error
	formulae  map[string]string
	casks     map[string]string
}

func queryBrewOutdated(ctx context.Context, host *Host) (*brewOutdatedSnapshot, error) {
	if host == nil || host.RunRead == nil {
		return nil, errors.New("host inspection is unavailable")
	}
	if host.inventory != nil {
		host.inventory.mu.Lock()
		if snap, ok := host.inventory.brewOutdated.(*brewOutdatedSnapshot); ok && snap != nil {
			host.inventory.mu.Unlock()
			return snap, snap.err
		}
		host.inventory.mu.Unlock()
	}

	snap := &brewOutdatedSnapshot{
		formulae: map[string]string{},
		casks:    map[string]string{},
	}

	brew, ok := findExecutable(host, "brew")
	if !ok {
		snap.err = errors.New("brew executable is unavailable")
		storeBrewSnapshot(host, snap)
		return snap, snap.err
	}

	res, err := host.RunRead(ctx, Command{
		Executable: brew,
		Args:       []string{"outdated", "--json=v2"},
		Env:        map[string]string{"HOMEBREW_NO_AUTO_UPDATE": "1"},
	})
	if err != nil || res.ExitCode != 0 || res.Truncated {
		if err == nil {
			err = fmt.Errorf("brew outdated exited with code %d", res.ExitCode)
		}
		snap.err = err
		storeBrewSnapshot(host, snap)
		return snap, snap.err
	}

	var data struct {
		Formulae []struct {
			Name           string `json:"name"`
			CurrentVersion string `json:"current_version"`
		} `json:"formulae"`
		Casks []struct {
			Name           string `json:"name"`
			CurrentVersion string `json:"current_version"`
		} `json:"casks"`
	}
	if err := json.Unmarshal(res.Stdout, &data); err != nil {
		snap.err = fmt.Errorf("parse brew outdated output: %w", err)
		storeBrewSnapshot(host, snap)
		return snap, snap.err
	}

	for _, f := range data.Formulae {
		if f.Name != "" {
			snap.formulae[f.Name] = f.CurrentVersion
		}
	}
	for _, c := range data.Casks {
		if c.Name != "" {
			snap.casks[c.Name] = c.CurrentVersion
		}
	}
	snap.available = true
	storeBrewSnapshot(host, snap)
	return snap, nil
}

func storeBrewSnapshot(host *Host, snap *brewOutdatedSnapshot) {
	if host != nil && host.inventory != nil {
		host.inventory.mu.Lock()
		host.inventory.brewOutdated = snap
		host.inventory.mu.Unlock()
	}
}

func discoverBrew(ctx context.Context, host *Host, scope Scope) Discovery {
	return discoverManagerPackages(ctx, host, scope, "brew", BrewInventory)
}

func checkBrewInventory(ctx context.Context, host *Host, scope Scope, instance Instance) []Finding {
	selected := instanceManagerHost(host, instance)
	key := FindingKey{IntegrationID: "brew", CheckID: "brew-outdated", InstanceID: instance.ID}
	f := Finding{
		Key:         key,
		Outcome:     OutcomeUnknown,
		Question:    "Does the selected manager establish an update?",
		Explanation: "Local receipts do not establish update availability. Homebrew repository metadata was not refreshed. Cask inspection is unsupported.",
		Evidence:    []Fact{instance.Version},
		References:  []PublicReference{{Kind: "documentation", Label: "Official Homebrew documentation", URL: "https://docs.brew.sh/Manpage"}},
	}
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
		return []Finding{f}
	}

	// 1. Try querying brew outdated --json=v2 (cached across audit)
	if snap, err := queryBrewOutdated(ctx, selected); err == nil && snap != nil && snap.available {
		pkg := instance.Provenance.Package
		isCask := strings.HasPrefix(pkg, "homebrew/cask/")
		name := filepath.Base(pkg)
		if isCask {
			name = strings.TrimPrefix(pkg, "homebrew/cask/")
		}

		brewExe, _ := findExecutable(selected, "brew")

		if isCask {
			if latest, outdated := snap.casks[name]; outdated {
				f.Outcome = OutcomeAttention
				f.Explanation = fmt.Sprintf("Homebrew reports that cask %s is outdated (latest: %s).", name, latest)
				f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Latest cask version", Value: latest, Source: "brew outdated --json=v2", ObservedAt: hostNow(host)})
				if brewExe != "" {
					f.Actions = []ActionProposal{{
						ID:     "brew-upgrade-cask",
						Key:    key,
						Mode:   ActionManual,
						Label:  "Upgrade Homebrew cask",
						Reason: "Cask is outdated in Homebrew.",
						Steps:  []CommandStep{{Label: "Run brew upgrade --cask", Command: Command{Executable: brewExe, Args: []string{"upgrade", "--cask", name}}}},
					}}
				}
			} else {
				f.Outcome = OutcomeOK
				f.Explanation = "The installed cask is current according to local Homebrew metadata (auto-update disabled)."
				f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Homebrew outdated check", Value: "current", Source: "brew outdated --json=v2", Note: "HOMEBREW_NO_AUTO_UPDATE=1", ObservedAt: hostNow(host)})
			}
			return []Finding{f}
		}

		// Formula
		if latest, outdated := snap.formulae[name]; outdated {
			f.Outcome = OutcomeAttention
			f.Explanation = fmt.Sprintf("Homebrew reports that formula %s is outdated (latest: %s).", name, latest)
			f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Latest formula version", Value: latest, Source: "brew outdated --json=v2", ObservedAt: hostNow(host)})
			if brewExe != "" {
				f.Actions = []ActionProposal{{
					ID:     "brew-upgrade-formula",
					Key:    key,
					Mode:   ActionManual,
					Label:  "Upgrade Homebrew formula",
					Reason: "Formula is outdated in Homebrew.",
					Steps:  []CommandStep{{Label: "Run brew upgrade", Command: Command{Executable: brewExe, Args: []string{"upgrade", name}}}},
				}}
			}
		} else {
			f.Outcome = OutcomeOK
			f.Explanation = "The installed formula is current according to local Homebrew metadata (auto-update disabled)."
			f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Homebrew outdated check", Value: "current", Source: "brew outdated --json=v2", Note: "HOMEBREW_NO_AUTO_UPDATE=1", ObservedAt: hostNow(host)})
		}
		return []Finding{f}
	}

	// 2. Fallback if brew CLI is not available (e.g. synthetic test host without brew CLI)
	inventory, err := BrewInventory(ctx, selected, scope)
	f.Evidence = append(f.Evidence, Fact{State: EvidenceUnavailable, Label: "Manager update availability", Source: "local Homebrew receipt", Note: inventory.FreshnessNote, ObservedAt: inventory.ObservedAt})
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
