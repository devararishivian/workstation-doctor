// Package doctor runs read-only checks on workstation components
// (Pi, Herdr, Ghostty, npm/uv packages, configuration, skills)
// and reports installed versions against latest versions.
package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Status of one component.
const (
	StatusOK      = "OK"
	StatusUpdate  = "UPDATE"
	StatusUnknown = "UNKNOWN"
)

// Result is the check result of one component.
type Result struct {
	Component string
	Installed string
	Latest    string
	Status    string
	Note      string
	// Manual is a manual step ready to copy and paste (only for UPDATE).
	Manual string
	// Fix is the command that automatic mode runs (only for UPDATE).
	Fix string
}

// Summary counts each status.
type Summary struct {
	OK, Update, Unknown int
}

// Summarize counts the results by status.
func Summarize(results []Result) Summary {
	var s Summary
	for _, r := range results {
		switch r.Status {
		case StatusOK:
			s.OK++
		case StatusUpdate:
			s.Update++
		default:
			s.Unknown++
		}
	}
	return s
}

// Pending returns results with status UPDATE (action is pending).
func Pending(results []Result) []Result {
	out := []Result{}
	for _, r := range results {
		if r.Status == StatusUpdate {
			out = append(out, r)
		}
	}
	return out
}

// userHome returns the user home directory or an error when the
// operating system cannot determine it.
func userHome() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return h, nil
}

// execOut runs a command with a timeout and returns trimmed stdout.
// A non-zero exit that still produces stdout is usable output
// (for example `npm outdated` exits with 1 when packages are outdated).
// An execution failure without usable stdout returns an error.
func execOut(ctx context.Context, timeout time.Duration, dir, name string, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(c, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && trimmed != "" {
			return trimmed, nil
		}
		return "", fmt.Errorf("cannot run %s: %w", commandLine(name, args), err)
	}
	return trimmed, nil
}

func commandLine(name string, args []string) string {
	return strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Run executes all checks in order and returns the results.
func Run(ctx context.Context) []Result {
	checks := []func(context.Context) Result{
		checkPi,
		checkHerdr,
		checkGhostty,
		func(c context.Context) Result { return checkNpmPkg(c, "opencode", "opencode-ai") },
		func(c context.Context) Result { return checkNpmPkg(c, "tokenjuice", "tokenjuice") },
		checkSerena,
		checkGortex,
		checkBrewOutdated,
		checkNpmOutdatedGlobal,
		checkPiPackages,
		checkSuperpowers,
		checkHerdrIntegrations,
		checkGhosttyConfig,
		checkPiConfig,
		checkSkills,
	}
	results := make([]Result, 0, len(checks))
	for _, c := range checks {
		results = append(results, c(ctx))
	}
	return results
}

// PrintTable prints the results as a text table.
func PrintTable(w io.Writer, results []Result) {
	fmt.Fprintf(w, "\n%-22s %-16s %-16s %-10s %s\n", "COMPONENT", "INSTALLED", "LATEST", "STATUS", "NOTE")
	fmt.Fprintln(w, "----------------------------------------------------------------------------------------------")
	for _, r := range results {
		fmt.Fprintf(w, "%-22s %-16s %-16s %-10s %s\n",
			trunc(r.Component, 22), trunc(r.Installed, 16), trunc(r.Latest, 16), r.Status, r.Note)
	}
	s := Summarize(results)
	fmt.Fprintf(w, "\nSummary: %d OK · %d need update · %d unknown\n", s.OK, s.Update, s.Unknown)
}

func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// PrintManual prints the ordered manual steps for UPDATE results.
func PrintManual(w io.Writer, results []Result) {
	pending := Pending(results)
	fmt.Fprintln(w, "\n== Ordered manual steps ==")
	if len(pending) == 0 {
		fmt.Fprintln(w, "No action is needed. All components are current.")
		return
	}
	for i, r := range pending {
		fmt.Fprintf(w, "%d. %s\n", i+1, r.Manual)
	}
}

func ok(component, installed, latest, note string) Result {
	return Result{Component: component, Installed: installed, Latest: latest, Status: StatusOK, Note: note}
}

func needUpdate(component, installed, latest, manual, fix, note string) Result {
	return Result{
		Component: component, Installed: installed, Latest: latest,
		Status: StatusUpdate, Note: note, Manual: manual, Fix: fix,
	}
}

func unknown(component, installed, latest, note string) Result {
	return Result{Component: component, Installed: installed, Latest: latest, Status: StatusUnknown, Note: note}
}

func npmLatest(ctx context.Context, pkg string) (string, error) {
	return execOut(ctx, 30*time.Second, "", "npm", "view", pkg, "version")
}

func npmGlobalInstalled(ctx context.Context, pkg string) (string, error) {
	out, err := execOut(ctx, 30*time.Second, "", "npm", "list", "-g", "--depth=0", "--json")
	if err != nil {
		return "", err
	}
	var v struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return "", fmt.Errorf("cannot parse npm list output: %w", err)
	}
	return v.Dependencies[pkg].Version, nil
}

