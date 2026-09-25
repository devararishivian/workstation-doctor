// Package doctor runs read-only checks on workstation components
// (Pi, Herdr, Ghostty, npm/uv packages, configuration, skills)
// and reports installed versions against latest versions.
package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// Status colors follow the Catppuccin palette. lipgloss renders them
// only on a capable terminal; piped output stays plain automatically.
var (
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1"))
	updateStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9E2AF"))
	unknownStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F38BA8"))
)

// Status of one component.
const (
	StatusOK      = "OK"
	StatusUpdate  = "UPDATE"
	StatusUnknown = "UNKNOWN"
)

// Category represents the audit component category.
type Category string

// Audit component categories.
const (
	CategoryTool   Category = "tool"
	CategoryConfig Category = "config"
	CategorySystem Category = "system"
	CategorySkill  Category = "skill"
)

// Checker is the strategy interface for an audit component.
type Checker interface {
	Name() string
	Category() Category
	Check(ctx context.Context) Result
}

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

// Engine manages checkers and coordinates concurrent execution.
type Engine struct {
	checkers []Checker
}

// NewEngine creates an Engine initialized with all standard audit checkers.
func NewEngine() *Engine {
	return &Engine{
		checkers: []Checker{
			&piVersionChecker{},
			&herdrVersionChecker{},
			&ghosttyVersionChecker{},
			newNpmPackageChecker("opencode", "opencode-ai"),
			newNpmPackageChecker("tokenjuice", "tokenjuice"),
			&serenaVersionChecker{},
			&gortexVersionChecker{},
			&ghosttyConfigValidChecker{},
			&ghosttyConfigVersionChecker{},
			&herdrConfigValidChecker{},
			&herdrConfigVersionChecker{},
			&piConfigValidChecker{},
			&piConfigVersionChecker{},
			&brewOutdatedChecker{},
			&npmOutdatedGlobalChecker{},
			&piPackagesChecker{},
			&superpowersChecker{},
			&herdrIntegrationsChecker{},
			&herdrPluginsChecker{},
			&skillsChecker{},
		},
	}
}

// Run executes all checkers concurrently and returns the results preserving order.
func (e *Engine) Run(ctx context.Context) []Result {
	results := make([]Result, len(e.checkers))
	var wg sync.WaitGroup
	for i, chk := range e.checkers {
		wg.Add(1)
		go func(idx int, c Checker) {
			defer wg.Done()
			results[idx] = c.Check(ctx)
		}(i, chk)
	}
	wg.Wait()
	return results
}

// Run executes all checks concurrently and returns the results.
// It serves as a facade for NewEngine().Run(ctx).
func Run(ctx context.Context) []Result {
	return NewEngine().Run(ctx)
}

// FormatTable renders the results as a text table. It returns text
// instead of writing it, so callers route the report through the
// program-output logger and the output stays pipeable.
func FormatTable(results []Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%-22s %-16s %-16s %-10s %s\n", "COMPONENT", "INSTALLED", "LATEST", "STATUS", "NOTE")
	fmt.Fprintln(&b, "----------------------------------------------------------------------------------------------")
	for _, r := range results {
		fmt.Fprintf(&b, "%-22s %-16s %-16s %-10s %s\n",
			trunc(r.Component, 22), trunc(r.Installed, 16), trunc(r.Latest, 16), r.Status, r.Note)
	}
	s := Summarize(results)
	fmt.Fprintf(&b, "\nSummary: %s · %s · %s\n",
		okStyle.Render(strconv.Itoa(s.OK)+" OK"),
		updateStyle.Render(strconv.Itoa(s.Update)+" need update"),
		unknownStyle.Render(strconv.Itoa(s.Unknown)+" unknown"))
	return b.String()
}

func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// FormatManual renders the ordered manual steps for UPDATE results.
// It returns text instead of writing it, for the same reason as FormatTable.
func FormatManual(results []Result) string {
	pending := Pending(results)
	var b strings.Builder
	fmt.Fprintln(&b, "\n== Ordered manual steps ==")
	if len(pending) == 0 {
		fmt.Fprintln(&b, "No action is needed. All components are current.")
		return b.String()
	}
	for i, r := range pending {
		fmt.Fprintf(&b, "%d. %s\n", i+1, r.Manual)
	}
	return b.String()
}

func ok(component, installed, latest, note string) Result {
	return Result{Component: component, Installed: installed, Latest: latest, Status: StatusOK, Note: note}
}

