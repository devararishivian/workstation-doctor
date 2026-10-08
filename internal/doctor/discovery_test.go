package doctor

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestExecutableCandidates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		setup      func(*testing.T, *Host, Scope) Scope
		defaults   []string
		want       []string
		wantStatus Availability
	}{
		{
			name: "resolve PATH alias and documented default once",
			setup: func(t *testing.T, host *Host, _ Scope) Scope {
				root := t.TempDir()
				bin := filepath.Join(root, "bin")
				if err := os.Mkdir(bin, 0o700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(root, "real-tool")
				if err := os.WriteFile(target, []byte("synthetic"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(bin, "tool")); err != nil {
					t.Fatal(err)
				}
				host.Path = bin
				return Scope{}
			},
			defaults: []string{"tool", "tool", "missing-tool"}, wantStatus: AvailabilityPresent,
		},
		{
			name: "explicit location with invalid path does not fall back",
			setup: func(t *testing.T, _ *Host, scope Scope) Scope {
				scope.Locations = map[string][]string{"fixture": {filepath.Join(t.TempDir(), "missing")}}
				return scope
			},
			defaults: []string{"fallback"}, wantStatus: AvailabilityUndetermined,
		},
		{
			name: "unreadable explicit override",
			setup: func(t *testing.T, _ *Host, scope Scope) Scope {
				file := filepath.Join(t.TempDir(), "not-a-directory")
				if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				scope.Locations = map[string][]string{"fixture": {filepath.Join(file, "tool")}}
				return scope
			},
			defaults: []string{"fallback"}, wantStatus: AvailabilityUndetermined,
		},
		{
			name:     "absent candidate",
			setup:    func(t *testing.T, host *Host, _ Scope) Scope { host.Path = t.TempDir(); return Scope{} },
			defaults: []string{"missing"}, wantStatus: AvailabilityAbsent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := &Host{OS: "linux", Home: t.TempDir(), Path: t.TempDir(), Now: func() time.Time { return time.Unix(100, 0) }}
			scope := tt.setup(t, host, Scope{})
			got := ExecutableCandidates(t.Context(), host, scope, "fixture", "tool", tt.defaults)
			if got.Availability != tt.wantStatus {
				t.Fatalf("availability=%s want %s; %+v", got.Availability, tt.wantStatus, got)
			}
			paths := make([]string, len(got.Instances))
			for i := range got.Instances {
				paths[i] = got.Instances[i].Executable.Value
			}
			if tt.want != nil && !slices.Equal(paths, tt.want) {
				t.Fatalf("executables=%v want %v", paths, tt.want)
			}
		})
	}
}

func TestManagerOwnership(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	packageDir := filepath.Join(root, "lib", "node_modules", "fixture")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := filepath.Join(binDir, "npm")
	if err := os.WriteFile(manager, []byte("manager"), 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(binDir, "fixture")
	if err := os.Symlink(filepath.Join(packageDir, "bin"), executable); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{Manager: "npm", Executable: manager, Root: filepath.Join(root, "lib", "node_modules"), Items: []InventoryItem{{Package: "fixture", Root: packageDir, Executables: []string{executable}}}}
	tests := []struct {
		name     string
		instance Instance
		want     EvidenceState
	}{
		{name: "same executable and root", instance: Instance{Executable: Fact{State: EvidenceKnown, Value: executable}, Root: Fact{State: EvidenceKnown, Value: packageDir}}, want: EvidenceKnown},
		{name: "same name at separate prefix", instance: Instance{Executable: Fact{State: EvidenceKnown, Value: filepath.Join(t.TempDir(), "bin", "fixture")}, Root: Fact{State: EvidenceKnown, Value: filepath.Join(t.TempDir(), "lib", "node_modules", "fixture")}}, want: EvidenceUnavailable},
		{name: "manager presence alone is not provenance", instance: Instance{Executable: Fact{State: EvidenceKnown, Value: filepath.Join(t.TempDir(), "unrelated", "fixture")}}, want: EvidenceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchProvenance(tt.instance, inventory)
			if got.State != tt.want {
				t.Fatalf("provenance=%+v want state %s", got, tt.want)
			}
		})
	}
}

func TestInventoryCacheScope(t *testing.T) {
	t.Parallel()
	cache := &auditInventory{}
	one := Inventory{Manager: "npm", Executable: "/one/bin/npm", Root: "/one", Items: []InventoryItem{{Package: "fixture"}}}
	two := Inventory{Manager: "npm", Executable: "/two/bin/npm", Root: "/two"}
	if err := cacheInventory(cache, Scope{}, one); err != nil {
		t.Fatal(err)
	}
	if err := cacheInventory(cache, Scope{}, two); err != nil {
		t.Fatal(err)
	}
	gotOne, ok := cachedInventory(cache, Scope{}, "npm", "/one/bin/npm", "/one")
	if !ok || len(gotOne.Items) != 1 {
		t.Fatalf("first inventory missing: %+v", gotOne)
	}
	gotOne.Items[0].Package = "changed"
	again, ok := cachedInventory(cache, Scope{}, "npm", "/one/bin/npm", "/one")
	if !ok || again.Items[0].Package != "fixture" {
		t.Fatal("cache exposed mutable inventory state")
	}
	if _, ok := cachedInventory(cache, Scope{}, "npm", "/one/bin/npm", "/two"); ok {
		t.Fatal("cache crossed manager prefix")
	}
	project := Scope{ProjectDir: "/project"}
	if _, ok := cachedInventory(cache, project, "npm", "/one/bin/npm", "/one"); ok {
		t.Fatal("cache crossed project scope")
	}
	if scopeKey(defaultScope(cache)) != "user" {
		t.Fatal("unexpected default scope")
	}
}

func TestReceiptInstallationTime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     []byte
		want    bool
		meaning string
	}{
		{name: "known receipt time", raw: []byte(`{"time":1791321600}`), want: true, meaning: "current package revision receipt time"},
		{name: "missing", raw: []byte(`{}`)},
		{name: "invalid", raw: []byte(`{"time":-1}`)},
		{name: "malformed", raw: []byte(`{"time":"later"}`)},
		{name: "overflow", raw: []byte(`{"time":9223372036854775807}`)},
		{name: "future", raw: []byte(`{"time":1791321600}`), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observedAt := time.Unix(1800000000, 0)
			if tt.name == "future" {
				observedAt = time.Unix(1700000000, 0)
			}
			got := parseReceiptInstallationTime(tt.raw, observedAt)
			if (got != nil) != tt.want {
				t.Fatalf("evidence=%+v want present %v", got, tt.want)
			}
			if got != nil && got.Meaning != tt.meaning {
				t.Fatalf("meaning=%q", got.Meaning)
			}
		})
	}
}
