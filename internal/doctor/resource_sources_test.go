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
	resourceFile(t, filepath.Join(h.Home, ".pi", "agent", "settings.json"), `{"packages":["./local","npm:example@1.2.3","git:github.com/obra/superpowers@main","unsupported:tool"]}`)
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