type piVersionChecker struct{}

func (c *piVersionChecker) Name() string       { return "pi" }
func (c *piVersionChecker) Category() Category { return CategoryTool }
func (c *piVersionChecker) Check(ctx context.Context) Result {
	return checkPi(ctx)
}

type herdrVersionChecker struct{}

func (c *herdrVersionChecker) Name() string       { return "herdr" }
func (c *herdrVersionChecker) Category() Category { return CategoryTool }
func (c *herdrVersionChecker) Check(ctx context.Context) Result {
	return checkHerdr(ctx)
}

type ghosttyVersionChecker struct{}

func (c *ghosttyVersionChecker) Name() string       { return "ghostty" }
func (c *ghosttyVersionChecker) Category() Category { return CategoryTool }
func (c *ghosttyVersionChecker) Check(ctx context.Context) Result {
	return checkGhostty(ctx)
}

type npmPackageChecker struct {
	label string
	pkg   string
}

func newNpmPackageChecker(label, pkg string) *npmPackageChecker {
	return &npmPackageChecker{label: label, pkg: pkg}
}

func (c *npmPackageChecker) Name() string       { return c.label }
func (c *npmPackageChecker) Category() Category { return CategoryTool }
func (c *npmPackageChecker) Check(ctx context.Context) Result {
	return checkNpmPkg(ctx, c.label, c.pkg)
}

type serenaVersionChecker struct{}

func (c *serenaVersionChecker) Name() string       { return "serena" }
func (c *serenaVersionChecker) Category() Category { return CategoryTool }
func (c *serenaVersionChecker) Check(ctx context.Context) Result {
	return checkSerena(ctx)
}

type gortexVersionChecker struct{}

func (c *gortexVersionChecker) Name() string       { return "gortex" }
func (c *gortexVersionChecker) Category() Category { return CategoryTool }
func (c *gortexVersionChecker) Check(ctx context.Context) Result {
	return checkGortex(ctx)
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

var pypiClient = &http.Client{Timeout: 15 * time.Second}

func pypiLatest(ctx context.Context, pkg string) (string, error) {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, "GET", "https://pypi.org/pypi/"+pkg+"/json", nil)
	if err != nil {
		return "", fmt.Errorf("cannot build PyPI request: %w", err)
	}
	resp, err := pypiClient.Do(req)
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

type herdrPluginSource struct {
	Kind           string `json:"kind"`
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	Subdir         string `json:"subdir,omitempty"`
	RequestedRef   string `json:"requested_ref,omitempty"`
	ResolvedCommit string `json:"resolved_commit"`
	ManagedPath    string `json:"managed_path,omitempty"`
}

type herdrPluginItem struct {
	PluginID string            `json:"plugin_id"`
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	Enabled  bool              `json:"enabled"`
	Source   herdrPluginSource `json:"source"`
}

func herdrPluginsPath() (string, error) {
	if cfg := os.Getenv("HERDR_CONFIG_PATH"); cfg != "" {
		return filepath.Join(filepath.Dir(cfg), "plugins.json"), nil
	}
	h, err := userHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".config", "herdr", "plugins.json"), nil
}

func checkHerdrPlugins(ctx context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr-plugins", "-", "-", "herdr is not installed")
	}
	path, err := herdrPluginsPath()
	if err != nil {
		return unknown("herdr-plugins", "-", "-", "cannot determine plugins path: "+err.Error())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ok("herdr-plugins", "0 plugins", "0 plugins", "no plugins installed")
		}
		return unknown("herdr-plugins", "-", "-", "cannot read plugins.json: "+err.Error())
	}
	var plugins []herdrPluginItem
	if err := json.Unmarshal(raw, &plugins); err != nil {
		return unknown("herdr-plugins", "-", "-", "cannot parse plugins.json: "+err.Error())
	}
	return evaluateHerdrPlugins(plugins, func(owner, repo, ref, managedPath string) (string, error) {
		return gitRemoteCommit(ctx, managedPath, owner, repo, ref)
	})
}

