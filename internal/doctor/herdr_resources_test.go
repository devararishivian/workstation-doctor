package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHerdrIntegrationApplicability(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	findings := checkHerdrIntegrationTargets(t.Context(), h, Scope{}, Instance{ID: "h", IntegrationID: "herdr-integr"})
	if len(findings) != 1 || findings[0].Outcome != OutcomeNotApplicable {
		t.Fatalf("absent targets=%+v", findings)
	}
}

func TestHerdrIntegrationUnsupportedStatus(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	makeExecutable(t, filepath.Join(h.Home, "herdr"))
	makeExecutable(t, filepath.Join(h.Home, "pi"))
	makeExecutable(t, filepath.Join(h.Home, "opencode"))
	h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
		if strings.Join(c.Args, " ") != "integration status" {
			t.Fatalf("command=%+v", c)
		}
		return CommandResult{Stdout: []byte("unrecognized target output")}, nil
	}
	finding := checkHerdrIntegrationTargets(t.Context(), h, Scope{}, Instance{ID: "i"})[0]
	if finding.Outcome != OutcomeUnknown {
		t.Fatalf("unsupported status=%+v", finding)
	}
}

func TestHerdrIntegrationAbsentSibling(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	makeExecutable(t, filepath.Join(h.Home, "herdr"))
	makeExecutable(t, filepath.Join(h.Home, "pi"))
	h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
		if strings.Join(c.Args, " ") != "integration status" {
			t.Fatalf("command=%+v", c)
		}
		return CommandResult{Stdout: []byte("pi: current (v4)\nopencode: not installed\n")}, nil
	}
	finding := checkHerdrIntegrationTargets(t.Context(), h, Scope{}, Instance{ID: "i"})[0]
	if finding.Outcome != OutcomeOK || len(finding.Evidence) != 1 || finding.Evidence[0].Value != "pi: current" {
		t.Fatalf("absent sibling affected selected target: %+v", finding)
	}
}

func TestHerdrIntegrationStaleStatus(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	makeExecutable(t, filepath.Join(h.Home, "herdr"))
	makeExecutable(t, filepath.Join(h.Home, "pi"))
	makeExecutable(t, filepath.Join(h.Home, "opencode"))
	h.RunRead = func(context.Context, Command) (CommandResult, error) {
		return CommandResult{Stdout: []byte("pi: current (v4)\nopencode: outdated (v2)\n")}, nil
	}
	finding := checkHerdrIntegrationTargets(t.Context(), h, Scope{}, Instance{ID: "i"})[0]
	if finding.Outcome != OutcomeAttention {
		t.Fatalf("stale status=%+v", finding)
	}
	assertNoAutomatic(t, finding)
}

func TestHerdrIntegrationStatusRejectsPartialUnknownOutput(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	makeExecutable(t, filepath.Join(h.Home, "herdr"))
	makeExecutable(t, filepath.Join(h.Home, "pi"))
	h.RunRead = func(context.Context, Command) (CommandResult, error) {
		return CommandResult{Stdout: []byte("pi: current\nnew format not understood\n")}, nil
	}
	finding := checkHerdrIntegrationTargets(t.Context(), h, Scope{}, Instance{ID: "i"})[0]
	if finding.Outcome != OutcomeUnknown {
		t.Fatalf("unknown status collapsed: %+v", finding)
	}
}

func TestHerdrPluginRegistryLocation(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	h.Env["XDG_CONFIG_HOME"] = filepath.Join(h.Home, "xdg")
	path, e := herdrPluginRegistryPath(h, Scope{})
	if e != nil || path != filepath.Join(h.Home, "xdg", "herdr", "plugins.json") {
		t.Fatal(path, e)
	}
	scope := Scope{Locations: map[string][]string{"herdr-config": {filepath.Join(h.Home, "custom", "config.toml")}}}
	path, e = herdrPluginRegistryPath(h, scope)
	if e != nil || path != filepath.Join(h.Home, "custom", "plugins.json") {
		t.Fatal(path, e)
	}
}

