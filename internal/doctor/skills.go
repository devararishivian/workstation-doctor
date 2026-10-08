package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
)

// SkillMetadata is the allowlisted Agent Skills frontmatter used by inspection.
type SkillMetadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ParseSkillMetadata parses only the bounded YAML frontmatter of a SKILL.md file.
func ParseSkillMetadata(raw []byte) (SkillMetadata, error) {
	if int64(len(raw)) > DefaultLimits().MaxFileBytes {
		return SkillMetadata{}, errors.New("skill file exceeds inspection limit")
	}
	text := string(raw)
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSuffix(lines[0], "\r") != "---" {
		return SkillMetadata{}, errors.New("skill frontmatter is missing")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		if line == "---" || line == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return SkillMetadata{}, errors.New("skill frontmatter is unterminated")
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &fields); err != nil || fields == nil {
		return SkillMetadata{}, errors.New("skill frontmatter must be a YAML mapping")
	}
	name, nameIsString := fields["name"].(string)
	description, descriptionIsString := fields["description"].(string)
	if !nameIsString || !descriptionIsString {
		return SkillMetadata{}, errors.New("required skill frontmatter fields must be strings")
	}
	metadata := SkillMetadata{Name: name, Description: description}
	if err := ValidateSkillMetadata(metadata); err != nil {
		return SkillMetadata{}, err
	}
	return metadata, nil
}

// ValidateSkillMetadata applies the Agent Skills required-field constraints.
func ValidateSkillMetadata(metadata SkillMetadata) error {
	if !validSkillName(metadata.Name) {
		return errors.New("skill name is invalid")
	}
	if !utf8.ValidString(metadata.Description) || strings.TrimSpace(metadata.Description) == "" || utf8.RuneCountInString(metadata.Description) > 1024 {
		return errors.New("skill description must contain 1 to 1024 Unicode characters")
	}
	return nil
}

