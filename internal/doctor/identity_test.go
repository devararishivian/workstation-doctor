package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalInstanceID(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "tool")
	alias := filepath.Join(root, "alias")
	other := filepath.Join(root, "other")
	for _, p := range []string{path, other} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	original, err := CanonicalInstanceID("fixture", "user", path)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, integration, scope, path string
		same, wantError                bool
	}{
		{"alias", "fixture", "user", alias, true, false},
		{"cleaned path", "fixture", "user", filepath.Join(root, ".", "tool"), true, false},
		{"other installation", "fixture", "user", other, false, false},
		{"other scope", "fixture", "project", path, false, false},
		{"other integration", "other", "user", path, false, false},
		{"missing override", "fixture", "user", filepath.Join(root, "missing"), false, true},
		{"empty integration", "", "user", path, false, true},
		{"empty scope", "fixture", "", path, false, true},
		{"empty path", "fixture", "user", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalInstanceID(tt.integration, tt.scope, tt.path)
			if (err != nil) != tt.wantError {
				t.Fatalf("identity error = %v, want error %t", err, tt.wantError)
			}
			if err == nil && (got == original) != tt.same {
				t.Fatalf("identity equality = %t, want %t", got == original, tt.same)
			}
		})
	}
}
