package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeclaredResourceScope(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	project := t.TempDir()
	resourceFile(t, filepath.Join(h.Home, ".pi", "agent", "settings.json"), `{"packages":["./local","npm:@example/tool@1.2.3","git:github.com/obra/superpowers@v1"]}`)
	resourceFile(t, filepath.Join(project, ".pi", "settings.json"), `{"packages":[{"source":"../project-local"}]}`)
	sources, e := DeclaredResources(t.Context(), h, Scope{ProjectDir: project})
	if e != nil || len(sources) != 4 {
		t.Fatal(sources, e)
	}
	if sources[0].Path != filepath.Join(h.Home, ".pi", "agent", "local") || sources[3].Path != filepath.Join(project, "project-local") || sources[0].Scope == sources[3].Scope {
		t.Fatal(sources)
	}
	if !sources[1].Pinned || !sources[2].Pinned {
		t.Fatal("pins lost")
	}
}

func TestDeclaredResourceRejectsTraversal(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"git:github.com/../secret", "git:github.com/owner/..", "npm:@../tool"} {
		t.Run(value, func(t *testing.T) {
			r := parseResourceSource(value, t.TempDir(), "user")
			if r.Kind != "unsupported" {
				t.Fatalf("unsafe source=%+v", r)
			}
		})
	}
}

func TestPiPackageSourceKinds(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	resourceFile(t, filepath.Join(h.Home, ".pi", "agent", "settings.json"), `{"packages":["./local","npm:example@1.2.3","git:github.com/example/extension@main","unsupported:tool"]}`)
	d := discoverPiPackages(t.Context(), h, Scope{})
	if len(d.Instances) != 4 {
		t.Fatal(d)
	}
	ids := map[string]bool{}
	for _, i := range d.Instances {
		if ids[i.ID] {
			t.Fatal("duplicate identity")
		}
		ids[i.ID] = true
		f := checkPiPackageSources(t.Context(), h, Scope{}, i)[0]
		assertNoAutomatic(t, f)
		if f.Outcome == OutcomeOK {
			t.Fatal("unknown source reported current")
		}
	}
}

func TestPiPackageAutomaticUpdateProposal(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	makeExecutable(t, filepath.Join(h.Home, "pi"))
	resourceFile(t, filepath.Join(h.Home, ".pi", "agent", "settings.json"), `{"packages":["npm:@foo/bar"]}`)
	resourceFile(t, filepath.Join(h.Home, ".pi", "agent", "npm", "node_modules", "@foo", "bar", "package.json"), `{"name":"@foo/bar","version":"1.0.0"}`)
	h.Fetch = func(context.Context, string) ([]byte, error) {
		return []byte(`{"version":"1.1.0"}`), nil
	}
	d := discoverPiPackages(t.Context(), h, Scope{})
	if len(d.Instances) != 1 {
		t.Fatalf("instances: %+v", d)
	}
	f := checkPiPackageSources(t.Context(), h, Scope{}, d.Instances[0])[0]
	if f.Outcome != OutcomeAttention {
		t.Fatalf("expected OutcomeAttention, got %+v", f)
	}
	if len(f.Actions) != 1 || f.Actions[0].Mode != ActionAutomatic {
		t.Fatalf("expected 1 automatic action, got %+v", f.Actions)
	}
	if err := ValidateProposal(f.Actions[0], DefaultLimits()); err != nil {
		t.Fatalf("invalid proposal: %v", err)
	}
}

func TestDeclaredResourceExplicitOverrides(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	root := t.TempDir()
	resourceFile(t, filepath.Join(h.Home, ".pi", "agent", "settings.json"), "invalid")
	d := discoverSuperpowers(t.Context(), h, Scope{Locations: map[string][]string{"superpowers": {root}}})
	if d.Availability != AvailabilityPresent || len(d.Instances) != 1 || d.Instances[0].Root.Value != root {
		t.Fatalf("explicit root lost: %+v", d)
	}
}

func TestDeclaredResourceWorktreeMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	common := t.TempDir()
	dir := filepath.Join(common, "worktrees", "example")
	resourceFile(t, filepath.Join(root, ".git"), "gitdir: "+dir+"\n")
	resourceFile(t, filepath.Join(dir, "commondir"), "../..\n")
	got, base, e := gitMetadataLocation(t.Context(), root)
	if e != nil || got != dir || base != common {
		t.Fatal(got, base, e)
	}
}