func checkPi(ctx context.Context) Result {
	if !commandExists("pi") {
		return unknown("pi", "-", "-", "pi binary is not in PATH")
	}
	inst, err := execOut(ctx, 15*time.Second, "", "pi", "--version")
	if err != nil || inst == "" {
		return unknown("pi", "-", "-", "cannot read pi --version")
	}
	latest, err := npmLatest(ctx, "@earendil-works/pi-coding-agent")
	if err != nil || latest == "" {
		return unknown("pi", inst, "-", "cannot read npm registry (offline?)")
	}
	if inst == latest {
		return ok("pi", inst, latest, "npm")
	}
	pkg := "@earendil-works/pi-coding-agent@" + latest
	cmd := "npm i -g " + pkg
	return needUpdate("pi", inst, latest, cmd, cmd, "npm package is outdated")
}

func checkHerdr(ctx context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr", "-", "-", "herdr binary is not in PATH")
	}
	out, err := execOut(ctx, 15*time.Second, "", "herdr", "--version")
	if err != nil {
		return unknown("herdr", "-", "-", "cannot read herdr --version")
	}
	inst := ""
	if f := strings.Fields(out); len(f) >= 2 {
		inst = f[1]
	}
	if inst == "" {
		return unknown("herdr", "-", "-", "cannot read herdr --version")
	}
	info, err := execOut(ctx, 60*time.Second, "", "brew", "info", "--json=v2", "herdr")
	if err != nil {
		return unknown("herdr", inst, "-", "cannot read brew info (offline?)")
	}
	var v struct {
		Formulae []struct {
			Versions struct {
				Stable string `json:"stable"`
			} `json:"versions"`
		} `json:"formulae"`
	}
	latest := ""
	if json.Unmarshal([]byte(info), &v) == nil && len(v.Formulae) > 0 {
		latest = v.Formulae[0].Versions.Stable
	}
	if latest == "" {
		return unknown("herdr", inst, "-", "cannot read brew info (offline?)")
	}
	if inst == latest {
		return ok("herdr", inst, latest, "brew")
	}
	return needUpdate("herdr", inst, latest, "brew upgrade herdr", "brew upgrade herdr", "brew formula is outdated")
}

const ghosttyBin = "/Applications/Ghostty.app/Contents/MacOS/ghostty"

