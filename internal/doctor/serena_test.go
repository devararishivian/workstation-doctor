package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSerenaPersistentAndDeclaredSources(t *testing.T) {
	t.Parallel()
	t.Run("persistent uv receipt and exact entrypoint", func(t *testing.T) {
		prefix := t.TempDir()
		root := filepath.Join(prefix, "tools")
		tool := filepath.Join(root, "serena-agent")
		bin := filepath.Join(prefix, "bin")
		site := filepath.Join(tool, "lib", "python3.13", "site-packages", "serena_agent-1.2.3.dist-info")
		for _, dir := range []string{filepath.Join(tool, "bin"), bin, site} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(tool, "bin", "serena")
		exe := filepath.Join(bin, "serena")
		if err := os.WriteFile(target, []byte("synthetic; never execute"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, exe); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "uv"), []byte("synthetic; never execute"), 0o700); err != nil {
			t.Fatal(err)
		}
		receipt := `[tool]
requirements = [{ name = "serena-agent" }]
entrypoints = [{ name = "serena", install-path = "` + exe + `", from = "serena-agent" }]
`
		if err := os.WriteFile(filepath.Join(tool, "uv-receipt.toml"), []byte(receipt), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(site, "METADATA"), []byte("Metadata-Version: 2.4\nName: serena-agent\nVersion: 1.2.3\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		host := &Host{OS: "linux", Home: t.TempDir(), Path: bin, Env: map[string]string{"UV_TOOL_DIR": root}, inventory: &auditInventory{}}
		host.RunRead = func(context.Context, Command) (CommandResult, error) {
			t.Error("uv discovery launched a process")
			return CommandResult{}, errors.New("forbidden")
		}
		host.Fetch = func(context.Context, string) ([]byte, error) { return []byte(`{"info":{"version":"1.2.4"}}`), nil }
		d := discoverSerena(t.Context(), host, Scope{})
		if len(d.Instances) != 1 || d.Instances[0].Provenance.Manager != "uv" || d.Instances[0].Version.Value != "1.2.3" {
			t.Fatalf("discovery=%+v", d)
		}
		f := checkSerenaInstance(t.Context(), host, Scope{}, d.Instances[0])[0]
		if f.Outcome != OutcomeAttention {
			t.Fatalf("finding=%+v", f)
		}
		assertNoAutomatic(t, f)
	})
	t.Run("declared transient source does not imply installed runtime", func(t *testing.T) {
		root := t.TempDir()
		config := filepath.Join(root, "mcp.json")
		raw := `{"mcpServers":{"synthetic-secret-name":{"command":"uvx","args":["--from","git+https://github.com/oraios/serena","serena","start-mcp-server"],"env":{"TOKEN":"synthetic-secret-value"}}}}`
		if err := os.WriteFile(config, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		host := &Host{OS: "linux", Home: root, Path: t.TempDir(), Env: map[string]string{}}
		host.RunRead = func(context.Context, Command) (CommandResult, error) {
			t.Error("declaration launched a process")
			return CommandResult{}, errors.New("forbidden")
		}
		scope := Scope{Locations: map[string][]string{"serena": {config}}}
		d := discoverSerena(t.Context(), host, scope)
		if d.Availability != AvailabilityUndetermined || len(d.Instances) != 1 || d.Instances[0].Executable.State == EvidenceKnown {
			t.Fatalf("declaration=%+v", d)
		}
		f := checkSerenaInstance(t.Context(), host, scope, d.Instances[0])[0]
		encoded, err := json.Marshal(struct {
			D Discovery
			F Finding
		}{d, f})
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"synthetic-secret-name", "synthetic-secret-value", "uvx", "git+https"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatal("declaration leaked configuration content")
			}
		}
		assertNoAutomatic(t, f)
	})
}
