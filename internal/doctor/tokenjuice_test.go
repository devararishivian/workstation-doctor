package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTokenjuiceManagerSelection(t *testing.T) {
	t.Parallel()
	host, _ := syntheticNpmTool(t, "tokenjuice", "tokenjuice")
	d := discoverTokenjuice(t.Context(), host, Scope{})
	if len(d.Instances) != 1 || d.Instances[0].Provenance.Package != "tokenjuice" {
		t.Fatalf("discovery=%+v", d)
	}
	assertNpmProposal(t, host, checkTokenjuiceInstance(t.Context(), host, Scope{}, d.Instances[0])[0], "tokenjuice@1.2.4")
	i := d.Instances[0]
	i.Provenance = Provenance{State: EvidenceUnavailable}
	assertNoAutomatic(t, checkTokenjuiceInstance(t.Context(), host, Scope{}, i)[0])
}

func TestTokenjuiceHomebrewEvidence(t *testing.T) {
	host, exe := syntheticBrewTool(t, "tokenjuice", "tokenjuice", "vincentkoc/tap")
	d := discoverTokenjuice(t.Context(), host, Scope{})
	if len(d.Instances) != 1 || d.Instances[0].Executable.Value != exe || d.Instances[0].Version.Value != "1.2.3" || d.Instances[0].Provenance.Manager != "brew" {
		t.Fatalf("discovery=%+v", d)
	}
	assertNoAutomatic(t, checkTokenjuiceInstance(t.Context(), host, Scope{}, d.Instances[0])[0])
}

func syntheticBrewTool(t *testing.T, name, pkg, tap string) (*Host, string) {
	t.Helper()
	prefix := t.TempDir()
	root := filepath.Join(prefix, "Cellar", pkg, "1.2.3")
	bin := filepath.Join(prefix, "bin")
	for _, dir := range []string{filepath.Join(root, "bin"), bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(root, "bin", name)
	if err := os.WriteFile(target, []byte("synthetic; never execute"), 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(bin, name)
	if err := os.Symlink(target, exe); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "brew"), []byte("synthetic; never execute"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "INSTALL_RECEIPT.json"), []byte(`{"time":1791321600,"source":{"tap":"`+tap+`","spec":"stable"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	host := &Host{OS: "linux", Home: t.TempDir(), Path: bin, Env: map[string]string{}, inventory: &auditInventory{}}
	host.RunRead = func(context.Context, Command) (CommandResult, error) {
		t.Error("Homebrew discovery executed a process")
		return CommandResult{}, errors.New("forbidden")
	}
	host.Fetch = func(context.Context, string) ([]byte, error) { return []byte(`{"version":"1.2.4"}`), nil }
	return host, exe
}
