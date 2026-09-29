package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The table must stay free of escape codes when stdout is not a
// terminal, so scripts can pipe it. lipgloss handles that; this test
// locks the behavior in.
func TestFormatTableIsPlainWhenPiped(t *testing.T) {
	out := FormatTable([]Result{
		{Component: "pi", Installed: "0.85.1", Latest: "0.85.1", Status: StatusOK, Note: "npm"},
	})
	if strings.Contains(out, "\x1b") {
		t.Fatalf("table contains escape codes:\n%q", out)
	}
	for _, want := range []string{"COMPONENT", "INSTALLED", "LATEST", "STATUS", "NOTE", "Summary:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table misses %q:\n%s", want, out)
		}
	}
}

func TestFormatManualEmpty(t *testing.T) {
	if got := FormatManual(nil); !strings.Contains(got, "No action is needed") {
		t.Fatalf("unexpected manual output:\n%s", got)
	}
}

func TestFormatManualListsPending(t *testing.T) {
	got := FormatManual([]Result{
		{Component: "pi", Installed: "0.85.0", Latest: "0.85.1", Status: StatusUpdate, Note: "npm", Manual: "npm i -g x", Fix: "npm i -g x"},
	})
	if !strings.Contains(got, "1. npm i -g x") {
		t.Fatalf("unexpected manual output:\n%s", got)
	}
}

func TestEvaluateHerdrPlugins(t *testing.T) {
	tests := []struct {
		name          string
		plugins       []herdrPluginItem
		resolveFunc   func(owner, repo, ref, managedPath string) (string, error)
		wantStatus    string
		wantInstalled string
		wantLatest    string
		wantNoteSub   string
		wantManualSub string
		wantFixSub    string
	}{
		{
			name:          "empty plugins list",
			plugins:       nil,
			resolveFunc:   nil,
			wantStatus:    StatusOK,
			wantInstalled: "0 plugins",
			wantLatest:    "0 plugins",
			wantNoteSub:   "no plugins installed",
		},
		{
			name: "all plugins up to date",
			plugins: []herdrPluginItem{
				{
					PluginID: "annotate",
					Name:     "Annotate",
					Source: herdrPluginSource{
						Kind:           "github",
						Owner:          "plannotator",
						Repo:           "herdr-annotate",
						ResolvedCommit: "commit1",
					},
				},
				{
					PluginID: "auto-title",
					Name:     "Auto Title",
					Source: herdrPluginSource{
						Kind:           "github",
						Owner:          "kryptamine",
						Repo:           "herdr-auto-title",
						ResolvedCommit: "commit2",
					},
				},
			},
			resolveFunc: func(_, repo, _, _ string) (string, error) {
				if repo == "herdr-annotate" {
					return "commit1", nil
				}
				return "commit2", nil
			},
			wantStatus:    StatusOK,
			wantInstalled: "2 plugins",
			wantLatest:    "current",
			wantNoteSub:   "all plugins are in sync",
		},
		{
			name: "one plugin outdated",
			plugins: []herdrPluginItem{
				{
					PluginID: "annotate",
					Name:     "Annotate",
					Source: herdrPluginSource{
						Kind:           "github",
						Owner:          "plannotator",
						Repo:           "herdr-annotate",
						ResolvedCommit: "commit1",
					},
				},
			},
			resolveFunc: func(_, _, _, _ string) (string, error) {
				return "commit2", nil
			},
			wantStatus:    StatusUpdate,
			wantInstalled: "1 outdated",
			wantLatest:    "current",
			wantNoteSub:   "outdated: annotate",
			wantManualSub: "herdr plugin install plannotator/herdr-annotate",
			wantFixSub:    "herdr plugin install plannotator/herdr-annotate --yes",
		},
		{
			name: "plugin with ref and subdir",
			plugins: []herdrPluginItem{
				{
					PluginID: "custom-tool",
					Source: herdrPluginSource{
						Kind:           "github",
						Owner:          "owner",
						Repo:           "monorepo",
						Subdir:         "plugins/tool",
						RequestedRef:   "v1.2.0",
						ResolvedCommit: "oldsha",
					},
				},
			},
			resolveFunc: func(_, _, _, _ string) (string, error) {
				return "newsha", nil
			},
			wantStatus:    StatusUpdate,
			wantInstalled: "1 outdated",
			wantLatest:    "current",
			wantNoteSub:   "outdated: custom-tool",
			wantManualSub: "herdr plugin install owner/monorepo/plugins/tool --ref v1.2.0",
			wantFixSub:    "herdr plugin install owner/monorepo/plugins/tool --ref v1.2.0 --yes",
		},
		{
			name: "non-github plugin is ignored",
			plugins: []herdrPluginItem{
				{
					PluginID: "local-dev",
					Source: herdrPluginSource{
						Kind: "local",
					},
				},
			},
			resolveFunc: func(_, _, _, _ string) (string, error) {
				return "some-sha", nil
			},
			wantStatus:    StatusOK,
			wantInstalled: "1 plugins",
			wantLatest:    "current",
			wantNoteSub:   "all plugins are in sync",
		},
		{
			name: "remote check returns error",
			plugins: []herdrPluginItem{
				{
					PluginID: "broken-remote",
					Source: herdrPluginSource{
						Kind:           "github",
						Owner:          "owner",
						Repo:           "repo",
						ResolvedCommit: "sha",
					},
				},
			},
			resolveFunc: func(_, _, _, _ string) (string, error) {
				return "", errors.New("network timeout")
			},
			wantStatus:    StatusUnknown,
			wantInstalled: "1 plugins",
			wantLatest:    "-",
			wantNoteSub:   "cannot reach remote repository for: broken-remote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateHerdrPlugins(tt.plugins, tt.resolveFunc)
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Installed != tt.wantInstalled {
				t.Errorf("installed = %q, want %q", got.Installed, tt.wantInstalled)
			}
			if got.Latest != tt.wantLatest {
				t.Errorf("latest = %q, want %q", got.Latest, tt.wantLatest)
			}
			if !strings.Contains(got.Note, tt.wantNoteSub) {
				t.Errorf("note %q does not contain %q", got.Note, tt.wantNoteSub)
			}
			if tt.wantManualSub != "" && !strings.Contains(got.Manual, tt.wantManualSub) {
				t.Errorf("manual %q does not contain %q", got.Manual, tt.wantManualSub)
			}
			if tt.wantFixSub != "" && !strings.Contains(got.Fix, tt.wantFixSub) {
				t.Errorf("fix %q does not contain %q", got.Fix, tt.wantFixSub)
			}
		})
	}
}

