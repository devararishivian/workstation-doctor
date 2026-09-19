// Package doctor menjalankan pengecekan read-only atas komponen
// workstation (Pi, Herdr, Ghostty, paket npm/uv, config, skills)
// dan melaporkan versi terpasang vs versi terbaru.
package doctor

import (
	"context"
	"encoding/json"
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

// Status hasil satu komponen.
const (
	StatusOK      = "OK"
	StatusUpdate  = "UPDATE"
	StatusUnknown = "UNKNOWN"
)

// Result adalah hasil pengecekan satu komponen.
type Result struct {
	Component string
	Installed string
	Latest    string
	Status    string
	Note      string
	// Manual adalah langkah manual siap copy-paste (hanya bila UPDATE).
	Manual string
	// Fix adalah perintah yang dijalankan mode otomatis (hanya bila UPDATE).
	Fix string
}

// Summary menghitung jumlah tiap status.
type Summary struct {
	OK, Update, Unknown int
}

// Summarize menghitung ringkasan dari daftar hasil.
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

// Pending mengembalikan hasil berstatus UPDATE (butuh tindakan).
func Pending(results []Result) []Result {
	out := []Result{}
	for _, r := range results {
		if r.Status == StatusUpdate {
			out = append(out, r)
		}
	}
	return out
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// execOut menjalankan perintah dengan timeout dan mengembalikan stdout.
// Exit code non-nol TIDAK dianggap fatal (mis. `npm outdated` exit 1
// saat ada paket tertinggal) — output tetap dipakai.
func execOut(ctx context.Context, timeout time.Duration, dir, name string, args ...string) string {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(c, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	// Exit non-nol bermakna di sini (mis. npm outdated exit 1
	// saat ada paket tertinggal) sehingga error sengaja diabaikan
	// dan stdout tetap dipakai.
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Run menjalankan seluruh cek berurutan dan mengembalikan hasilnya.
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

// PrintTable mencetak hasil dalam format tabel teks.
func PrintTable(w io.Writer, results []Result) {
	fmt.Fprintf(w, "\n%-22s %-16s %-16s %-10s %s\n", "KOMPONEN", "TERPASANG", "TERBARU", "STATUS", "CATATAN")
	fmt.Fprintln(w, "----------------------------------------------------------------------------------------------")
	for _, r := range results {
		fmt.Fprintf(w, "%-22s %-16s %-16s %-10s %s\n",
			trunc(r.Component, 22), trunc(r.Installed, 16), trunc(r.Latest, 16), r.Status, r.Note)
	}
	s := Summarize(results)
	fmt.Fprintf(w, "\nRingkasan: %d OK · %d perlu update · %d unknown\n", s.OK, s.Update, s.Unknown)
}

func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// PrintManual mencetak langkah manual berurutan untuk hasil UPDATE.
func PrintManual(w io.Writer, results []Result) {
	pending := Pending(results)
	fmt.Fprintln(w, "\n== Langkah manual berurutan ==")
	if len(pending) == 0 {
		fmt.Fprintln(w, "Tidak ada tindakan. Semua komponen terkini.")
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

func npmLatest(ctx context.Context, pkg string) string {
	return execOut(ctx, 30*time.Second, "", "npm", "view", pkg, "version")
}

func npmGlobalInstalled(ctx context.Context, pkg string) string {
	out := execOut(ctx, 30*time.Second, "", "npm", "list", "-g", "--depth=0", "--json")
	var v struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if json.Unmarshal([]byte(out), &v) != nil {
		return ""
	}
	return v.Dependencies[pkg].Version
}

func checkPi(ctx context.Context) Result {
	if !commandExists("pi") {
		return unknown("pi", "-", "-", "binary pi tidak ada di PATH")
	}
	inst := execOut(ctx, 15*time.Second, "", "pi", "--version")
	if inst == "" {
		return unknown("pi", "-", "-", "gagal baca pi --version")
	}
	latest := npmLatest(ctx, "@earendil-works/pi-coding-agent")
	if latest == "" {
		return unknown("pi", inst, "-", "gagal baca npm registry (offline?)")
	}
	if inst == latest {
		return ok("pi", inst, latest, "npm")
	}
	pkg := fmt.Sprintf("@earendil-works/pi-coding-agent@%s", latest)
	cmd := "npm i -g " + pkg
	return needUpdate("pi", inst, latest, cmd, cmd, "npm tertinggal")
}

func checkHerdr(ctx context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr", "-", "-", "binary herdr tidak ada di PATH")
	}
	out := execOut(ctx, 15*time.Second, "", "herdr", "--version")
	inst := ""
	if f := strings.Fields(out); len(f) >= 2 {
		inst = f[1]
	}
	if inst == "" {
		return unknown("herdr", "-", "-", "gagal baca herdr --version")
	}
	info := execOut(ctx, 60*time.Second, "", "brew", "info", "--json=v2", "herdr")
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
		return unknown("herdr", inst, "-", "gagal baca brew info (offline?)")
	}
	if inst == latest {
		return ok("herdr", inst, latest, "brew")
	}
	return needUpdate("herdr", inst, latest, "brew upgrade herdr", "brew upgrade herdr", "brew tertinggal")
}

const ghosttyBin = "/Applications/Ghostty.app/Contents/MacOS/ghostty"

func checkGhostty(ctx context.Context) Result {
	if _, err := os.Stat(ghosttyBin); err != nil {
		return unknown("ghostty", "-", "-", "Ghostty.app tidak ditemukan")
	}
	out := execOut(ctx, 15*time.Second, "", ghosttyBin, "--version")
	inst := ""
	if line, _, _ := strings.Cut(out, "\n"); line != "" {
		if f := strings.Fields(line); len(f) >= 2 {
			inst = f[len(f)-1]
		}
	}
	if inst == "" {
		return unknown("ghostty", "-", "-", "gagal baca ghostty --version")
	}
	info := execOut(ctx, 60*time.Second, "", "brew", "info", "--cask", "--json=v2", "ghostty")
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
		return unknown("ghostty", inst, "-", "gagal baca brew cask info (offline?)")
	}
	if inst == latest {
		return ok("ghostty", inst, latest, "brew cask")
	}
	return needUpdate("ghostty", inst, latest,
		"brew upgrade --cask ghostty", "brew upgrade --cask ghostty", "cask tertinggal")
}

func checkNpmPkg(ctx context.Context, label, pkg string) Result {
	if !commandExists("npm") {
		return unknown(label, "-", "-", "npm tidak ada di PATH")
	}
	inst := npmGlobalInstalled(ctx, pkg)
	if inst == "" {
		return unknown(label, "-", "-", "paket npm global tidak ditemukan / npm error")
	}
	latest := npmLatest(ctx, pkg)
	if latest == "" {
		return unknown(label, inst, "-", "gagal baca versi terbaru (offline?)")
	}
	if inst == latest {
		return ok(label, inst, latest, "npm global")
	}
	cmd := fmt.Sprintf("npm i -g %s@%s", pkg, latest)
	return needUpdate(label, inst, latest, cmd, cmd, "npm global tertinggal")
}

func checkSerena(ctx context.Context) Result {
	if !commandExists("serena") {
		return unknown("serena", "-", "-", "binary serena tidak ada di PATH")
	}
	out := execOut(ctx, 15*time.Second, "", "serena", "--version")
	inst := ""
	if f := strings.Fields(out); len(f) >= 2 {
		inst = f[1]
	}
	if inst == "" {
		return unknown("serena", "-", "-", "gagal baca serena --version")
	}
	latest := pypiLatest(ctx, "serena-agent")
	if latest == "" {
		return unknown("serena", inst, "-", "gagal baca PyPI (offline?)")
	}
	if inst == latest {
		return ok("serena", inst, latest, "uv tool + PyPI")
	}
	return needUpdate("serena", inst, latest,
		"uv tool upgrade serena-agent", "uv tool upgrade serena-agent", "uv tool tertinggal")
}

func pypiLatest(ctx context.Context, pkg string) string {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, "GET", "https://pypi.org/pypi/"+pkg+"/json", nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close() //nolint:errcheck // body close best-effort pada read path
	var v struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return ""
	}
	return v.Info.Version
}

func checkGortex(ctx context.Context) Result {
	if !commandExists("gortex") {
		return unknown("gortex", "-", "-", "binary gortex tidak ada di PATH")
	}
	out := execOut(ctx, 15*time.Second, "", "gortex", "version")
	inst := ""
	if line, _, _ := strings.Cut(out, "\n"); line != "" {
		if f := strings.Fields(line); len(f) >= 2 {
			inst = f[1]
		}
	}
	if inst == "" {
		return unknown("gortex", "-", "-", "gagal baca gortex version")
	}
	// `gortex upgrade` tanpa --run hanya mencetak rencana; bila sudah
	// terkini ia menyatakan "already the latest".
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, "gortex", "upgrade")
	upOut, _ := cmd.CombinedOutput()
	if strings.Contains(strings.ToLower(string(upOut)), "already the latest") {
		return ok("gortex", inst, inst, "self-update: latest")
	}
	return needUpdate("gortex", inst, "latest?",
		"gortex upgrade --run", "gortex upgrade --run",
		"update tersedia (cek: gortex upgrade)")
}

func checkBrewOutdated(ctx context.Context) Result {
	if !commandExists("brew") {
		return unknown("brew-outdated", "-", "-", "brew tidak ada")
	}
	out := execOut(ctx, 120*time.Second, "", "brew", "outdated")
	if strings.TrimSpace(out) == "" {
		return ok("brew-outdated", "0", "0", "semua formula/cask terkini")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	names := strings.Join(lines, " ")
	return needUpdate("brew-outdated", fmt.Sprintf("%d paket", len(lines)), "-",
		"brew upgrade # tertinggal: "+names, "brew upgrade",
		"tertanggal: "+names)
}

func npmOutdatedKeys(ctx context.Context, dir string) string {
	out := execOut(ctx, 60*time.Second, dir, "npm", "outdated", "--json")
	if strings.TrimSpace(out) == "" {
		return ""
	}
	var v map[string]any
	if json.Unmarshal([]byte(out), &v) != nil {
		return ""
	}
	return strings.Join(slices.Sorted(maps.Keys(v)), " ")
}

func checkNpmOutdatedGlobal(ctx context.Context) Result {
	if !commandExists("npm") {
		return unknown("npm-outdated-g", "-", "-", "npm tidak ada")
	}
	list := npmOutdatedKeys(ctx, "")
	if list == "" {
		return ok("npm-outdated-g", "0", "0", "semua paket global terkini")
	}
	return needUpdate("npm-outdated-g", list, "-",
		"npm update -g # tertinggal: "+list, "npm update -g",
		"paket global tertinggal")
}

func checkPiPackages(ctx context.Context) Result {
	dir := filepath.Join(home(), ".pi", "agent", "npm")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return unknown("pi-packages", "-", "-", dir+" tidak ada")
	}
	list := npmOutdatedKeys(ctx, dir)
	if list == "" {
		return ok("pi-packages", "0", "0", "~/.pi/agent/npm terkini")
	}
	return needUpdate("pi-packages", list, "-",
		"cd ~/.pi/agent/npm && npm update # tertinggal: "+list,
		"npm update --prefix \""+dir+"\"",
		"extension Pi tertinggal")
}

func checkSuperpowers(ctx context.Context) Result {
	dir := filepath.Join(home(), ".pi", "agent", "git", "github.com", "obra", "superpowers")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return unknown("superpowers", "-", "-", "repo tidak ditemukan")
	}
	local := execOut(ctx, 15*time.Second, dir, "git", "rev-parse", "HEAD")
	if local == "" {
		return unknown("superpowers", "-", "-", "gagal baca git HEAD")
	}
	remote := ""
	if f := strings.Fields(execOut(ctx, 30*time.Second, dir, "git", "ls-remote", "origin", "main")); len(f) > 0 {
		remote = f[0]
	}
	if remote == "" {
		return unknown("superpowers", shortSHA(local), "-", "gagal hubungi origin (offline?)")
	}
	// Status file lokal sebagai info tambahan (read-only).
	dirty := execOut(ctx, 15*time.Second, dir, "git", "status", "-uno", "--porcelain=v1")
	note := "main sinkron origin"
	if dirty != "" {
		first, _, _ := strings.Cut(dirty, "\n")
		note += "; kerja kotor: " + first
	}
	if local == remote {
		return ok("superpowers", shortSHA(local), shortSHA(remote), note)
	}
	return needUpdate("superpowers", shortSHA(local), shortSHA(remote),
		"git -C ~/.pi/agent/git/github.com/obra/superpowers pull --ff-only",
		"git -C \""+dir+"\" pull --ff-only",
		"main tertinggal dari origin")
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func checkHerdrIntegrations(ctx context.Context) Result {
	if !commandExists("herdr") {
		return unknown("herdr-integr", "-", "-", "herdr tidak ada")
	}
	out := execOut(ctx, 30*time.Second, "", "herdr", "integration", "status")
	if strings.TrimSpace(out) == "" {
		return unknown("herdr-integr", "-", "-", "gagal baca integration status")
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
		return ok("herdr-integr", "pi,opencode", "current", "hook status agen sinkron")
	}
	targets := strings.Join(need, " ")
	cmds := make([]string, 0, len(need))
	for _, t := range need {
		cmds = append(cmds, "herdr integration install "+t)
	}
	return needUpdate("herdr-integr", "stale", "current",
		strings.Join(cmds, "\n  "),
		strings.Join(cmds, " && "),
		"hook ketinggalan: "+targets)
}

func checkGhosttyConfig(ctx context.Context) Result {
	if _, err := os.Stat(ghosttyBin); err != nil {
		return unknown("ghostty-config", "-", "-", "Ghostty.app tidak ada")
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, ghosttyBin, "+show-config", "--changes-only")
	if err := cmd.Run(); err != nil {
		return unknown("ghostty-config", "invalid?", "valid", "config gagal divalidasi, cek manual")
	}
	return ok("ghostty-config", "valid", "valid", "config terparse")
}

func checkPiConfig(_ context.Context) Result {
	h := home()
	settings := filepath.Join(h, ".pi", "agent", "settings.json")
	mcp := filepath.Join(h, ".pi", "agent", "mcp.json")
	cache := filepath.Join(h, ".pi", "agent", "mcp-cache.json")
	problems := []string{}
	for _, f := range []string{settings, mcp} {
		raw, err := os.ReadFile(f)
		if err != nil {
			problems = append(problems, filepath.Base(f)+" hilang")
			continue
		}
		var v any
		if json.Unmarshal(raw, &v) != nil {
			problems = append(problems, filepath.Base(f)+" JSON rusak")
		}
	}
	if _, err := os.Stat(cache); err != nil {
		problems = append(problems, "mcp-cache.json hilang")
	}
	if len(problems) > 0 {
		return unknown("pi-config", "cek", "-", "masalah config: "+strings.Join(problems, "; "))
	}
	// Hitung server tanpa pernah mencetak isi (mungkin berisi secret).
	raw, _ := os.ReadFile(mcp)
	var v struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	_ = json.Unmarshal(raw, &v)
	return ok("pi-config", "valid", "valid",
		fmt.Sprintf("%d MCP server, cache ada", len(v.MCPServers)))
}

var skillDescRe = regexp.MustCompile(`(?ms)^description:\s*(.+?)\s*$`)

func checkSkills(_ context.Context) Result {
	root := filepath.Join(home(), ".agents", "skills")
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return unknown("skills", "-", "-", root+" tidak ada")
	}
	total, bad := 0, []string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
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
			rel, _ := filepath.Rel(root, p)
			bad = append(bad, rel)
		}
		return nil
	})
	if total == 0 {
		return unknown("skills", "-", "-", "tidak ada SKILL.md ditemukan")
	}
	if len(bad) == 0 {
		return ok("skills", fmt.Sprintf("%d skill", total), "100%<=1024", "batas agentskills.io lolos")
	}
	show := bad
	if len(show) > 5 {
		show = show[:5]
	}
	return unknown("skills", fmt.Sprintf("%d skill", total), "-",
		fmt.Sprintf("%d deskripsi >1024 char: %s", len(bad), strings.Join(show, ", ")))
}