func TestDeclaredResourceRejectsExecutableGitConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, ".git")
	resourceFile(t, filepath.Join(dir, "config"), "[ filter \"custom\" ]\nclean = malicious\n")
	if gitInspectionSafe(t.Context(), root, dir, dir) {
		t.Fatal("configured filter accepted")
	}
}

func TestSuperpowersRequestedRef(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, ref, status string
		offline           bool
		want              Outcome
	}{{"pinned", "v1", "", false, OutcomeNotApplicable}, {"moving unknown ancestry", "main", "", false, OutcomeUnknown}, {"dirty", "main", "?? new-file\x00", false, OutcomeAttention}, {"offline", "main", "", true, OutcomeUnknown}, {"commit pin", strings.Repeat("a", 40), "", false, OutcomeNotApplicable}} {
		t.Run(tc.name, func(t *testing.T) {
			h := testHost(t)
			resourceFile(t, filepath.Join(h.Home, "git"), "synthetic; never execute")
			if e := os.Chmod(filepath.Join(h.Home, "git"), 0o700); e != nil {
				t.Fatal(e)
			}
			root := filepath.Join(h.Home, "repo")
			resourceFile(t, filepath.Join(root, ".git", "config"), "[core]\nrepositoryformatversion = 0\n")
			h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
				for _, a := range c.Args {
					if a == "fetch" || a == "pull" {
						t.Fatal("mutation")
					}
				}
				if value, present := c.Env["GIT_CONFIG_PARAMETERS"]; !present || value != "" {
					t.Fatal("inherited Git configuration parameters were not disabled")
				}
				if c.Env["GIT_OPTIONAL_LOCKS"] != "0" {
					t.Fatal("locks")
				}
				last := strings.Join(c.Args, " ")
				if strings.Contains(last, "rev-parse") {
					return CommandResult{Stdout: []byte(strings.Repeat("a", 40))}, nil
				}
				if strings.Contains(last, "status") {
					if !strings.Contains(last, "--untracked-files=all") {
						t.Fatal("untracked omitted")
					}
					return CommandResult{Stdout: []byte(tc.status)}, nil
				}
				if strings.Contains(last, "symbolic-ref") {
					return CommandResult{Stdout: []byte("main")}, nil
				}
				return CommandResult{ExitCode: 1}, errors.New("ancestry unavailable")
			}
			h.Fetch = func(context.Context, string) ([]byte, error) {
				if tc.offline {
					return nil, errors.New("offline")
				}
				return []byte(`{"sha":"` + strings.Repeat("b", 40) + `"}`), nil
			}
			source := ResourceSource{Kind: "git", Identity: "github.com/obra/superpowers", Path: root, RequestedRef: tc.ref, Scope: "user", Pinned: tc.ref != "main"}
			i := sourceInstance(h, "superpowers", source)
			f := checkSuperpowersSource(t.Context(), h, Scope{}, i)[0]
			if f.Outcome != tc.want {
				t.Fatalf("%+v", f)
			}
			assertNoAutomatic(t, f)
		})
	}
}

