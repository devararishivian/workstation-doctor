package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func readBrewReceipts(ctx context.Context, inventory Inventory) (Inventory, error) {
	cellar := filepath.Join(inventory.Root, "Cellar")
	packages, err := inventoryEntries(ctx, cellar)
	if errors.Is(err, os.ErrNotExist) {
		return inventory, nil
	}
	if err != nil {
		return inventory, err
	}
	count := 0
	for _, pkg := range packages {
		if strings.HasPrefix(pkg.Name(), ".") || !pkg.IsDir() {
			continue
		}
		if !validFormulaName(pkg.Name()) {
			continue
		}
		versions, e := inventoryEntries(ctx, filepath.Join(cellar, pkg.Name()))
		if e != nil {
			continue
		}
		for _, version := range versions {
			if strings.HasPrefix(version.Name(), ".") || !version.IsDir() {
				continue
			}
			count++
			if count > DefaultLimits().MaxFiles {
				return inventory, errors.New("homebrew receipts exceed entry limit")
			}
			root := filepath.Join(cellar, pkg.Name(), version.Name())
			raw, e := ReadBounded(ctx, filepath.Join(root, "INSTALL_RECEIPT.json"), DefaultLimits().MaxFileBytes)
			if e != nil {
				continue
			}
			var receipt struct{ Source struct{ Tap, Spec string } }
			if json.Unmarshal(raw, &receipt) != nil || !validText(receipt.Source.Tap, DefaultLimits().MaxIdentifierBytes) || receipt.Source.Tap == "" || receipt.Source.Spec != "stable" {
				continue
			}
			canonical, e := filepath.EvalSymlinks(root)
			if e != nil {
				continue
			}
			cellarCanonical, e := filepath.EvalSymlinks(cellar)
			if e != nil {
				continue
			}
			rel, e := filepath.Rel(cellarCanonical, canonical)
			if e != nil || !filepath.IsLocal(rel) {
				continue
			}
			item := InventoryItem{Package: receipt.Source.Tap + "/" + pkg.Name(), Root: canonical, InstalledVersion: version.Name(), Channel: "Homebrew stable formula", InstalledAt: parseReceiptInstallationTime(raw, inventory.ObservedAt), UpdateDecision: Fact{State: EvidenceUnavailable, Label: "Manager update availability", Source: "local Homebrew receipt", Note: "Formula repository metadata was not refreshed."}}
			bins, e := inventoryEntries(ctx, filepath.Join(root, "bin"))
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				return inventory, e
			}
			for _, bin := range bins {
				alias := filepath.Join(inventory.Root, "bin", bin.Name())
				resolved, e := filepath.EvalSymlinks(alias)
				if e != nil {
					continue
				}
				target, e := filepath.EvalSymlinks(filepath.Join(root, "bin", bin.Name()))
				if e == nil && samePath(target, resolved) {
					item.Executables = append(item.Executables, alias)
				}
			}
			inventory.Items = append(inventory.Items, item)
		}
	}

	caskroom := filepath.Join(inventory.Root, "Caskroom")
	casks, err := inventoryEntries(ctx, caskroom)
	if err == nil {
		for _, cask := range casks {
			if strings.HasPrefix(cask.Name(), ".") || !cask.IsDir() {
				continue
			}
			versions, e := inventoryEntries(ctx, filepath.Join(caskroom, cask.Name()))
			if e != nil {
				continue
			}
			for _, version := range versions {
				if strings.HasPrefix(version.Name(), ".") || !version.IsDir() {
					continue
				}
				caskRoot := filepath.Join(caskroom, cask.Name(), version.Name())
				canonical, e := filepath.EvalSymlinks(caskRoot)
				if e != nil {
					continue
				}
				item := InventoryItem{
					Package:          "homebrew/cask/" + cask.Name(),
					Root:             canonical,
					InstalledVersion: version.Name(),
					Channel:          "Homebrew cask",
					UpdateDecision:   Fact{State: EvidenceUnavailable, Label: "Manager update availability", Source: "local Homebrew cask", Note: "Cask repository metadata was not refreshed."},
				}
				alias := filepath.Join(inventory.Root, "bin", cask.Name())
				resolved, e := filepath.EvalSymlinks(alias)
				if e == nil {
					target, e := filepath.EvalSymlinks(filepath.Join(caskRoot, cask.Name()))
					if e == nil && samePath(target, resolved) {
						item.Executables = append(item.Executables, alias)
					}
				}
				inventory.Items = append(inventory.Items, item)
			}
		}
	}

	return inventory, nil
}

// validFormulaName is a conservative basename allowlist, separate from npm names.
// Homebrew formula names can include version suffixes and plus signs.
func validFormulaName(name string) bool {
	if len(name) == 0 || len(name) > DefaultLimits().MaxIdentifierBytes || name == "." || name == ".." {
		return false
	}
	for n, r := range name {
		alphanumeric := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if !alphanumeric && (n == 0 || r != '-' && r != '_' && r != '.' && r != '+' && r != '@') {
			return false
		}
	}
	return true
}

// attachBrewEvidence accepts only an exact linked binary in a receipt-owned keg.
func attachBrewEvidence(ctx context.Context, host *Host, scope Scope, d Discovery, packageName string) Discovery {
	if host == nil {
		return d
	}
	inventory, err := BrewInventory(ctx, host, scope)
	if err != nil {
		return d
	}
	for n := range d.Instances {
		i := &d.Instances[n]
		if i.Provenance.State == EvidenceKnown {
			continue
		}
		for _, item := range inventory.Items {
			if filepath.Base(item.Package) != packageName {
				continue
			}
			for _, alias := range item.Executables {
				target, e := filepath.EvalSymlinks(alias)
				if e != nil || !samePath(target, i.ResolvedPath.Value) {
					continue
				}
				i.Root = Fact{State: EvidenceKnown, Label: "Installation root", Value: item.Root, Source: "Homebrew keg receipt", ObservedAt: inventory.ObservedAt}
				evidence := *i
				evidence.Executable.Value = alias
				i.Provenance = MatchProvenance(evidence, inventory)
				i.Version = Fact{State: EvidenceKnown, Label: "Installed version", Value: item.InstalledVersion, Source: "Homebrew keg revision", ObservedAt: inventory.ObservedAt}
				i.InstalledAt = item.InstalledAt
			}
		}
	}
	return d
}