func TestHerdrPluginPartialResults(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	root := t.TempDir()
	registry := filepath.Join(root, "plugins.json")
	goodRoot := filepath.Join(root, "good")
	offlineRoot := filepath.Join(root, "offline")
	resourceFile(t, filepath.Join(goodRoot, ".git", "config"), "[core]\nrepositoryformatversion = 0\n")
	resourceFile(t, filepath.Join(offlineRoot, ".git", "config"), "[core]\nrepositoryformatversion = 0\n")
	makeExecutable(t, filepath.Join(h.Home, "git"))
	raw := `[{"plugin_id":"good","plugin_root":"` + goodRoot + `","enabled":true,"source":{"kind":"github","owner":"a","repo":"good","subdir":"plugins/good","requested_ref":"main","resolved_commit":"` + strings.Repeat("a", 40) + `","managed_path":"` + goodRoot + `"}},{"plugin_id":"offline","plugin_root":"` + offlineRoot + `","enabled":true,"source":{"kind":"github","owner":"b","repo":"offline","requested_ref":"main","resolved_commit":"` + strings.Repeat("b", 40) + `","managed_path":"` + offlineRoot + `"}}]`
	resourceFile(t, registry, raw)
	makeExecutable(t, filepath.Join(h.Home, "herdr"))
	h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
		if c.Env["GIT_OPTIONAL_LOCKS"] != "0" {
			t.Fatalf("unsafe git env: %+v", c)
		}
		if slices.Contains(c.Args, "rev-parse") {
			sha := strings.Repeat("a", 40)
			if strings.Contains(c.Dir, "offline") {
				sha = strings.Repeat("b", 40)
			}
			return CommandResult{Stdout: []byte(sha)}, nil
		}
		if !slices.Contains(c.Args, "--untracked-files=all") || !slices.Contains(c.Args, "--ignored=matching") {
			t.Fatalf("untracked status omitted: %+v", c)
		}
		return CommandResult{}, nil
	}
	h.Fetch = func(_ context.Context, address string) ([]byte, error) {
		if strings.Contains(address, "/offline/") {
			return nil, errors.New("offline")
		}
		if strings.Contains(address, "/compare/") {
			return []byte(`{"status":"ahead","ahead_by":2}`), nil
		}
		return []byte(`{"commit":{"sha":"` + strings.Repeat("c", 40) + `"}}`), nil
	}
	d := discoverHerdrPlugins(t.Context(), h, Scope{Locations: map[string][]string{"herdr-config": {filepath.Join(root, "config.toml")}}})
	if len(d.Instances) != 2 {
		t.Fatalf("discovery=%+v", d)
	}
	results := make([]Finding, 0, 2)
	for _, i := range d.Instances {
		results = append(results, checkHerdrPluginSources(t.Context(), h, Scope{}, i)...)
	}
	if len(results) != 2 || results[0].Outcome == results[1].Outcome || results[0].Key.InstanceID == results[1].Key.InstanceID {
		t.Fatalf("partial results=%+v", results)
	}
	var auto bool
	for _, f := range results {
		for _, a := range f.Actions {
			if a.Mode == ActionAutomatic {
				auto = true
				if len(a.Steps) != 1 || a.Steps[0].Command.Executable != filepath.Join(h.Home, "herdr") || !slices.Equal(a.Steps[0].Command.Args, []string{"plugin", "install", "a/good/plugins/good", "--ref", "main", "--yes"}) || ValidateProposal(a, DefaultLimits()) != nil {
					t.Fatalf("unsafe action=%+v", a)
				}
			}
		}
	}
	if !auto {
		t.Fatal("verified branch advancement lacks direct-argv proposal")
	}
}