func TestCheckSuperpowersSourceWithGitTagAndRelease(t *testing.T) {
	t.Run("outdated release proposes update", func(t *testing.T) {
		h := testHost(t)
		resourceFile(t, filepath.Join(h.Home, "git"), "synthetic; never execute")
		if e := os.Chmod(filepath.Join(h.Home, "git"), 0o700); e != nil {
			t.Fatal(e)
		}
		resourceFile(t, filepath.Join(h.Home, "pi"), "synthetic pi")
		if e := os.Chmod(filepath.Join(h.Home, "pi"), 0o700); e != nil {
			t.Fatal(e)
		}
		root := filepath.Join(h.Home, "superpowers")
		resourceFile(t, filepath.Join(root, ".git", "config"), "[core]\nrepositoryformatversion = 0\n")

		h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
			last := strings.Join(c.Args, " ")
			if strings.Contains(last, "rev-parse") {
				return CommandResult{Stdout: []byte(strings.Repeat("a", 40))}, nil
			}
			if strings.Contains(last, "symbolic-ref") {
				return CommandResult{Stdout: []byte("main")}, nil
			}
			if strings.Contains(last, "status") {
				return CommandResult{Stdout: []byte("")}, nil
			}
			if strings.Contains(last, "describe") {
				return CommandResult{Stdout: []byte("v6.4.2")}, nil
			}
			return CommandResult{ExitCode: 1}, errors.New("unsupported")
		}

		h.Fetch = func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, "releases/latest") {
				return []byte(`{"tag_name":"v7.0.0","html_url":"https://github.com/obra/superpowers/releases/tag/v7.0.0"}`), nil
			}
			return nil, errors.New("not found")
		}

		source := ResourceSource{Kind: "git", Identity: "github.com/obra/superpowers", Path: root, Manager: "pi", Scope: "user"}
		i := sourceInstance(h, "superpowers", source)
		findings := checkSuperpowersSource(t.Context(), h, Scope{}, i)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		f := findings[0]
		if f.Outcome != OutcomeAttention {
			t.Fatalf("expected OutcomeAttention, got %v: %s", f.Outcome, f.Explanation)
		}
		if len(f.Actions) != 1 {
			t.Fatalf("expected 1 action proposal, got %d", len(f.Actions))
		}
		if f.Actions[0].Mode != ActionAutomatic {
			t.Fatalf("expected ActionAutomatic, got %v", f.Actions[0].Mode)
		}
		if !strings.Contains(f.Actions[0].Steps[0].Command.Args[1], "git:github.com/obra/superpowers") {
			t.Fatalf("unexpected step command args: %v", f.Actions[0].Steps[0].Command.Args)
		}
	})

	t.Run("current release matches ok", func(t *testing.T) {
		h := testHost(t)
		resourceFile(t, filepath.Join(h.Home, "git"), "synthetic; never execute")
		if e := os.Chmod(filepath.Join(h.Home, "git"), 0o700); e != nil {
			t.Fatal(e)
		}
		root := filepath.Join(h.Home, "superpowers")
		resourceFile(t, filepath.Join(root, ".git", "config"), "[core]\nrepositoryformatversion = 0\n")

		h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
			last := strings.Join(c.Args, " ")
			if strings.Contains(last, "rev-parse") {
				return CommandResult{Stdout: []byte(strings.Repeat("a", 40))}, nil
			}
			if strings.Contains(last, "symbolic-ref") {
				return CommandResult{Stdout: []byte("main")}, nil
			}
			if strings.Contains(last, "status") {
				return CommandResult{Stdout: []byte("")}, nil
			}
			if strings.Contains(last, "describe") {
				return CommandResult{Stdout: []byte("v7.0.0")}, nil
			}
			return CommandResult{ExitCode: 1}, errors.New("unsupported")
		}

		h.Fetch = func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, "releases/latest") {
				return []byte(`{"tag_name":"v7.0.0","html_url":"https://github.com/obra/superpowers/releases/tag/v7.0.0"}`), nil
			}
			return nil, errors.New("not found")
		}

		source := ResourceSource{Kind: "git", Identity: "github.com/obra/superpowers", Path: root, Manager: "pi", Scope: "user"}
		i := sourceInstance(h, "superpowers", source)
		findings := checkSuperpowersSource(t.Context(), h, Scope{}, i)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		f := findings[0]
		if f.Outcome != OutcomeOK {
			t.Fatalf("expected OutcomeOK, got %v: %s", f.Outcome, f.Explanation)
		}
		if len(f.Actions) != 0 {
			t.Fatalf("expected 0 actions for up to date release, got %d", len(f.Actions))
		}
	})
}

func TestGitUpdateUsesUpdateNotInstall(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	resourceFile(t, filepath.Join(h.Home, "git"), "synthetic; never execute")
	if e := os.Chmod(filepath.Join(h.Home, "git"), 0o700); e != nil {
		t.Fatal(e)
	}
	resourceFile(t, filepath.Join(h.Home, "pi"), "synthetic pi")
	if e := os.Chmod(filepath.Join(h.Home, "pi"), 0o700); e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(h.Home, "superpowers")
	resourceFile(t, filepath.Join(root, ".git", "config"), "[core]\nrepositoryformatversion = 0\n")

	h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
		last := strings.Join(c.Args, " ")
		if strings.Contains(last, "rev-parse") {
			return CommandResult{Stdout: []byte(strings.Repeat("a", 40))}, nil
		}
		if strings.Contains(last, "symbolic-ref") {
			return CommandResult{Stdout: []byte("main")}, nil
		}
		if strings.Contains(last, "status") {
			return CommandResult{Stdout: []byte("")}, nil
		}
		if strings.Contains(last, "describe") {
			return CommandResult{Stdout: []byte("v6.4.2")}, nil
		}
		return CommandResult{ExitCode: 1}, errors.New("unsupported")
	}

	h.Fetch = func(_ context.Context, url string) ([]byte, error) {
		if strings.Contains(url, "releases/latest") {
			return []byte(`{"tag_name":"v7.0.0","html_url":"https://github.com/obra/superpowers/releases/tag/v7.0.0"}`), nil
		}
		return nil, errors.New("not found")
	}

	source := ResourceSource{Kind: "git", Identity: "github.com/obra/superpowers", Path: root, Manager: "pi", Scope: "user"}
	i := sourceInstance(h, "superpowers", source)
	findings := checkSuperpowersSource(t.Context(), h, Scope{}, i)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if len(f.Actions) != 1 {
		t.Fatalf("expected 1 action proposal, got %d", len(f.Actions))
	}
	if f.Actions[0].Steps[0].Command.Args[0] != "update" {
		t.Fatalf("expected 'update' command, got %q", f.Actions[0].Steps[0].Command.Args[0])
	}
	if !strings.Contains(f.Actions[0].Steps[0].Command.Args[1], "git:github.com/obra/superpowers") {
		t.Fatalf("expected git identity in args, got %v", f.Actions[0].Steps[0].Command.Args)
	}
}

