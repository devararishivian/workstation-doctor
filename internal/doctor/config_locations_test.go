package doctor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resourceFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func configInstance(id string) Instance {
	return Instance{ID: "synthetic", IntegrationID: id, Version: Fact{State: EvidenceKnown, Value: "1.2.3"}}
}

func TestConfigurationPrecedence(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	h.Env["PI_CODING_AGENT_DIR"] = filepath.Join(h.Home, "custom")
	p, e := ResolvePiConfiguration(h, Scope{})
	if e != nil || p[0].Value != filepath.Join(h.Home, "custom", "settings.json") {
		t.Fatalf("pi=%v %v", p, e)
	}
	h.Env["XDG_CONFIG_HOME"] = filepath.Join(h.Home, "xdg")
	p, e = ResolveHerdrConfiguration(h, Scope{})
	if e != nil || p[0].Value != filepath.Join(h.Home, "xdg", "herdr", "config.toml") {
		t.Fatalf("herdr=%v %v", p, e)
	}
	explicit := filepath.Join(h.Home, "explicit.toml")
	h.Env["HERDR_CONFIG_PATH"] = explicit
	p, e = ResolveHerdrConfiguration(h, Scope{})
	if e != nil || p[0].Value != explicit {
		t.Fatal(p, e)
	}
	h.Env["HERDR_CONFIG_PATH"] = "relative"
	if _, e = ResolveHerdrConfiguration(h, Scope{}); e == nil {
		t.Fatal("invalid override fell back")
	}
	p, e = ResolveGhosttyConfiguration(h, Scope{}, "1.2.3")
	if e != nil || len(p) != 4 || filepath.Base(p[0].Value) != "config.ghostty" || filepath.Base(p[1].Value) != "config" {
		t.Fatal(p, e)
	}
	p, e = ResolveGhosttyConfiguration(h, Scope{}, "1.2.2")
	if e != nil || len(p) != 2 || filepath.Base(p[0].Value) != "config" {
		t.Fatal(p, e)
	}
}

func TestConfigurationOptionalDefaults(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	for _, tc := range []struct {
		id   string
		eval func(context.Context, *Host, Scope, Instance) []Finding
	}{{"pi", checkPiConfigValid}, {"herdr", checkHerdrConfigValid}, {"ghostty", checkGhosttyConfigValid}} {
		t.Run(tc.id, func(t *testing.T) {
			f := tc.eval(t.Context(), h, Scope{}, configInstance(tc.id))[0]
			if f.Outcome != OutcomeOK {
				t.Fatalf("default=%+v", f)
			}
			assertNoAutomatic(t, f)
		})
	}
	resourceFile(t, filepath.Join(h.Home, ".config", "herdr", "config.toml"), "[ui]\n")
	path := filepath.Join(h.Home, ".pi", "agent", "settings.json")
	if e := os.MkdirAll(path, 0o700); e != nil {
		t.Fatal(e)
	}
	if f := checkPiConfigValid(t.Context(), h, Scope{}, configInstance("pi"))[0]; f.Outcome != OutcomeUnknown {
		t.Fatal(f)
	}
}

func TestMCPAggregateOnly(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{"mcpServers":{"SECRET_NAME":{"command":"SECRET_COMMAND","url":"SECRET_URL","headers":{"SECRET_HEADER":"SECRET_TOKEN"}}}}`, `{"mcpServers": SECRET_TOKEN}`} {
		t.Run(raw[:12], func(t *testing.T) {
			h := testHost(t)
			resourceFile(t, filepath.Join(h.Home, ".pi", "agent", "mcp.json"), raw)
			f := checkPiConfigValid(t.Context(), h, Scope{}, configInstance("pi"))[0]
			b, e := json.Marshal(f)
			if e != nil {
				t.Fatal(e)
			}
			for _, sentinel := range []string{"SECRET_NAME", "SECRET_COMMAND", "SECRET_URL", "SECRET_HEADER", "SECRET_TOKEN"} {
				if strings.Contains(string(b), sentinel) {
					t.Fatal("secret leak")
				}
			}
			if strings.Contains(raw, "SECRET_TOKEN}") && f.Outcome != OutcomeAttention {
				t.Fatal(f)
			}
		})
	}
}