func checkGhostty(ctx context.Context) Result {
	if _, err := os.Stat(ghosttyBin); err != nil {
		return unknown("ghostty", "-", "-", "Ghostty.app was not found")
	}
	out, err := execOut(ctx, 15*time.Second, "", ghosttyBin, "--version")
	if err != nil {
		return unknown("ghostty", "-", "-", "cannot read ghostty --version")
	}
	inst := ""
	if line, _, _ := strings.Cut(out, "\n"); line != "" {
		if f := strings.Fields(line); len(f) >= 2 {
			inst = f[len(f)-1]
		}
	}
	if inst == "" {
		return unknown("ghostty", "-", "-", "cannot read ghostty --version")
	}
	info, err := execOut(ctx, 60*time.Second, "", "brew", "info", "--cask", "--json=v2", "ghostty")
	if err != nil {
		return unknown("ghostty", inst, "-", "cannot read brew cask info (offline?)")
	}
	var v struct {
		Casks []struct {
			Version string `json:"version"`
		} `json:"casks"`
	}
	latest := ""
	if json.Unmarshal([]byte(info), &v) == nil && len(v.Casks) > 0 {
		latest, _, _ = strings.Cut(v.Casks[0].Version, ",")
	}
	if latest == "" {
		return unknown("ghostty", inst, "-", "cannot read brew cask info (offline?)")
	}
	if inst == latest {
		return ok("ghostty", inst, latest, "brew cask")
	}
	return needUpdate("ghostty", inst, latest,
		"brew upgrade --cask ghostty", "brew upgrade --cask ghostty", "cask is outdated")
}

func checkNpmPkg(ctx context.Context, label, pkg string) Result {
	if !commandExists("npm") {
		return unknown(label, "-", "-", "npm is not in PATH")
	}
	inst, err := npmGlobalInstalled(ctx, pkg)
	if err != nil {
		return unknown(label, "-", "-", "cannot run npm list")
	}
	if inst == "" {
		return unknown(label, "-", "-", "global npm package was not found")
	}
	latest, err := npmLatest(ctx, pkg)
	if err != nil || latest == "" {
		return unknown(label, inst, "-", "cannot read latest version (offline?)")
	}
	if inst == latest {
		return ok(label, inst, latest, "npm global")
	}
	cmd := fmt.Sprintf("npm i -g %s@%s", pkg, latest)
	return needUpdate(label, inst, latest, cmd, cmd, "global npm package is outdated")
}

func checkSerena(ctx context.Context) Result {
	if !commandExists("serena") {
		return unknown("serena", "-", "-", "serena binary is not in PATH")
	}
	out, err := execOut(ctx, 15*time.Second, "", "serena", "--version")
	if err != nil {
		return unknown("serena", "-", "-", "cannot read serena --version")
	}
	inst := ""
	if f := strings.Fields(out); len(f) >= 2 {
		inst = f[1]
	}
	if inst == "" {
		return unknown("serena", "-", "-", "cannot read serena --version")
	}
	latest, err := pypiLatest(ctx, "serena-agent")
	if err != nil || latest == "" {
		return unknown("serena", inst, "-", "cannot read PyPI (offline?)")
	}
	if inst == latest {
		return ok("serena", inst, latest, "uv tool + PyPI")
	}
	return needUpdate("serena", inst, latest,
		"uv tool upgrade serena-agent", "uv tool upgrade serena-agent", "uv tool is outdated")
}

func pypiLatest(ctx context.Context, pkg string) (string, error) {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, "GET", "https://pypi.org/pypi/"+pkg+"/json", nil)
	if err != nil {
		return "", fmt.Errorf("cannot build PyPI request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach PyPI: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // close errors need no action on a read path
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("PyPI returned status %s", resp.Status)
	}
	var v struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", fmt.Errorf("cannot parse PyPI response: %w", err)
	}
	return v.Info.Version, nil
}

func checkGortex(ctx context.Context) Result {
	if !commandExists("gortex") {
		return unknown("gortex", "-", "-", "gortex binary is not in PATH")
	}
	out, err := execOut(ctx, 15*time.Second, "", "gortex", "version")
	if err != nil {
		return unknown("gortex", "-", "-", "cannot read gortex version")
	}
	inst := ""
	if line, _, _ := strings.Cut(out, "\n"); line != "" {
		if f := strings.Fields(line); len(f) >= 2 {
			inst = f[1]
		}
	}
	if inst == "" {
		return unknown("gortex", "-", "-", "cannot read gortex version")
	}
	// `gortex upgrade` without --run only prints the plan. It reports
	// "already the latest" when the version is current.
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, "gortex", "upgrade")
	upOut, err := cmd.CombinedOutput()
	if err != nil {
		return unknown("gortex", inst, "-", "cannot run gortex upgrade")
	}
	if strings.Contains(strings.ToLower(string(upOut)), "already the latest") {
		return ok("gortex", inst, inst, "self-update: latest")
	}
	return needUpdate("gortex", inst, "latest?",
		"gortex upgrade --run", "gortex upgrade --run",
		"update is available (inspect with: gortex upgrade)")
}