func gitRemoteCommit(ctx context.Context, dir, owner, repo, ref string) (string, error) {
	targetRef := ref
	if targetRef == "" {
		targetRef = "HEAD"
	}
	url := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	out, err := execOut(ctx, 15*time.Second, "", "git", "ls-remote", url, targetRef)
	if err != nil && dir != "" {
		out, err = execOut(ctx, 15*time.Second, dir, "git", "ls-remote", "origin", targetRef)
	}
	if err != nil {
		return "", fmt.Errorf("remote commit lookup for %s/%s: %w", owner, repo, err)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return "", errors.New("empty output from git ls-remote")
	}
	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		if strings.HasSuffix(line, "^{}") {
			if f := strings.Fields(line); len(f) > 0 {
				return f[0], nil
			}
		}
	}
	if f := strings.Fields(lines[0]); len(f) > 0 {
		return f[0], nil
	}
	return "", errors.New("no commit found in git ls-remote output")
}

type pluginCheckResult struct {
	name      string
	remoteSHA string
	err       error
}

func evaluateHerdrPlugins(
	plugins []herdrPluginItem,
	resolveRemoteCommit func(owner, repo, ref, managedPath string) (string, error),
) Result {
	if len(plugins) == 0 {
		return ok("herdr-plugins", "0 plugins", "0 plugins", "no plugins installed")
	}

	pluginResults := make([]pluginCheckResult, len(plugins))
	var wg sync.WaitGroup
	for i, p := range plugins {
		if p.Source.Kind != "github" {
			continue
		}
		name := p.PluginID
		if name == "" {
			name = p.Name
		}
		if name == "" {
			name = p.Source.Owner + "/" + p.Source.Repo
		}
		wg.Add(1)
		go func(idx int, item herdrPluginItem, pluginName string) {
			defer wg.Done()
			sha, err := resolveRemoteCommit(item.Source.Owner, item.Source.Repo, item.Source.RequestedRef, item.Source.ManagedPath)
			pluginResults[idx] = pluginCheckResult{
				name:      pluginName,
				remoteSHA: sha,
				err:       err,
			}
		}(i, p, name)
	}
	wg.Wait()

	var (
		outdatedNames []string
		manuals       []string
		fixes         []string
		failed        []string
	)
	for i, p := range plugins {
		if p.Source.Kind != "github" {
			continue
		}
		res := pluginResults[i]
		if res.err != nil {
			failed = append(failed, res.name)
			continue
		}
		if res.remoteSHA != "" && p.Source.ResolvedCommit != res.remoteSHA {
			outdatedNames = append(outdatedNames, res.name)
			target := p.Source.Owner + "/" + p.Source.Repo
			if p.Source.Subdir != "" {
				target += "/" + p.Source.Subdir
			}
			manualCmd := "herdr plugin install " + target
			fixCmd := "herdr plugin install " + target
			if p.Source.RequestedRef != "" {
				manualCmd += " --ref " + p.Source.RequestedRef
				fixCmd += " --ref " + p.Source.RequestedRef
			}
			fixCmd += " --yes"
			manuals = append(manuals, manualCmd)
			fixes = append(fixes, fixCmd)
		}
	}
	if len(failed) > 0 {
		return unknown("herdr-plugins", fmt.Sprintf("%d plugins", len(plugins)), "-",
			"cannot reach remote repository for: "+strings.Join(failed, " "))
	}
	if len(outdatedNames) == 0 {
		return ok("herdr-plugins", fmt.Sprintf("%d plugins", len(plugins)), "current", "all plugins are in sync")
	}
	return needUpdate(
		"herdr-plugins",
		fmt.Sprintf("%d outdated", len(outdatedNames)),
		"current",
		strings.Join(manuals, "\n  "),
		strings.Join(fixes, " && "),
		"outdated: "+strings.Join(outdatedNames, " "),
	)
}

type ghosttyConfigValidChecker struct{}

func (c *ghosttyConfigValidChecker) Name() string       { return "ghostty-config-valid" }
func (c *ghosttyConfigValidChecker) Category() Category { return CategoryConfig }
func (c *ghosttyConfigValidChecker) Check(ctx context.Context) Result {
	if _, err := os.Stat(ghosttyBin); err != nil {
		return unknown("ghostty-config-valid", "-", "-", "Ghostty.app was not found")
	}
	out, err := execOut(ctx, 15*time.Second, "", ghosttyBin, "+show-config", "--changes-only")
	if err != nil && out == "" {
		return unknown("ghostty-config-valid", "invalid", "valid", "configuration failed validation, inspect it manually")
	}
	return ok("ghostty-config-valid", "valid", "valid", "configuration is valid")
}

type ghosttyConfigVersionChecker struct{}