func TestToolCheckersInterface(t *testing.T) {
	checkers := []Checker{
		&piVersionChecker{},
		&herdrVersionChecker{},
		&ghosttyVersionChecker{},
		&starshipVersionChecker{},
		newNpmPackageChecker("opencode", "opencode-ai"),
		newNpmPackageChecker("tokenjuice", "tokenjuice"),
		&serenaVersionChecker{},
		&gortexVersionChecker{},
	}

	for _, c := range checkers {
		if c.Name() == "" {
			t.Errorf("checker %T has empty Name()", c)
		}
		if c.Category() != CategoryTool {
			t.Errorf("checker %s category = %s, want %s", c.Name(), c.Category(), CategoryTool)
		}
	}
}

func TestConfigCheckers(t *testing.T) {
	checkers := []Checker{
		&ghosttyConfigValidChecker{},
		&ghosttyConfigVersionChecker{},
		&herdrConfigValidChecker{},
		&herdrConfigVersionChecker{},
		&piConfigValidChecker{},
		&piConfigVersionChecker{},
	}

	for _, c := range checkers {
		if c.Category() != CategoryConfig {
			t.Errorf("checker %s category = %s, want %s", c.Name(), c.Category(), CategoryConfig)
		}
	}
}

func TestEvaluateStarshipVersion(t *testing.T) {
	tests := []struct {
		name       string
		installed  string
		latest     string
		wantStatus string
		wantManual string
		wantFix    string
		wantNote   string
	}{
		{
			name:       "matching versions",
			installed:  "1.26.0",
			latest:     "1.26.0",
			wantStatus: StatusOK,
			wantNote:   "brew",
		},
		{
			name:       "outdated version",
			installed:  "1.25.0",
			latest:     "1.26.0",
			wantStatus: StatusUpdate,
			wantManual: "brew upgrade starship",
			wantFix:    "brew upgrade starship",
			wantNote:   "brew formula is outdated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := evaluateStarshipVersion(tt.installed, tt.latest)
			if res.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", res.Status, tt.wantStatus)
			}
			if tt.wantManual != "" && res.Manual != tt.wantManual {
				t.Errorf("manual = %q, want %q", res.Manual, tt.wantManual)
			}
			if tt.wantFix != "" && res.Fix != tt.wantFix {
				t.Errorf("fix = %q, want %q", res.Fix, tt.wantFix)
			}
			if res.Note != tt.wantNote {
				t.Errorf("note = %q, want %q", res.Note, tt.wantNote)
			}
		})
	}
}

func TestEvaluatePiConfigVersion(t *testing.T) {
	// If lastChangelogVersion matches installed, status is OK
	res := evaluatePiConfigVersion("0.87.1", "0.87.1")
	if res.Status != StatusOK {
		t.Errorf("got status %s, want %s", res.Status, StatusOK)
	}

	// If lastChangelogVersion is older, status is UPDATE
	res = evaluatePiConfigVersion("0.85.0", "0.87.1")
	if res.Status != StatusUpdate {
		t.Errorf("got status %s, want %s", res.Status, StatusUpdate)
	}
}