func checkBrewOutdated(ctx context.Context) Result {
	if !commandExists("brew") {
		return unknown("brew-outdated", "-", "-", "brew is not installed")
	}
	out, err := execOut(ctx, 120*time.Second, "", "brew", "outdated")
	if err != nil {
		return unknown("brew-outdated", "-", "-", "cannot run brew outdated")
	}
	if strings.TrimSpace(out) == "" {
		return ok("brew-outdated", "0", "0", "all formulae and casks are current")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	names := strings.Join(lines, " ")
	return needUpdate("brew-outdated", fmt.Sprintf("%d packages", len(lines)), "-",
		"brew upgrade # outdated: "+names, "brew upgrade",
		"outdated: "+names)
}

func npmOutdatedKeys(ctx context.Context, dir string) (string, error) {
	out, err := execOut(ctx, 60*time.Second, dir, "npm", "outdated", "--json")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "", nil
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return "", fmt.Errorf("cannot parse npm outdated output: %w", err)
	}
	return strings.Join(slices.Sorted(maps.Keys(v)), " "), nil
}

func checkNpmOutdatedGlobal(ctx context.Context) Result {
	if !commandExists("npm") {
		return unknown("npm-outdated-g", "-", "-", "npm is not installed")
	}
	list, err := npmOutdatedKeys(ctx, "")
	if err != nil {
		return unknown("npm-outdated-g", "-", "-", "cannot run npm outdated")
	}
	if list == "" {
		return ok("npm-outdated-g", "0", "0", "all global packages are current")
	}
	return needUpdate("npm-outdated-g", list, "-",
		"npm update -g # outdated: "+list, "npm update -g",
		"global packages are outdated")
}

func checkPiPackages(ctx context.Context) Result {
	h, err := userHome()
	if err != nil {
		return unknown("pi-packages", "-", "-", "cannot determine home directory")
	}
	dir := filepath.Join(h, ".pi", "agent", "npm")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return unknown("pi-packages", "-", "-", dir+" is missing")
	}
	list, err := npmOutdatedKeys(ctx, dir)
	if err != nil {
		return unknown("pi-packages", "-", "-", "cannot run npm outdated")
	}
	if list == "" {
		return ok("pi-packages", "0", "0", "~/.pi/agent/npm is current")
	}
	return needUpdate("pi-packages", list, "-",
		"cd ~/.pi/agent/npm && npm update # outdated: "+list,
		"npm update --prefix \""+dir+"\"",
		"Pi extensions are outdated")
}