func (c *ghosttyConfigVersionChecker) Name() string       { return "ghostty-config-version" }
func (c *ghosttyConfigVersionChecker) Category() Category { return CategoryConfig }
func (c *ghosttyConfigVersionChecker) Check(ctx context.Context) Result {
	if _, err := os.Stat(ghosttyBin); err != nil {
		return unknown("ghostty-config-version", "-", "-", "Ghostty.app was not found")
	}
	out, err := execOut(ctx, 15*time.Second, "", ghosttyBin, "+show-config", "--changes-only")
	if err != nil && out == "" {
		return unknown("ghostty-config-version", "-", "-", "cannot read ghostty configuration")
	}
	lower := strings.ToLower(out)
	if strings.Contains(lower, "deprecated") || strings.Contains(lower, "obsolete") {
		return needUpdate("ghostty-config-version", "deprecated options", "current",
			"ghostty +show-config --changes-only", "ghostty +show-config --changes-only",
			"configuration contains deprecated options")
	}
	return ok("ghostty-config-version", "current", "current", "configuration options are current")
}

type herdrConfigValidChecker struct{}

func (c *herdrConfigValidChecker) Name() string       { return "herdr-config-valid" }
func (c *herdrConfigValidChecker) Category() Category { return CategoryConfig }
func (c *herdrConfigValidChecker) Check(ctx context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr-config-valid", "-", "-", "herdr is not installed")
	}
	out, err := execOut(ctx, 15*time.Second, "", "herdr", "config", "check")
	if err != nil {
		return unknown("herdr-config-valid", "invalid", "valid", "herdr config check failed: "+err.Error())
	}
	if strings.Contains(strings.ToLower(out), "ok") {
		return ok("herdr-config-valid", "valid", "valid", "herdr config is valid")
	}
	return unknown("herdr-config-valid", "check", "valid", out)
}

type herdrConfigVersionChecker struct{}

func (c *herdrConfigVersionChecker) Name() string       { return "herdr-config-version" }
func (c *herdrConfigVersionChecker) Category() Category { return CategoryConfig }
func (c *herdrConfigVersionChecker) Check(_ context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr-config-version", "-", "-", "herdr is not installed")
	}
	path, err := herdrConfigPath()
	if err != nil {
		return unknown("herdr-config-version", "-", "-", "cannot determine herdr config path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ok("herdr-config-version", "default", "default", "default configuration is active")
		}
		return unknown("herdr-config-version", "-", "-", "cannot read config.toml: "+err.Error())
	}
	content := string(raw)
	missing := []string{}
	for _, section := range []string{"[ui]", "[theme]", "[[keys.command]]"} {
		if !strings.Contains(content, section) {
			missing = append(missing, section)
		}
	}
	if len(missing) > 0 {
		return needUpdate("herdr-config-version", "legacy", "current",
			"herdr config check", "herdr config check",
			"missing modern sections: "+strings.Join(missing, ", "))
	}
	return ok("herdr-config-version", "current", "current", "config.toml structure is current")
}

func herdrConfigPath() (string, error) {
	if cfg := os.Getenv("HERDR_CONFIG_PATH"); cfg != "" {
		return cfg, nil
	}
	h, err := userHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".config", "herdr", "config.toml"), nil
}

type piConfigValidChecker struct{}

func (c *piConfigValidChecker) Name() string       { return "pi-config-valid" }
func (c *piConfigValidChecker) Category() Category { return CategoryConfig }
func (c *piConfigValidChecker) Check(_ context.Context) Result {
	h, err := userHome()
	if err != nil {
		return unknown("pi-config-valid", "-", "-", "cannot determine home directory")
	}
	settings := filepath.Join(h, ".pi", "agent", "settings.json")
	mcp := filepath.Join(h, ".pi", "agent", "mcp.json")
	cache := filepath.Join(h, ".pi", "agent", "mcp-cache.json")
	problems := []string{}

	settingsRaw, err := os.ReadFile(settings)
	if err != nil {
		problems = append(problems, "settings.json is missing")
	} else {
		var v any
		if json.Unmarshal(settingsRaw, &v) != nil {
			problems = append(problems, "settings.json has invalid JSON")
		}
	}

	mcpRaw, err := os.ReadFile(mcp)
	var mcpParsed struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err != nil {
		problems = append(problems, "mcp.json is missing")
	} else if json.Unmarshal(mcpRaw, &mcpParsed) != nil {
		problems = append(problems, "mcp.json has invalid JSON")
	}

	if _, err := os.Stat(cache); err != nil {
		problems = append(problems, "mcp-cache.json is missing")
	}

	if len(problems) > 0 {
		return unknown("pi-config-valid", "invalid", "valid", "configuration problem: "+strings.Join(problems, "; "))
	}
	return ok("pi-config-valid", "valid", "valid",
		fmt.Sprintf("%d MCP servers, cache is present", len(mcpParsed.MCPServers)))
}