func TestEvaluatePiConfigValid(t *testing.T) {
	t.Run("valid with mcp-adapter.json", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcp-adapter.json"), []byte(`{"mcpServers":{"server1":{},"server2":{}},"secret":"do-not-leak"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcp-cache.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}

		res := evaluatePiConfigValid(dir)
		if res.Status != StatusOK {
			t.Fatalf("expected StatusOK, got %s (note: %s)", res.Status, res.Note)
		}
		if !strings.Contains(res.Note, "2 MCP servers, cache is present") {
			t.Errorf("unexpected note: %s", res.Note)
		}
		// Invariant: secrets must never leak into result text
		if strings.Contains(res.Note, "do-not-leak") {
			t.Errorf("secret leaked in note: %s", res.Note)
		}
	})

	t.Run("valid with legacy mcp.json fallback", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"mcpServers":{"srv1":{}}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcp-cache.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}

		res := evaluatePiConfigValid(dir)
		if res.Status != StatusOK {
			t.Fatalf("expected StatusOK, got %s (note: %s)", res.Status, res.Note)
		}
		if !strings.Contains(res.Note, "1 MCP servers, cache is present") {
			t.Errorf("unexpected note: %s", res.Note)
		}
	})

	t.Run("missing mcp-adapter.json and mcp.json", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcp-cache.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}

		res := evaluatePiConfigValid(dir)
		if res.Status != StatusUnknown {
			t.Fatalf("expected StatusUnknown, got %s", res.Status)
		}
		if !strings.Contains(res.Note, "mcp-adapter.json is missing") {
			t.Errorf("expected missing mcp-adapter.json in note: %s", res.Note)
		}
	})

	t.Run("invalid JSON in mcp-adapter.json", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcp-adapter.json"), []byte(`{broken`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcp-cache.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}

		res := evaluatePiConfigValid(dir)
		if res.Status != StatusUnknown {
			t.Fatalf("expected StatusUnknown, got %s", res.Status)
		}
		if !strings.Contains(res.Note, "mcp-adapter.json has invalid JSON") {
			t.Errorf("expected invalid JSON note: %s", res.Note)
		}
	})

	t.Run("invalid JSON in legacy mcp.json", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{bad-legacy`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcp-cache.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}

		res := evaluatePiConfigValid(dir)
		if res.Status != StatusUnknown {
			t.Fatalf("expected StatusUnknown, got %s", res.Status)
		}
		if !strings.Contains(res.Note, "mcp.json has invalid JSON") {
			t.Errorf("expected invalid JSON note: %s", res.Note)
		}
	})

	t.Run("missing settings and cache", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "mcp-adapter.json"), []byte(`{"mcpServers":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}

		res := evaluatePiConfigValid(dir)
		if res.Status != StatusUnknown {
			t.Fatalf("expected StatusUnknown, got %s", res.Status)
		}
		if !strings.Contains(res.Note, "settings.json is missing") {
			t.Errorf("expected missing settings in note: %s", res.Note)
		}
		if !strings.Contains(res.Note, "mcp-cache.json is missing") {
			t.Errorf("expected missing cache in note: %s", res.Note)
		}
	})
}

type mockChecker struct {
	name  string
	delay time.Duration
}

func (m *mockChecker) Name() string       { return m.name }
func (m *mockChecker) Category() Category { return CategoryTool }
func (m *mockChecker) Check(_ context.Context) Result {
	time.Sleep(m.delay)
	return Result{Component: m.name, Status: StatusOK}
}

func TestEngineRunDeterministicOrder(t *testing.T) {
	engine := &Engine{
		checkers: []Checker{
			&mockChecker{name: "first", delay: 30 * time.Millisecond},
			&mockChecker{name: "second", delay: 10 * time.Millisecond},
			&mockChecker{name: "third", delay: 20 * time.Millisecond},
		},
	}

	res := engine.Run(context.Background())
	if len(res) != 3 {
		t.Fatalf("len(res) = %d, want 3", len(res))
	}
	if res[0].Component != "first" || res[1].Component != "second" || res[2].Component != "third" {
		t.Errorf("unexpected order: %s, %s, %s", res[0].Component, res[1].Component, res[2].Component)
	}
}

func TestSystemCheckersInterface(t *testing.T) {
	checkers := []Checker{
		&brewOutdatedChecker{},
		&npmOutdatedGlobalChecker{},
		&piPackagesChecker{},
		&superpowersChecker{},
		&herdrIntegrationsChecker{},
		&herdrPluginsChecker{},
		&skillsChecker{},
	}

	for _, c := range checkers {
		if c.Name() == "" {
			t.Errorf("checker %T has empty Name()", c)
		}
		if c.Category() != CategorySystem && c.Category() != CategorySkill {
			t.Errorf("checker %s category = %s, want system or skill", c.Name(), c.Category())
		}
	}
}
