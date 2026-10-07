package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

var skillDescRe = regexp.MustCompile(`(?ms)^description:\s*(.+?)\s*$`)

func checkSkills(_ context.Context) Result {
	h, err := userHome()
	if err != nil {
		return unknown("skills", "-", "-", "cannot determine home directory")
	}
	root := filepath.Join(h, ".agents", "skills")
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return unknown("skills", "-", "-", root+" is missing")
	}
	total, bad := 0, []string{}
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}
		total++
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		head := string(raw)
		if len(head) > 6000 {
			head = head[:6000]
		}
		m := skillDescRe.FindStringSubmatch(head)
		if m == nil {
			return nil
		}
		desc := strings.Join(strings.Fields(strings.Trim(m[1], `"'`)), " ")
		if utf8.RuneCountInString(desc) > 1024 {
			rel := p
			if r, err := filepath.Rel(root, p); err == nil {
				rel = r
			}
			bad = append(bad, rel)
		}
		return nil
	})
	if walkErr != nil {
		return unknown("skills", "-", "-", "cannot scan skills directory")
	}
	if total == 0 {
		return unknown("skills", "-", "-", "no SKILL.md files were found")
	}
	if len(bad) == 0 {
		return ok("skills", fmt.Sprintf("%d skill", total), "100%<=1024", "within the agentskills.io limit")
	}
	show := bad
	if len(show) > 5 {
		show = show[:5]
	}
	return unknown("skills", fmt.Sprintf("%d skill", total), "-",
		fmt.Sprintf("%d descriptions over 1024 chars: %s", len(bad), strings.Join(show, ", ")))
}

type skillsChecker struct{}

func (c *skillsChecker) Name() string { return "skills" }

func (c *skillsChecker) Category() Category { return CategorySkill }

func (c *skillsChecker) Check(ctx context.Context) Result {
	return checkSkills(ctx)
}