func checkSuperpowers(ctx context.Context) Result {
	h, err := userHome()
	if err != nil {
		return unknown("superpowers", "-", "-", "cannot determine home directory")
	}
	dir := filepath.Join(h, ".pi", "agent", "git", "github.com", "obra", "superpowers")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return unknown("superpowers", "-", "-", "repo was not found")
	}
	local, err := execOut(ctx, 15*time.Second, dir, "git", "rev-parse", "HEAD")
	if err != nil || local == "" {
		return unknown("superpowers", "-", "-", "cannot read git HEAD")
	}
	remoteOut, err := execOut(ctx, 30*time.Second, dir, "git", "ls-remote", "origin", "main")
	if err != nil {
		return unknown("superpowers", shortSHA(local), "-", "cannot reach origin (offline?)")
	}
	remote := ""
	if f := strings.Fields(remoteOut); len(f) > 0 {
		remote = f[0]
	}
	if remote == "" {
		return unknown("superpowers", shortSHA(local), "-", "cannot reach origin (offline?)")
	}
	dirty, err := execOut(ctx, 15*time.Second, dir, "git", "status", "-uno", "--porcelain=v1")
	if err != nil {
		return unknown("superpowers", shortSHA(local), "-", "cannot read git status")
	}
	note := "main is in sync with origin"
	if dirty != "" {
		first, _, _ := strings.Cut(dirty, "\n")
		note += "; dirty files: " + first
	}
	if local == remote {
		return ok("superpowers", shortSHA(local), shortSHA(remote), note)
	}
	return needUpdate("superpowers", shortSHA(local), shortSHA(remote),
		"git -C ~/.pi/agent/git/github.com/obra/superpowers pull --ff-only",
		"git -C \""+dir+"\" pull --ff-only",
		"main is behind origin")
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func checkHerdrIntegrations(ctx context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr-integr", "-", "-", "herdr is not installed")
	}
	out, err := execOut(ctx, 30*time.Second, "", "herdr", "integration", "status")
	if err != nil || strings.TrimSpace(out) == "" {
		return unknown("herdr-integr", "-", "-", "cannot read integration status")
	}
	need := []string{}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "pi:") && !strings.Contains(line, "current") {
			need = append(need, "pi")
		}
		if strings.HasPrefix(line, "opencode:") && !strings.Contains(line, "current") {
			need = append(need, "opencode")
		}
	}
	if len(need) == 0 {
		return ok("herdr-integr", "pi,opencode", "current", "agent state hooks are in sync")
	}
	targets := strings.Join(need, " ")
	cmds := make([]string, 0, len(need))
	for _, t := range need {
		cmds = append(cmds, "herdr integration install "+t)
	}
	return needUpdate("herdr-integr", "stale", "current",
		strings.Join(cmds, "\n  "),
		strings.Join(cmds, " && "),
		"stale hooks: "+targets)
}

func checkGhosttyConfig(ctx context.Context) Result {
	if _, err := os.Stat(ghosttyBin); err != nil {
		return unknown("ghostty-config", "-", "-", "Ghostty.app was not found")
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, ghosttyBin, "+show-config", "--changes-only")
	if err := cmd.Run(); err != nil {
		return unknown("ghostty-config", "invalid?", "valid", "configuration failed validation, inspect it manually")
	}
	return ok("ghostty-config", "valid", "valid", "configuration is valid")
}

func checkPiConfig(_ context.Context) Result {
	h, err := userHome()
	if err != nil {
		return unknown("pi-config", "-", "-", "cannot determine home directory")
	}
	settings := filepath.Join(h, ".pi", "agent", "settings.json")
	mcp := filepath.Join(h, ".pi", "agent", "mcp.json")
	cache := filepath.Join(h, ".pi", "agent", "mcp-cache.json")
	problems := []string{}
	for _, f := range []string{settings, mcp} {
		raw, err := os.ReadFile(f)
		if err != nil {
			problems = append(problems, filepath.Base(f)+" is missing")
			continue
		}
		var v any
		if json.Unmarshal(raw, &v) != nil {
			problems = append(problems, filepath.Base(f)+" has invalid JSON")
		}
	}
	if _, err := os.Stat(cache); err != nil {
		problems = append(problems, "mcp-cache.json is missing")
	}
	if len(problems) > 0 {
		return unknown("pi-config", "check", "-", "configuration problem: "+strings.Join(problems, "; "))
	}
	// Count the servers without ever printing the content (it can contain secrets).
	raw, err := os.ReadFile(mcp)
	if err != nil {
		return unknown("pi-config", "check", "-", "configuration problem: mcp.json is unreadable")
	}
	var v struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return unknown("pi-config", "check", "-", "configuration problem: mcp.json is unreadable")
	}
	return ok("pi-config", "valid", "valid",
		fmt.Sprintf("%d MCP servers, cache is present", len(v.MCPServers)))
}

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
