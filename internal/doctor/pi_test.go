package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func syntheticNpmTool(t *testing.T, name, pkg string) (*Host, string) {
	t.Helper()
	prefix := t.TempDir()
	root := filepath.Join(prefix, "lib", "node_modules", filepath.FromSlash(pkg))
	bin := filepath.Join(prefix, "bin")
	for _, dir := range []string{root, bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(root, "cli.js")
	if err := os.WriteFile(target, []byte("synthetic executable; never execute"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"`+pkg+`","version":"1.2.3","bin":{"`+name+`":"cli.js"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(bin, name)
	if err := os.Symlink(target, executable); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte("synthetic manager; never execute"), 0o700); err != nil {
		t.Fatal(err)
	}
	host := &Host{OS: "linux", Home: t.TempDir(), Path: bin, Env: map[string]string{"NPM_CONFIG_PREFIX": prefix}, Now: func() time.Time { return time.Unix(1800000000, 0) }, inventory: &auditInventory{}}
	host.RunRead = func(context.Context, Command) (CommandResult, error) {
		t.Error("metadata discovery executed a process")
		return CommandResult{}, errors.New("forbidden")
	}
	host.Fetch = func(_ context.Context, _ string) ([]byte, error) { return []byte(`{"version":"1.2.4"}`), nil }
	return host, executable
}

func TestPiInstanceSources(t *testing.T) {
	t.Parallel()
	t.Run("npm metadata and agent override", func(t *testing.T) {
		host, exe := syntheticNpmTool(t, "pi", "@earendil-works/pi-coding-agent")
		agent := t.TempDir()
		host.Env["PI_CODING_AGENT_DIR"] = agent
		d := discoverPi(t.Context(), host, Scope{})
		if len(d.Instances) != 1 {
			t.Fatalf("discovery=%+v", d)
		}
		i := d.Instances[0]
		if i.Executable.Value != exe || i.Version.Value != "1.2.3" || i.Provenance.State != EvidenceKnown || i.Provenance.Package != "@earendil-works/pi-coding-agent" {
			t.Fatalf("instance=%+v", i)
		}
		if len(i.Configuration) != 1 || i.Configuration[0].Value != agent {
			t.Fatalf("configuration=%+v", i.Configuration)
		}
		f := checkPiInstance(t.Context(), host, Scope{}, i)[0]
		assertNpmProposal(t, host, f, "@earendil-works/pi-coding-agent@1.2.4")
	})
	t.Run("absent", func(t *testing.T) {
		host := &Host{Home: t.TempDir(), Path: t.TempDir(), Env: map[string]string{}}
		if d := discoverPi(t.Context(), host, Scope{}); d.Availability != AvailabilityAbsent {
			t.Fatalf("discovery=%+v", d)
		}
	})
	t.Run("native ownership stays unknown", func(t *testing.T) {
		host, exe := syntheticNpmTool(t, "pi", "@earendil-works/pi-coding-agent")
		host.Env["NPM_CONFIG_PREFIX"] = t.TempDir()
		d := discoverPi(t.Context(), host, Scope{Locations: map[string][]string{"pi": {exe}}})
		if len(d.Instances) != 1 || d.Instances[0].Provenance.State == EvidenceKnown {
			t.Fatalf("discovery=%+v", d)
		}
		assertNoAutomatic(t, checkPiInstance(t.Context(), host, Scope{}, d.Instances[0])[0])
	})
}

func assertNoAutomatic(t *testing.T, f Finding) {
	t.Helper()
	for _, a := range f.Actions {
		if a.Mode == ActionAutomatic {
			t.Fatalf("guessed updater: %+v", a)
		}
	}
}

func assertNpmProposal(t *testing.T, host *Host, f Finding, target string) {
	t.Helper()
	if f.Outcome != OutcomeAttention || len(f.Actions) != 1 {
		t.Fatalf("finding=%+v", f)
	}
	a := f.Actions[0]
	if err := ValidateProposal(a, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if a.Mode != ActionAutomatic || len(a.Steps) != 1 {
		t.Fatalf("proposal=%+v", a)
	}
	c := a.Steps[0].Command
	if c.Executable != filepath.Join(host.Path, "npm") || len(c.Args) != 5 || c.Args[0] != "install" || c.Args[1] != "--global" || c.Args[2] != "--prefix" || c.Args[3] != host.Env["NPM_CONFIG_PREFIX"] || c.Args[4] != target {
		t.Fatalf("command=%+v", c)
	}
}