type piConfigVersionChecker struct{}

func (c *piConfigVersionChecker) Name() string       { return "pi-config-version" }
func (c *piConfigVersionChecker) Category() Category { return CategoryConfig }
func (c *piConfigVersionChecker) Check(ctx context.Context) Result {
	if !commandExists("pi") {
		return unknown("pi-config-version", "-", "-", "pi binary is not in PATH")
	}
	inst, err := execOut(ctx, 15*time.Second, "", "pi", "--version")
	if err != nil || inst == "" {
		return unknown("pi-config-version", "-", "-", "cannot read pi --version")
	}
	h, err := userHome()
	if err != nil {
		return unknown("pi-config-version", "-", "-", "cannot determine home directory")
	}
	settings := filepath.Join(h, ".pi", "agent", "settings.json")
	raw, err := os.ReadFile(settings)
	if err != nil {
		return unknown("pi-config-version", "-", inst, "settings.json is missing")
	}
	var v struct {
		LastChangelogVersion string `json:"lastChangelogVersion"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return unknown("pi-config-version", "-", inst, "settings.json has invalid JSON")
	}
	return evaluatePiConfigVersion(v.LastChangelogVersion, inst)
}

func evaluatePiConfigVersion(lastChangelogVersion, installedVersion string) Result {
	if lastChangelogVersion == "" {
		return needUpdate("pi-config-version", "unknown", installedVersion,
			"pi /settings", "pi /reload", "lastChangelogVersion is missing in settings.json")
	}
	if lastChangelogVersion == installedVersion {
		return ok("pi-config-version", lastChangelogVersion, installedVersion, "config is in sync with Pi version")
	}
	return needUpdate("pi-config-version", lastChangelogVersion, installedVersion,
		"pi /settings", "pi /reload", "config version is older than installed Pi version")
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

type brewOutdatedChecker struct{}

func (c *brewOutdatedChecker) Name() string       { return "brew-outdated" }
func (c *brewOutdatedChecker) Category() Category { return CategorySystem }
func (c *brewOutdatedChecker) Check(ctx context.Context) Result {
	return checkBrewOutdated(ctx)
}

type npmOutdatedGlobalChecker struct{}

func (c *npmOutdatedGlobalChecker) Name() string       { return "npm-outdated-g" }
func (c *npmOutdatedGlobalChecker) Category() Category { return CategorySystem }
func (c *npmOutdatedGlobalChecker) Check(ctx context.Context) Result {
	return checkNpmOutdatedGlobal(ctx)
}

type piPackagesChecker struct{}

func (c *piPackagesChecker) Name() string       { return "pi-packages" }
func (c *piPackagesChecker) Category() Category { return CategorySystem }
func (c *piPackagesChecker) Check(ctx context.Context) Result {
	return checkPiPackages(ctx)
}

type superpowersChecker struct{}

func (c *superpowersChecker) Name() string       { return "superpowers" }
func (c *superpowersChecker) Category() Category { return CategorySystem }
func (c *superpowersChecker) Check(ctx context.Context) Result {
	return checkSuperpowers(ctx)
}

type herdrIntegrationsChecker struct{}

func (c *herdrIntegrationsChecker) Name() string       { return "herdr-integr" }
func (c *herdrIntegrationsChecker) Category() Category { return CategorySystem }
func (c *herdrIntegrationsChecker) Check(ctx context.Context) Result {
	return checkHerdrIntegrations(ctx)
}

type herdrPluginsChecker struct{}

func (c *herdrPluginsChecker) Name() string       { return "herdr-plugins" }
func (c *herdrPluginsChecker) Category() Category { return CategorySystem }
func (c *herdrPluginsChecker) Check(ctx context.Context) Result {
	return checkHerdrPlugins(ctx)
}

type skillsChecker struct{}

func (c *skillsChecker) Name() string       { return "skills" }
func (c *skillsChecker) Category() Category { return CategorySkill }
func (c *skillsChecker) Check(ctx context.Context) Result {
	return checkSkills(ctx)
}
