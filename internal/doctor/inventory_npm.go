package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

// readNpmPackages reads only the selected global root and one scoped-name level.
// It does not evaluate npm configuration or execute package code.
func readNpmPackages(ctx context.Context, inventory Inventory) (Inventory, error) {
	entries, err := inventoryEntries(ctx, inventory.Root)
	if err != nil {
		return inventory, err
	}
	count := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return inventory, fmt.Errorf("inspect global packages: %w", err)
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		names := []string{name}
		if strings.HasPrefix(name, "@") {
			children, e := inventoryEntries(ctx, filepath.Join(inventory.Root, name))
			if e != nil {
				return inventory, e
			}
			names = nil
			for _, child := range children {
				names = append(names, name+"/"+child.Name())
			}
		}
		for _, pkg := range names {
			count++
			if count > DefaultLimits().MaxFiles {
				return inventory, errors.New("global inventory exceeds package limit")
			}
			item, e := readNpmPackage(ctx, inventory, pkg)
			if e != nil {
				return inventory, e
			}
			if item != nil {
				inventory.Items = append(inventory.Items, *item)
			}
		}
	}
	return inventory, nil
}

func inventoryEntries(ctx context.Context, path string) (entries []os.DirEntry, err error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("inspect inventory directory: %w", err)
	}
	dir, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open inventory directory: %w", err)
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	entries, err = dir.ReadDir(DefaultLimits().MaxFiles + 1)
	if len(entries) > DefaultLimits().MaxFiles {
		return nil, errors.New("inventory directory exceeds entry limit")
	}
	// ReadDir returns EOF only when the directory is empty or shorter than the cap.
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("read inventory directory: %w", err)
	}
	return entries, nil
}

func readNpmPackage(ctx context.Context, inventory Inventory, pkg string) (*InventoryItem, error) {
	if !validPackageName(pkg) {
		return nil, errors.New("global package identity is unsupported")
	}
	root := filepath.Join(inventory.Root, filepath.FromSlash(pkg))
	canonical, e := filepath.EvalSymlinks(root)
	if e != nil {
		return nil, fmt.Errorf("resolve global package: %w", e)
	}
	inventoryRoot, e := filepath.EvalSymlinks(inventory.Root)
	if e != nil {
		return nil, fmt.Errorf("resolve global root: %w", e)
	}
	rel, e := filepath.Rel(inventoryRoot, canonical)
	if e != nil || !filepath.IsLocal(rel) {
		return nil, errors.New("linked package outside global root is unsupported")
	}
	raw, e := ReadBounded(ctx, filepath.Join(root, "package.json"), DefaultLimits().MaxFileBytes)
	if e != nil {
		return nil, e
	}
	var metadata struct {
		Name, Version string
		Bin           json.RawMessage
	}
	if json.Unmarshal(raw, &metadata) != nil || metadata.Name != pkg || !semver.IsValid(canonicalSemver(metadata.Version)) {
		return nil, errors.New("global package metadata is unsupported")
	}
	item := &InventoryItem{Package: pkg, Root: canonical, InstalledVersion: metadata.Version, Channel: "npm global", UpdateDecision: Fact{State: EvidenceUnavailable, Label: "Manager update availability", Source: "local package metadata", Note: "Registry availability was not queried."}}
	bins := map[string]string{}
	if len(metadata.Bin) > 0 && string(metadata.Bin) != "null" {
		if json.Unmarshal(metadata.Bin, &bins) != nil {
			var bin string
			if json.Unmarshal(metadata.Bin, &bin) != nil {
				return nil, errors.New("package bin metadata is unsupported")
			}
			bins[filepath.Base(pkg)] = bin
		}
	}
	if len(bins) > DefaultLimits().MaxFiles {
		return nil, errors.New("package binaries exceed limit")
	}
	for name, target := range bins {
		if !validIdentifier(name, DefaultLimits().MaxIdentifierBytes) || filepath.Base(name) != name || !filepath.IsLocal(target) {
			return nil, errors.New("package binary location is invalid")
		}
		declared := filepath.Join(root, target)
		resolved, e := filepath.EvalSymlinks(declared)
		if e != nil {
			continue
		}
		rel, e := filepath.Rel(canonical, resolved)
		if e != nil || !filepath.IsLocal(rel) {
			continue
		}
		executable := filepath.Join(filepath.Dir(filepath.Dir(inventory.Root)), "bin", name)
		active, e := filepath.EvalSymlinks(executable)
		if e == nil && samePath(active, resolved) {
			item.Executables = append(item.Executables, executable)
		}
	}
	return item, nil
}

func validPackageName(name string) bool {
	if len(name) == 0 || len(name) > DefaultLimits().MaxIdentifierBytes {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) > 2 || len(parts) == 2 && !strings.HasPrefix(parts[0], "@") {
		return false
	}
	for _, part := range parts {
		part = strings.TrimPrefix(part, "@")
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.' {
				return false
			}
		}
	}
	return true
}