func TestHerdrPluginDirtyCheckoutBlocksUpdate(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	root := t.TempDir()
	managed := filepath.Join(root, "managed")
	makeExecutable(t, filepath.Join(h.Home, "git"))
	resourceFile(t, filepath.Join(managed, ".git", "config"), "[core]\nrepositoryformatversion = 0\n")
	entry := herdrRegistryEntry{PluginID: "dirty", PluginRoot: managed, Source: herdrPluginSource{Kind: "github", Owner: "a", Repo: "b", RequestedRef: "main", ResolvedCommit: strings.Repeat("a", 40), ManagedPath: managed}}
	raw, _ := json.Marshal([]herdrRegistryEntry{entry})
	resourceFile(t, filepath.Join(root, "plugins.json"), string(raw))
	h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
		if slices.Contains(c.Args, "rev-parse") {
			return CommandResult{Stdout: []byte(strings.Repeat("a", 40))}, nil
		}
		return CommandResult{Stdout: []byte("?? user-change\\x00")}, nil
	}
	h.Fetch = func(context.Context, string) ([]byte, error) {
		t.Fatal("dirty checkout must not query or propose an update")
		return nil, nil
	}
	d := discoverHerdrPlugins(t.Context(), h, Scope{Locations: map[string][]string{"herdr-config": {filepath.Join(root, "config.toml")}}})
	f := checkHerdrPluginSources(t.Context(), h, Scope{}, d.Instances[0])[0]
	if f.Outcome != OutcomeAttention || len(f.Actions) != 0 {
		t.Fatalf("dirty checkout=%+v", f)
	}
}

func TestHerdrPluginLocalAndUnsupportedSources(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	root := t.TempDir()
	entries := []herdrRegistryEntry{{PluginID: "local", PluginRoot: filepath.Join(root, "local"), Source: herdrPluginSource{Kind: "local"}}, {PluginID: "unsupported", Source: herdrPluginSource{Kind: "other", Owner: "SECRET_OWNER"}}}
	raw, _ := json.Marshal(entries)
	resourceFile(t, filepath.Join(root, "plugins.json"), string(raw))
	d := discoverHerdrPlugins(t.Context(), h, Scope{Locations: map[string][]string{"herdr-config": {filepath.Join(root, "config.toml")}}})
	if d.Availability != AvailabilityUndetermined || len(d.Instances) != 2 {
		t.Fatalf("unsupported sibling hid coverage: %+v", d)
	}
	findings := make([]Finding, 0, 2)
	for _, i := range d.Instances {
		findings = append(findings, checkHerdrPluginSources(t.Context(), h, Scope{}, i)...)
	}
	var localNA, unsupportedUnknown bool
	for _, f := range findings {
		if f.Outcome == OutcomeNotApplicable {
			localNA = true
		}
		if f.Outcome == OutcomeUnknown {
			unsupportedUnknown = true
		}
		b, _ := json.Marshal(f)
		if strings.Contains(string(b), "SECRET_OWNER") {
			t.Fatal("invalid private source value escaped registry parsing")
		}
	}
	if !localNA || !unsupportedUnknown {
		t.Fatalf("findings=%+v", findings)
	}
}

func TestHerdrPluginDivergentRefsDoNotPropose(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	root := t.TempDir()
	managed := filepath.Join(root, "managed")
	makeExecutable(t, filepath.Join(h.Home, "git"))
	resourceFile(t, filepath.Join(managed, ".git", "config"), "[core]\nrepositoryformatversion = 0\n")
	entry := herdrRegistryEntry{PluginID: "diverged", PluginRoot: managed, Source: herdrPluginSource{Kind: "github", Owner: "a", Repo: "b", RequestedRef: "main", ResolvedCommit: strings.Repeat("a", 40), ManagedPath: managed}}
	raw, _ := json.Marshal([]herdrRegistryEntry{entry})
	resourceFile(t, filepath.Join(root, "plugins.json"), string(raw))
	h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
		if slices.Contains(c.Args, "rev-parse") {
			return CommandResult{Stdout: []byte(strings.Repeat("a", 40))}, nil
		}
		return CommandResult{}, nil
	}
	h.Fetch = func(_ context.Context, address string) ([]byte, error) {
		if strings.Contains(address, "/compare/") {
			return []byte(`{"status":"diverged","ahead_by":3}`), nil
		}
		return []byte(`{"commit":{"sha":"` + strings.Repeat("b", 40) + `"}}`), nil
	}
	d := discoverHerdrPlugins(t.Context(), h, Scope{Locations: map[string][]string{"herdr-config": {filepath.Join(root, "config.toml")}}})
	f := checkHerdrPluginSources(t.Context(), h, Scope{}, d.Instances[0])[0]
	if f.Outcome != OutcomeUnknown || len(f.Actions) != 0 {
		t.Fatalf("divergent plugin=%+v", f)
	}
}

