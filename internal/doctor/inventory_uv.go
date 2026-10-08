package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// readUvReceipts supports the receipt/entrypoint subset verified in uv 0.9.7.
// It never asks Python or uv to materialize or inspect an environment.
func readUvReceipts(ctx context.Context, inventory Inventory) (Inventory, error) {
	entries, err := inventoryEntries(ctx, inventory.Root)
	if errors.Is(err, os.ErrNotExist) {
		return inventory, nil
	}
	if err != nil {
		return inventory, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !validPackageName(entry.Name()) || strings.Contains(entry.Name(), "/") {
			return inventory, errors.New("uv tool identity is unsupported")
		}
		root := filepath.Join(inventory.Root, entry.Name())
		raw, e := ReadBounded(ctx, filepath.Join(root, "uv-receipt.toml"), DefaultLimits().MaxFileBytes)
		if e != nil {
			return inventory, e
		}
		var receipt struct {
			Tool struct {
				Entrypoints []struct {
					Name        string
					InstallPath string `toml:"install-path"`
					From        string
				}
				Requirements []map[string]any
				Constraints  []map[string]any
				Overrides    []map[string]any
			}
		}
		if toml.Unmarshal(raw, &receipt) != nil || len(receipt.Tool.Entrypoints) > DefaultLimits().MaxFiles {
			return inventory, errors.New("uv receipt format is unsupported")
		}
		canonical, e := filepath.EvalSymlinks(root)
		if e != nil {
			return inventory, fmt.Errorf("resolve uv tool root: %w", e)
		}
		canonicalRoot, e := filepath.EvalSymlinks(inventory.Root)
		if e != nil {
			return inventory, fmt.Errorf("resolve uv inventory root: %w", e)
		}
		rel, e := filepath.Rel(canonicalRoot, canonical)
		if e != nil || !filepath.IsLocal(rel) {
			return inventory, errors.New("uv tool root escapes selected inventory")
		}
		item := InventoryItem{Package: entry.Name(), Root: canonical, Channel: "uv tool; update source not established", UpdateDecision: Fact{State: EvidenceUnavailable, Label: "Manager update availability", Source: "uv receipt", Note: "Receipt ownership does not establish a safe update source."}}
		for _, point := range receipt.Tool.Entrypoints {
			if point.From != entry.Name() || point.Name == "" || filepath.Base(point.Name) != point.Name || !filepath.IsAbs(point.InstallPath) {
				continue
			}
			resolved, e := filepath.EvalSymlinks(point.InstallPath)
			if e != nil {
				continue
			}
			expected, e := filepath.EvalSymlinks(filepath.Join(root, "bin", point.Name))
			if e == nil && samePath(expected, resolved) {
				item.Executables = append(item.Executables, point.InstallPath)
			}
		}
		if len(receipt.Tool.Constraints) > 0 || len(receipt.Tool.Overrides) > 0 {
			item.RequestedPin = "constraints or overrides declared"
		}
		for _, req := range receipt.Tool.Requirements {
			if len(req) > 1 {
				item.RequestedPin = "requested source or version constraint declared"
			}
		}
		version, e := uvMetadataVersion(ctx, root, entry.Name())
		if e != nil {
			return inventory, e
		}
		item.InstalledVersion = version
		inventory.Items = append(inventory.Items, item)
	}
	return inventory, nil
}

func uvMetadataVersion(ctx context.Context, root, pkg string) (string, error) {
	version := ""
	count := 0
	err := filepath.WalkDir(filepath.Join(root, "lib"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("inspect uv metadata location: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("inspect uv metadata: %w", err)
		}
		count++
		if count > DefaultLimits().MaxFiles {
			return errors.New("uv metadata exceeds entry limit")
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return fmt.Errorf("resolve uv metadata scope: %w", e)
		}
		if len(strings.Split(rel, string(filepath.Separator))) > DefaultLimits().MaxDepth {
			return errors.New("uv metadata exceeds depth limit")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || entry.Name() != "METADATA" || !strings.HasSuffix(filepath.Dir(path), ".dist-info") {
			return nil
		}
		raw, e := ReadBounded(ctx, path, DefaultLimits().MaxFileBytes)
		if e != nil {
			return e
		}
		name := ""
		found := ""
		for line := range strings.SplitSeq(string(raw), "\n") {
			if line == "" {
				break
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			if key == "Name" {
				name = strings.TrimSpace(value)
			}
			if key == "Version" {
				found = strings.TrimSpace(value)
			}
		}
		if strings.ReplaceAll(strings.ToLower(name), "_", "-") == pkg {
			if version != "" || !validText(found, DefaultLimits().MaxIdentifierBytes) || found == "" {
				return errors.New("uv package version evidence is ambiguous")
			}
			version = found
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("inspect uv installed version: %w", err)
	}
	return version, nil
}
