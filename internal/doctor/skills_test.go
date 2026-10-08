package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillFrontmatter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		want    SkillMetadata
		fixture string
		wantErr bool
	}{
		{name: "quoted unicode description", fixture: "quoted-skill", want: SkillMetadata{Name: "quoted-skill", Description: "Inspect quoted 東京 metadata safely"}},
		{name: "folded unicode description", fixture: "folded-skill", want: SkillMetadata{Name: "folded-skill", Description: "Inspect folded 東京 metadata"}},
		{name: "literal unicode description", fixture: "literal-skill", want: SkillMetadata{Name: "literal-skill", Description: "Inspect literal\n東京 metadata"}},
		{name: "duplicate yaml key", fixture: "duplicate-key", wantErr: true},
		{name: "missing description", raw: "---\nname: example-skill\n---\nbody\n", wantErr: true},
		{name: "missing name", raw: "---\ndescription: A skill\n---\nbody\n", wantErr: true},
		{name: "non-string description", raw: "---\nname: example-skill\ndescription: 42\n---\n", wantErr: true},
		{name: "optional metadata preserved by schema", raw: "---\nname: example-skill\ndescription: A valid description\nmetadata:\n  version: \"1.0\"\n---\n", want: SkillMetadata{Name: "example-skill", Description: "A valid description"}},
		{name: "body ignored", raw: "---\nname: example-skill\ndescription: A valid description\n---\n: invalid yaml [\n", want: SkillMetadata{Name: "example-skill", Description: "A valid description"}},
		{name: "no frontmatter", raw: "# Not a skill document\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := []byte(tt.raw)
			if tt.fixture != "" {
				var err error
				raw, err = os.ReadFile(filepath.Join("testdata", "resources", "skills", tt.fixture, "SKILL.md"))
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := ParseSkillMetadata(raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSkillMetadata() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("ParseSkillMetadata() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSkillFileSizeBound(t *testing.T) {
	t.Parallel()
	raw := make([]byte, int(DefaultLimits().MaxFileBytes)+1)
	if _, err := ParseSkillMetadata(raw); err == nil {
		t.Fatal("accepted a skill file larger than the inspection limit")
	}
}

func TestSkillDescriptionBoundary(t *testing.T) {
	t.Parallel()
	metadata := SkillMetadata{Name: "example-skill", Description: strings.Repeat("界", 1024)}
	if err := ValidateSkillMetadata(metadata); err != nil {
		t.Fatalf("accepted description at Unicode boundary: %v", err)
	}
	metadata.Description += "界"
	if err := ValidateSkillMetadata(metadata); err == nil {
		t.Fatal("accepted 1025 Unicode characters")
	}
}

func TestSkillConsumerRoots(t *testing.T) {
	t.Parallel()
	host := testHost(t)
	writeSkill(t, filepath.Join(host.Home, ".agents", "skills"), "user-skill", "User skill")
	project := t.TempDir()
	writeSkill(t, filepath.Join(project, ".agents", "skills"), "project-skill", "Project skill")
	discovery := discoverSkills(context.Background(), host, Scope{ProjectDir: project})
	if discovery.Availability != AvailabilityPresent || len(discovery.Instances) != 2 {
		t.Fatalf("supported user and selected-project roots not discovered: %+v", discovery)
	}
	explicit := t.TempDir()
	writeSkill(t, explicit, "explicit-skill", "Explicit skill")
	discovery = discoverSkills(context.Background(), host, Scope{ProjectDir: project, SkillRoots: []string{explicit}})
	if discovery.Availability != AvailabilityPresent || len(discovery.Instances) != 1 {
		t.Fatalf("explicit roots did not replace inferred roots: %+v", discovery)
	}
}

func TestSkillNameAndDirectoryRules(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "-leading", "trailing-", "two--hyphens", "Uppercase", strings.Repeat("a", 65), "東京"} {
		if err := ValidateSkillMetadata(SkillMetadata{Name: name, Description: "Valid description"}); err == nil {
			t.Errorf("accepted invalid skill name %q", name)
		}
	}
	host := testHost(t)
	root := t.TempDir()
	directory := filepath.Join(root, "directory-name")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "SKILL.md")
	content := "---\nname: different-name\ndescription: Valid description\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	discovery := discoverSkills(context.Background(), host, Scope{SkillRoots: []string{root}})
	if len(discovery.Instances) != 1 {
		t.Fatalf("discovery=%+v", discovery)
	}
	findings := checkSkillMetadata(context.Background(), host, Scope{}, discovery.Instances[0])
	if len(findings) != 1 || findings[0].Outcome != OutcomeAttention {
		t.Fatalf("skill name mismatch was not attention: %+v", findings)
	}
}

func TestSkillTraversalBounds(t *testing.T) {
	t.Parallel()
	host := testHost(t)
	siblingParent := t.TempDir()
	first := filepath.Join(siblingParent, "first")
	second := filepath.Join(siblingParent, "second")
	writeSkill(t, first, "example-one", "First skill")
	writeSkill(t, second, "example-two", "Second skill")
	if err := os.Symlink(first, filepath.Join(first, "cycle")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	writeSkill(t, outside, "outside-skill", "Must not be followed")
	if err := os.Symlink(outside, filepath.Join(first, "external")); err != nil {
		t.Fatal(err)
	}

	scope := Scope{SkillRoots: []string{first, second}}
	discovery := discoverSkills(context.Background(), host, scope)
	if discovery.Availability != AvailabilityUndetermined || len(discovery.Instances) != 2 || len(discovery.Diagnostics) == 0 {
		t.Fatalf("declared sibling roots or symlink policy not honored: availability=%s instances=%+v diagnostics=%v", discovery.Availability, discovery.Instances, discovery.Diagnostics)
	}
	seenDirectories := map[string]bool{}
	for _, instance := range discovery.Instances {
		for _, fact := range instance.Configuration {
			if fact.Label == "Skill directory relative path" {
				seenDirectories[fact.Value] = true
			}
		}
	}
	if !seenDirectories["example-one"] || !seenDirectories["example-two"] || seenDirectories[filepath.Join("external", "outside-skill")] {
		t.Fatalf("symlink traversal escaped a declared root: paths=%v outside=%s", seenDirectories, outside)
	}

	deepRoot := t.TempDir()
	deep := deepRoot
	for depth := 0; depth <= DefaultLimits().MaxDepth; depth++ {
		deep = filepath.Join(deep, "nested")
		if err := os.Mkdir(deep, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(t, deep, "too-deep", "Beyond the scan bound")
	bounded := discoverSkills(context.Background(), host, Scope{SkillRoots: []string{deepRoot}})
	if bounded.Availability != AvailabilityUndetermined || len(bounded.Diagnostics) == 0 {
		t.Fatalf("deep traversal did not report incomplete coverage: %+v", bounded)
	}
	limits := DefaultLimits()
	limits.MaxFiles = 1
	limited := discoverSkillsWithLimits(context.Background(), host, scope, limits)
	if limited.Availability != AvailabilityUndetermined || len(limited.Diagnostics) == 0 {
		t.Fatalf("file-count limit did not report incomplete coverage: %+v", limited)
	}
}

func TestSkillUnreadableFile(t *testing.T) {
	t.Parallel()
	host := testHost(t)
	root := t.TempDir()
	path := writeSkill(t, root, "example-skill", "A useful description")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("current test user can read mode-000 files")
	}
	discovery := discoverSkills(context.Background(), host, Scope{SkillRoots: []string{root}})
	if len(discovery.Instances) != 1 {
		t.Fatalf("skill instance lost after read failure: %+v", discovery)
	}
	findings := checkSkillMetadata(context.Background(), host, Scope{}, discovery.Instances[0])
	if len(findings) != 1 || findings[0].Outcome != OutcomeUnknown {
		t.Fatalf("unreadable skill metadata did not remain unknown: %+v", findings)
	}
}

func writeSkill(t *testing.T, root, name, description string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