func validSkillName(name string) bool {
	if len(name) == 0 || len(name) > 64 || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

type skillScanRoot struct {
	path, scope string
	explicit    bool
}

func discoverSkills(ctx context.Context, host *Host, scope Scope) Discovery {
	return discoverSkillsWithLimits(ctx, host, scope, DefaultLimits())
}

func discoverSkillsWithLimits(ctx context.Context, host *Host, scope Scope, limits Limits) Discovery {
	discovery := Discovery{Availability: AvailabilityAbsent}
	if err := ctx.Err(); err != nil {
		return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Skill discovery was canceled."}}
	}
	if host == nil || !filepath.IsAbs(host.Home) || limits.MaxFiles <= 0 || limits.MaxDepth <= 0 {
		return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Skill discovery scope or limits are invalid."}}
	}
	roots, explicit, err := skillRoots(host, scope)
	if err != nil {
		return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Skill roots could not be established."}}
	}
	if len(roots) == 0 {
		return discovery
	}
	incomplete := false
	visitedFiles := map[string]bool{}
	var fileInfos []os.FileInfo
	entries := 0
	for _, candidate := range roots {
		if err := ctx.Err(); err != nil {
			incomplete = true
			discovery.Diagnostics = append(discovery.Diagnostics, "Skill discovery was canceled; coverage is incomplete.")
			break
		}
		absolute, err := filepath.Abs(candidate.path)
		if err != nil || !filepath.IsAbs(absolute) || !validText(absolute, limits.MaxPathBytes) {
			incomplete = true
			discovery.Diagnostics = append(discovery.Diagnostics, "A skill root path is invalid.")
			continue
		}
		root, err := os.OpenRoot(absolute)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && !candidate.explicit {
				continue
			}
			incomplete = true
			discovery.Diagnostics = append(discovery.Diagnostics, "A skill root could not be opened.")
			continue
		}
		info, statErr := root.Stat(".")
		if statErr != nil || !info.IsDir() {
			if closeErr := root.Close(); closeErr != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, "A skill root could not be closed cleanly.")
			}
			incomplete = true
			discovery.Diagnostics = append(discovery.Diagnostics, "A skill root is not a readable directory.")
			continue
		}
		dirs := []os.FileInfo{info}
		var walk func(string, int) bool
		walk = func(rel string, depth int) bool {
			if err := ctx.Err(); err != nil {
				return false
			}
			if depth > limits.MaxDepth {
				discovery.Diagnostics = append(discovery.Diagnostics, "Skill directory depth limit reached; coverage is incomplete.")
				return false
			}
			directory, err := root.Open(rel)
			if err != nil {
				incomplete = true
				discovery.Diagnostics = append(discovery.Diagnostics, "A skill directory could not be read.")
				return true
			}
			children, readErr := directory.ReadDir(-1)
			closeErr := directory.Close()
			if readErr != nil || closeErr != nil {
				incomplete = true
				discovery.Diagnostics = append(discovery.Diagnostics, "A skill directory could not be read completely.")
			}
			for _, child := range children {
				entries++
				if entries > limits.MaxFiles {
					discovery.Diagnostics = append(discovery.Diagnostics, "Skill file limit reached; coverage is incomplete.")
					return false
				}
				childRel := child.Name()
				if rel != "." {
					childRel = filepath.Join(rel, child.Name())
				}
				if !validText(childRel, limits.MaxPathBytes) {
					incomplete = true
					discovery.Diagnostics = append(discovery.Diagnostics, "A skill path exceeds the inspection limit.")
					continue
				}
				childInfo, statErr := root.Stat(childRel)
				if statErr != nil {
					incomplete = true
					discovery.Diagnostics = append(discovery.Diagnostics, "A linked skill path could not be resolved within its declared root.")
					continue
				}
				if childInfo.IsDir() {
					alreadyVisited := false
					for _, prior := range dirs {
						if os.SameFile(childInfo, prior) {
							alreadyVisited = true
							break
						}
					}
					if alreadyVisited {
						continue
					}
					dirs = append(dirs, childInfo)
					if !walk(childRel, depth+1) {
						return false
					}
					continue
				}
				if child.Name() != "SKILL.md" || !childInfo.Mode().IsRegular() {
					continue
				}
				filePath := filepath.Join(absolute, childRel)
				key := filepath.Clean(filePath)
				duplicateFile := visitedFiles[key]
				if !duplicateFile {
					for _, prior := range fileInfos {
						if os.SameFile(childInfo, prior) {
							duplicateFile = true
							break
						}
					}
				}
				if duplicateFile {
					continue
				}
				visitedFiles[key] = true
				fileInfos = append(fileInfos, childInfo)
				id := skillInstanceID(candidate.scope, filePath)
				discovery.Instances = append(discovery.Instances, Instance{ID: id, IntegrationID: "skills", Scope: candidate.scope, Availability: AvailabilityPresent, DiscoverySource: "declared skill root", ObservedAt: hostNow(host), Root: Fact{State: EvidenceKnown, Label: "Declared skill root", Value: absolute, Source: "declared root traversal", ObservedAt: hostNow(host)}, Configuration: []Fact{{State: EvidenceKnown, Label: "Skill directory relative path", Value: filepath.Dir(childRel), Source: "declared root traversal"}}})
			}
			return true
		}
		if !walk(".", 0) {
			incomplete = true
		}
		if err := root.Close(); err != nil {
			incomplete = true
			discovery.Diagnostics = append(discovery.Diagnostics, "A skill root could not be closed cleanly.")
		}
		if entries > limits.MaxFiles {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		incomplete = true
		discovery.Diagnostics = append(discovery.Diagnostics, "Skill discovery was canceled; coverage is incomplete.")
	}
	switch {
	case incomplete:
		discovery.Availability = AvailabilityUndetermined
	case len(discovery.Instances) > 0, explicit:
		discovery.Availability = AvailabilityPresent
	}
	return discovery
}

