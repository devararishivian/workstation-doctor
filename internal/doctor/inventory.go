package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"
)

// Inventory is a single manager/prefix observation in one selected scope.
type Inventory struct {
	Manager, Executable, Root, FreshnessNote string
	ObservedAt                               time.Time
	Items                                    []InventoryItem
}

// InventoryItem is a bounded package observation from a manager-owned root.
type InventoryItem struct {
	Package, Root, InstalledVersion, AvailableVersion, Channel, RequestedPin string
	Executables                                                              []string
	UpdateDecision                                                           Fact
	InstalledAt                                                              *InstallationEvidence
}

type (
	inventoryKey   struct{ manager, executable, root, project string }
	inventoryEntry struct{ value Inventory }
)

func scopeKey(scope Scope) string {
	if scope.ProjectDir == "" {
		return "user"
	}
	return filepath.Clean(scope.ProjectDir)
}

func defaultScope(_ *auditInventory) Scope { return Scope{} }

func packageRoot(prefix string) string { return filepath.Join(prefix, "lib", "node_modules") }

func cacheInventory(cache *auditInventory, scope Scope, inventory Inventory) error {
	if cache == nil {
		return errors.New("inventory cache is unavailable")
	}
	key := inventoryKey{manager: inventory.Manager, executable: filepath.Clean(inventory.Executable), root: filepath.Clean(inventory.Root), project: scopeKey(scope)}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.inventories == nil {
		cache.inventories = map[inventoryKey]inventoryEntry{}
	}
	cache.inventories[key] = inventoryEntry{value: cloneInventory(inventory)}
	return nil
}

func cachedInventory(cache *auditInventory, scope Scope, manager, executable, root string) (Inventory, bool) {
	if cache == nil {
		return Inventory{}, false
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	key := inventoryKey{manager: manager, executable: filepath.Clean(executable), root: filepath.Clean(root), project: scopeKey(scope)}
	entry, ok := cache.inventories[key]
	if !ok {
		return Inventory{}, false
	}
	return cloneInventory(entry.value), true
}

func cloneInventory(inventory Inventory) Inventory {
	inventory.Items = slices.Clone(inventory.Items)
	for i := range inventory.Items {
		inventory.Items[i].Executables = slices.Clone(inventory.Items[i].Executables)
		if inventory.Items[i].InstalledAt != nil {
			evidence := *inventory.Items[i].InstalledAt
			inventory.Items[i].InstalledAt = &evidence
		}
	}
	return inventory
}

// BrewInventory observes the installed Homebrew root without refreshing metadata.
func BrewInventory(ctx context.Context, host *Host, scope Scope) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, fmt.Errorf("inspect Homebrew inventory: %w", err)
	}
	path, ok := findExecutable(host, "brew")
	if !ok {
		return Inventory{}, errors.New("homebrew executable is unavailable")
	}
	root := filepath.Dir(filepath.Dir(path))
	if inventory, ok := cachedInventory(host.inventory, scope, "brew", path, root); ok {
		return inventory, nil
	}
	return Inventory{Manager: "brew", Executable: path, Root: root, ObservedAt: hostNow(host), FreshnessNote: "Local installed receipts only; manager update availability was not refreshed."}, nil
}

// NpmInventory observes npm's selected global prefix without querying a registry.
func NpmInventory(ctx context.Context, host *Host, scope Scope) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, fmt.Errorf("inspect npm inventory: %w", err)
	}
	path, ok := findExecutable(host, "npm")
	if !ok {
		return Inventory{}, errors.New("npm executable is unavailable")
	}
	prefix := host.Env["NPM_CONFIG_PREFIX"]
	if prefix == "" {
		prefix = filepath.Dir(filepath.Dir(path))
	}
	root := packageRoot(prefix)
	if inventory, ok := cachedInventory(host.inventory, scope, "npm", path, root); ok {
		return inventory, nil
	}
	return Inventory{Manager: "npm", Executable: path, Root: root, ObservedAt: hostNow(host), FreshnessNote: "Local package metadata only; registry availability was not queried."}, nil
}

// UvInventory reports uv's documented tool root without launching transient tools.
func UvInventory(ctx context.Context, host *Host, scope Scope) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, fmt.Errorf("inspect uv inventory: %w", err)
	}
	path, ok := findExecutable(host, "uv")
	if !ok {
		return Inventory{}, errors.New("uv executable is unavailable")
	}
	root := host.Env["UV_TOOL_DIR"]
	if root == "" {
		root = filepath.Join(uvDataDir(host), "tools")
	}
	if inventory, ok := cachedInventory(host.inventory, scope, "uv", path, root); ok {
		return inventory, nil
	}
	return Inventory{Manager: "uv", Executable: path, Root: root, ObservedAt: hostNow(host), FreshnessNote: "Documented tool root observed; package inventory format is not established."}, nil
}

func uvDataDir(host *Host) string {
	if path := host.Env["XDG_DATA_HOME"]; filepath.IsAbs(path) {
		return filepath.Join(path, "uv")
	}
	return filepath.Join(host.Home, ".local", "share", "uv")
}

// MatchProvenance requires both package-root membership and matching executable evidence.
func MatchProvenance(instance Instance, inventory Inventory) Provenance {
	for _, item := range inventory.Items {
		if instance.Root.State != EvidenceKnown || !samePath(instance.Root.Value, item.Root) {
			continue
		}
		for _, executable := range item.Executables {
			if instance.Executable.State == EvidenceKnown && samePath(instance.Executable.Value, executable) {
				return Provenance{State: EvidenceKnown, Manager: inventory.Manager, Package: item.Package, Root: item.Root, Channel: item.Channel}
			}
		}
	}
	return Provenance{State: EvidenceUnavailable}
}

func parseReceiptInstallationTime(raw []byte, observedAt time.Time) *InstallationEvidence {
	var receipt struct {
		Time *int64 `json:"time"`
	}
	if json.Unmarshal(raw, &receipt) != nil || receipt.Time == nil || *receipt.Time <= 0 {
		return nil
	}
	at := time.Unix(*receipt.Time, 0).UTC()
	if at.Year() < 1 || at.Year() > 9999 || at.After(observedAt) {
		return nil
	}
	return &InstallationEvidence{At: at, Source: "Homebrew INSTALL_RECEIPT.json time", Meaning: "current package revision receipt time", Precision: "second", ObservedAt: observedAt}
}