func TestNpmUpdateUsesUpdateNotInstall(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	makeExecutable(t, filepath.Join(h.Home, "pi"))
	resourceFile(t, filepath.Join(h.Home, ".pi", "agent", "settings.json"), `{"packages":["npm:@foo/bar"]}`)
	resourceFile(t, filepath.Join(h.Home, ".pi", "agent", "npm", "node_modules", "@foo", "bar", "package.json"), `{"name":"@foo/bar","version":"1.0.0"}`)
	h.Fetch = func(context.Context, string) ([]byte, error) {
		return []byte(`{"version":"1.1.0"}`), nil
	}
	d := discoverPiPackages(t.Context(), h, Scope{})
	if len(d.Instances) != 1 {
		t.Fatalf("instances: %+v", d)
	}
	f := checkPiPackageSources(t.Context(), h, Scope{}, d.Instances[0])[0]
	if len(f.Actions) != 1 {
		t.Fatalf("expected 1 action proposal, got %d", len(f.Actions))
	}
	if f.Actions[0].Steps[0].Command.Args[0] != "update" {
		t.Fatalf("expected 'update' command, got %q", f.Actions[0].Steps[0].Command.Args[0])
	}
}

func TestGitUpdateFallsBackToInstallWhenPathMissing(t *testing.T) {
	t.Parallel()
	h := testHost(t)
	resourceFile(t, filepath.Join(h.Home, "git"), "synthetic; never execute")
	if e := os.Chmod(filepath.Join(h.Home, "git"), 0o700); e != nil {
		t.Fatal(e)
	}
	resourceFile(t, filepath.Join(h.Home, "pi"), "synthetic pi")
	if e := os.Chmod(filepath.Join(h.Home, "pi"), 0o700); e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(h.Home, "superpowers")
	resourceFile(t, filepath.Join(root, ".git", "config"), "[core]\nrepositoryformatversion = 0\n")

	h.RunRead = func(_ context.Context, c Command) (CommandResult, error) {
		last := strings.Join(c.Args, " ")
		if strings.Contains(last, "rev-parse") {
			return CommandResult{Stdout: []byte(strings.Repeat("a", 40))}, nil
		}
		if strings.Contains(last, "symbolic-ref") {
			return CommandResult{Stdout: []byte("main")}, nil
		}
		if strings.Contains(last, "status") {
			return CommandResult{Stdout: []byte("")}, nil
		}
		if strings.Contains(last, "describe") {
			return CommandResult{Stdout: []byte("v6.4.2")}, nil
		}
		return CommandResult{ExitCode: 1}, errors.New("unsupported")
	}

	h.Fetch = func(_ context.Context, url string) ([]byte, error) {
		if strings.Contains(url, "releases/latest") {
			_ = os.RemoveAll(root)
			return []byte(`{"tag_name":"v7.0.0","html_url":"https://github.com/obra/superpowers/releases/tag/v7.0.0"}`), nil
		}
		return nil, errors.New("not found")
	}

	source := ResourceSource{Kind: "git", Identity: "github.com/obra/superpowers", Path: root, Manager: "pi", Scope: "user"}
	i := sourceInstance(h, "superpowers", source)
	findings := checkSuperpowersSource(t.Context(), h, Scope{}, i)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if len(f.Actions) != 1 {
		t.Fatalf("expected 1 action proposal, got %d", len(f.Actions))
	}
	if f.Actions[0].Steps[0].Command.Args[0] != "install" {
		t.Fatalf("expected 'install' command, got %q", f.Actions[0].Steps[0].Command.Args[0])
	}
}