func TestHerdrPluginPinnedSources(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	root := t.TempDir()
	p := herdrRegistryEntry{PluginID: "fixed", Enabled: true, Source: herdrPluginSource{Kind: "github", Owner: "a", Repo: "b", RequestedRef: strings.Repeat("c", 40), ResolvedCommit: strings.Repeat("c", 40)}}
	raw, e := json.Marshal([]herdrRegistryEntry{p})
	if e != nil {
		t.Fatal(e)
	}
	resourceFile(t, filepath.Join(root, "plugins.json"), string(raw))
	h.Fetch = func(context.Context, string) ([]byte, error) {
		t.Fatal("pinned plugin must not query remote")
		return nil, nil
	}
	d := discoverHerdrPlugins(t.Context(), h, Scope{Locations: map[string][]string{"herdr-config": {filepath.Join(root, "config.toml")}}})
	if len(d.Instances) != 1 {
		t.Fatal(d)
	}
	f := checkHerdrPluginSources(t.Context(), h, Scope{}, d.Instances[0])[0]
	if f.Outcome != OutcomeNotApplicable {
		t.Fatalf("finding=%+v", f)
	}
	assertNoAutomatic(t, f)
}

func TestHerdrPluginTagPin(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	root := t.TempDir()
	managed := filepath.Join(root, "managed")
	makeExecutable(t, filepath.Join(h.Home, "git"))
	resourceFile(t, filepath.Join(managed, ".git", "config"), "[core]\nrepositoryformatversion = 0\n")
	p := herdrRegistryEntry{PluginID: "tagged", PluginRoot: managed, Source: herdrPluginSource{Kind: "github", Owner: "a", Repo: "b", RequestedRef: "v1.2.3", ResolvedCommit: strings.Repeat("a", 40), ManagedPath: managed}}
	raw, _ := json.Marshal([]herdrRegistryEntry{p})
	resourceFile(t, filepath.Join(root, "plugins.json"), string(raw))
	h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
		if slices.Contains(c.Args, "rev-parse") {
			return CommandResult{Stdout: []byte(strings.Repeat("a", 40))}, nil
		}
		return CommandResult{}, nil
	}
	var calls atomic.Int32
	h.Fetch = func(_ context.Context, address string) ([]byte, error) {
		calls.Add(1)
		if strings.Contains(address, "/branches/") {
			return nil, errors.New("not a branch")
		}
		return []byte(`{"object":{"sha":"` + strings.Repeat("b", 40) + `"}}`), nil
	}
	d := discoverHerdrPlugins(t.Context(), h, Scope{Locations: map[string][]string{"herdr-config": {filepath.Join(root, "config.toml")}}})
	f := checkHerdrPluginSources(t.Context(), h, Scope{}, d.Instances[0])[0]
	if f.Outcome != OutcomeNotApplicable || calls.Load() != 2 {
		t.Fatalf("finding=%+v calls=%d", f, calls.Load())
	}
}

func TestHerdrPluginMalformedAndMissingRegistry(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	root := t.TempDir()
	config := filepath.Join(root, "config.toml")
	d := discoverHerdrPlugins(t.Context(), h, Scope{Locations: map[string][]string{"herdr-config": {config}}})
	if d.Availability != AvailabilityPresent || len(d.Instances) != 1 {
		t.Fatalf("missing registry must retain scope: %+v", d)
	}
	resourceFile(t, filepath.Join(root, "plugins.json"), "not-json")
	d = discoverHerdrPlugins(t.Context(), h, Scope{Locations: map[string][]string{"herdr-config": {config}}})
	if d.Availability != AvailabilityUndetermined || len(d.Instances) != 0 {
		t.Fatalf("invalid registry=%+v", d)
	}
}

func makeExecutable(t *testing.T, path string) {
	t.Helper()
	resourceFile(t, path, "synthetic; never execute")
	if e := os.Chmod(path, 0o700); e != nil {
		t.Fatal(e)
	}
}