func skillRoots(host *Host, scope Scope) ([]skillScanRoot, bool, error) {
	if paths, ok := scope.Locations["skills"]; ok {
		if len(paths) == 0 || len(paths) > DefaultLimits().MaxFiles {
			return nil, true, errors.New("explicit skill roots are invalid")
		}
		roots := make([]skillScanRoot, 0, len(paths))
		for _, path := range paths {
			if !filepath.IsAbs(path) {
				return nil, true, errors.New("explicit skill root is not absolute")
			}
			roots = append(roots, skillScanRoot{path: path, scope: "selected", explicit: true})
		}
		return roots, true, nil
	}
	if len(scope.SkillRoots) > 0 {
		if len(scope.SkillRoots) > DefaultLimits().MaxFiles {
			return nil, true, errors.New("explicit skill roots exceed limit")
		}
		roots := make([]skillScanRoot, 0, len(scope.SkillRoots))
		for _, path := range scope.SkillRoots {
			if !filepath.IsAbs(path) {
				return nil, true, errors.New("explicit skill root is not absolute")
			}
			roots = append(roots, skillScanRoot{path: path, scope: "selected", explicit: true})
		}
		return roots, true, nil
	}
	roots := []skillScanRoot{{path: filepath.Join(host.Home, ".agents", "skills"), scope: "user"}}
	if scope.ProjectDir != "" {
		if !filepath.IsAbs(scope.ProjectDir) {
			return nil, false, errors.New("selected project path is not absolute")
		}
		roots = append(roots, skillScanRoot{path: filepath.Join(scope.ProjectDir, ".agents", "skills"), scope: "project"})
	}
	return roots, false, nil
}

func skillInstanceID(scope, path string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + filepath.Clean(path)))
	return "skills:" + hex.EncodeToString(sum[:])
}

func checkSkillMetadata(ctx context.Context, _ *Host, _ Scope, instance Instance) []Finding {
	finding := Finding{Key: FindingKey{IntegrationID: "skills", CheckID: "skills", InstanceID: instance.ID}, Outcome: OutcomeUnknown, Question: "Does each discovered skill have valid metadata?", Explanation: "Skill metadata could not be read safely."}
	if err := ctx.Err(); err != nil {
		finding.Outcome = OutcomeCanceled
		finding.Explanation = "Skill inspection was canceled."
		return []Finding{finding}
	}
	if !filepath.IsAbs(instance.Root.Value) {
		return []Finding{finding}
	}
	relative := ""
	for _, fact := range instance.Configuration {
		if fact.Label == "Skill directory relative path" {
			relative = fact.Value
			break
		}
	}
	if relative == "" || !filepath.IsLocal(relative) {
		return []Finding{finding}
	}
	root, err := os.OpenRoot(instance.Root.Value)
	if err != nil {
		return []Finding{finding}
	}
	raw, readErr := readSkillFile(ctx, root, filepath.Join(relative, "SKILL.md"), DefaultLimits().MaxFileBytes)
	closeErr := root.Close()
	if readErr != nil || closeErr != nil {
		if ctx.Err() != nil {
			finding.Outcome = OutcomeCanceled
			finding.Explanation = "Skill inspection was canceled."
		}
		return []Finding{finding}
	}
	metadata, err := ParseSkillMetadata(raw)
	if err != nil || metadata.Name != filepath.Base(relative) {
		finding.Outcome = OutcomeAttention
		finding.Explanation = "Skill frontmatter is invalid or its name does not match its directory."
		return []Finding{finding}
	}
	finding.Outcome = OutcomeOK
	finding.Explanation = "Required skill metadata is valid and the name matches its directory."
	finding.Evidence = []Fact{{State: EvidenceKnown, Label: "Skill name", Value: metadata.Name, Source: "bounded SKILL.md frontmatter"}, {State: EvidenceKnown, Label: "Description length", Value: fmt.Sprintf("%d Unicode characters", utf8.RuneCountInString(metadata.Description)), Source: "bounded SKILL.md frontmatter"}}
	return []Finding{finding}
}

func readSkillFile(ctx context.Context, root *os.Root, path string, limit int64) (raw []byte, err error) {
	file, err := root.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open skill metadata: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			wrapped := fmt.Errorf("close skill metadata: %w", closeErr)
			if err != nil {
				err = errors.Join(err, wrapped)
			} else {
				err = wrapped
				raw = nil
			}
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat skill metadata: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > limit || limit <= 0 {
		return nil, errors.New("skill file is not a bounded regular file")
	}
	reader := io.LimitReader(file, limit+1)
	buffer := make([]byte, 32<<10)
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("read skill metadata: %w", err)
		}
		n, readErr := reader.Read(buffer)
		raw = append(raw, buffer[:n]...)
		if int64(len(raw)) > limit {
			return nil, errors.New("skill file exceeds inspection limit")
		}
		if errors.Is(readErr, io.EOF) {
			return raw, nil
		}
		if readErr != nil {
			return nil, fmt.Errorf("read skill metadata: %w", readErr)
		}
	}
}
