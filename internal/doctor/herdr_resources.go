package doctor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const herdrPluginRegistryName = "plugins.json"

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
	PluginID   string            `json:"plugin_id"`
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Enabled    bool              `json:"enabled"`
	PluginRoot string            `json:"plugin_root"`
	Source     herdrPluginSource `json:"source"`
}

type herdrRegistryEntry = herdrPluginItem

func herdrPluginRegistryPath(h *Host, s Scope) (string, error) {
	paths, e := ResolveHerdrConfiguration(h, s)
	if e != nil {
		return "", e
	}
	if len(paths) != 1 {
		return "", errors.New("herdr configuration location is ambiguous")
	}
	return filepath.Join(filepath.Dir(paths[0].Value), herdrPluginRegistryName), nil
}

// discoverHerdrPlugins reads Herdr's global plugin registry without loading plugin code.
func discoverHerdrPlugins(ctx context.Context, h *Host, s Scope) Discovery {
	if h == nil {
		return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Herdr registry location is unavailable."}}
	}
	path, e := herdrPluginRegistryPath(h, s)
	if e != nil {
		return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Herdr registry location is unavailable."}}
	}
	raw, e := ReadBounded(ctx, path, DefaultLimits().MaxFileBytes)
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Herdr registry inspection was canceled."}}
	}
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Herdr plugin registry could not be read within inspection limits."}}
	}
	entries := []herdrRegistryEntry{}
	if e == nil {
		if json.Unmarshal(raw, &entries) != nil || len(entries) > DefaultLimits().MaxFiles {
			return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Herdr plugin registry format is unsupported."}}
		}
	}
	root := filepath.Dir(path)
	instances := make([]Instance, 0, len(entries)+1)
	if len(entries) == 0 {
		instances = append(instances, herdrRegistryInstance(h, root))
		return Discovery{Availability: AvailabilityPresent, Instances: instances}
	}
	seen := map[string]bool{}
	incomplete := false
	for _, entry := range entries {
		if !validIdentifier(entry.PluginID, DefaultLimits().MaxIdentifierBytes) || seen[entry.PluginID] || !validPluginRoot(entry.PluginRoot) {
			incomplete = true
			continue
		}
		seen[entry.PluginID] = true
		if entry.Source.Kind == "" {
			entry.Source.Kind = "local"
		}
		if !validHerdrPluginSource(entry.Source) {
			entry.Source = herdrPluginSource{Kind: "unsupported"}
			incomplete = true
		}
		instances = append(instances, herdrPluginInstance(h, root, entry))
	}
	slices.SortFunc(instances, func(a, b Instance) int { return strings.Compare(a.ID, b.ID) })
	discovery := Discovery{Availability: AvailabilityPresent, Instances: instances}
	if incomplete {
		discovery.Availability = AvailabilityUndetermined
		discovery.Diagnostics = []string{"Some Herdr plugin registry entries are unsupported; coverage is incomplete."}
	}
	return discovery
}

func validHerdrPluginSource(s herdrPluginSource) bool {
	if s.Kind == "local" {
		return s.Owner == "" && s.Repo == ""
	}
	if s.Kind != "github" || !publicGitComponent(s.Owner) || !publicGitComponent(s.Repo) || !validText(s.Subdir, DefaultLimits().MaxPathBytes) || !validHerdrRef(s.RequestedRef) || !validText(s.ResolvedCommit, 64) {
		return false
	}
	if s.Subdir != "" && (!filepath.IsLocal(filepath.FromSlash(s.Subdir)) || filepath.Clean(filepath.FromSlash(s.Subdir)) == "." || strings.Contains(s.Subdir, "\\")) {
		return false
	}
	if s.ResolvedCommit != "" && !gitObjectID(strings.ToLower(s.ResolvedCommit)) {
		return false
	}
	if s.ManagedPath != "" && (!filepath.IsAbs(s.ManagedPath) || !validText(s.ManagedPath, DefaultLimits().MaxPathBytes)) {
		return false
	}
	return true
}

func validPluginRoot(path string) bool {
	return path == "" || filepath.IsAbs(path) && validText(path, DefaultLimits().MaxPathBytes)
}

func validHerdrRef(ref string) bool {
	if !validText(ref, 256) {
		return false
	}
	if ref == "" {
		return true
	}
	if strings.HasPrefix(ref, "-") || strings.HasSuffix(ref, ".") || strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.ContainsRune(ref, '\\') {
		return false
	}
	for part := range strings.SplitSeq(ref, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	for _, c := range ref {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && !strings.ContainsRune("._-/", c) {
			return false
		}
	}
	return true
}

func herdrRegistryInstance(h *Host, root string) Instance {
	return makeHerdrResourceInstance(h, root, "registry", root, "scope", "")
}

func herdrPluginInstance(h *Host, registry string, p herdrRegistryEntry) Instance {
	path := p.PluginRoot
	if p.Source.ManagedPath != "" {
		path = p.Source.ManagedPath
	}
	if path != "" && !filepath.IsAbs(path) {
		path = ""
	}
	identity := p.PluginID
	i := makeHerdrResourceInstance(h, registry, identity, registry, p.Source.Kind, path)
	i.Configuration = append(i.Configuration,
		Fact{State: EvidenceKnown, Label: "Plugin identity", Value: p.PluginID, Source: "validated Herdr registry"},
		Fact{State: EvidenceKnown, Label: "Plugin source kind", Value: p.Source.Kind, Source: "validated Herdr registry"},
		Fact{State: EvidenceKnown, Label: "Plugin root", Value: path, Source: "Herdr registry", Note: "Declared path is not proof of active loading."},
		Fact{State: EvidenceKnown, Label: "Plugin checkout path", Value: firstNonEmpty(p.Source.ManagedPath, p.PluginRoot), Source: "Herdr registry"},
		Fact{State: EvidenceKnown, Label: "Requested ref", Value: p.Source.RequestedRef, Source: "Herdr registry"},
		Fact{State: EvidenceKnown, Label: "Resolved revision", Value: p.Source.ResolvedCommit, Source: "Herdr registry"},
		Fact{State: EvidenceKnown, Label: "Plugin owner", Value: p.Source.Owner, Source: "validated Herdr registry"},
		Fact{State: EvidenceKnown, Label: "Plugin repository", Value: p.Source.Repo, Source: "validated Herdr registry"},
		Fact{State: EvidenceKnown, Label: "Plugin subdirectory", Value: p.Source.Subdir, Source: "validated Herdr registry"},
		Fact{State: EvidenceKnown, Label: "Plugin enabled", Value: fmt.Sprint(p.Enabled), Source: "Herdr registry"},
	)
	return i
}

func makeHerdrResourceInstance(h *Host, registry, identity, root, kind, path string) Instance {
	sum := sha256.Sum256([]byte(registry + "\x00" + identity))
	now := hostNow(h)
	i := Instance{ID: "herdr-plugins:" + hex.EncodeToString(sum[:]), IntegrationID: "herdr-plugins", Scope: "user", DiscoverySource: "Herdr plugin registry", ObservedAt: now, Availability: AvailabilityPresent, Root: Fact{State: EvidenceKnown, Label: "Registry root", Value: root, Source: "Herdr configuration location", ObservedAt: now}, Provenance: Provenance{State: EvidenceKnown, Manager: "herdr"}, Configuration: []Fact{{State: EvidenceKnown, Label: "Plugin source kind", Value: kind, Source: "Herdr registry"}}}
	if path != "" {
		i.ResolvedPath = Fact{State: EvidenceKnown, Label: "Plugin root", Value: path, Source: "Herdr registry", ObservedAt: now}
	}
	return i
}

func herdrPluginSourceFromInstance(i Instance) herdrPluginSource {
	s := herdrPluginSource{}
	for _, f := range i.Configuration {
		switch f.Label {
		case "Plugin source kind":
			s.Kind = f.Value
		case "Plugin owner":
			s.Owner = f.Value
		case "Plugin repository":
			s.Repo = f.Value
		case "Plugin subdirectory":
			s.Subdir = f.Value
		case "Requested ref":
			s.RequestedRef = f.Value
		case "Resolved revision":
			s.ResolvedCommit = f.Value
		case "Plugin checkout path":
			s.ManagedPath = f.Value
		}
	}
	return s
}

func checkHerdrPluginSources(ctx context.Context, h *Host, _ Scope, i Instance) (findings []Finding) {
	defer func() {
		if ctx.Err() != nil {
			for n := range findings {
				findings[n].Outcome = OutcomeCanceled
				findings[n].Actions = nil
			}
		}
	}()
	f := Finding{Key: FindingKey{IntegrationID: "herdr-plugins", CheckID: "herdr-plugins", InstanceID: i.ID}, Outcome: OutcomeUnknown, Question: "Does this Herdr plugin have an established upstream change?", Explanation: "Plugin source is inspected without loading plugin code or running its build commands.", Evidence: []Fact{i.Root}}
	id := ""
	for _, fact := range i.Configuration {
		if fact.Label == "Plugin identity" {
			id = fact.Value
		}
	}
	if id == "" {
		f.Outcome = OutcomeNotApplicable
		f.Explanation = "The registry scope contains no plugin entries."
		return []Finding{f}
	}
	src := herdrPluginSourceFromInstance(i)
	f.Evidence = append(f.Evidence, i.Configuration...)
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
		return []Finding{f}
	}
	if src.Kind == "local" {
		f.Outcome = OutcomeNotApplicable
		f.Explanation = "Locally linked plugins are not remote update targets."
		return []Finding{f}
	}
	if src.Kind != "github" || src.Owner == "" || src.Repo == "" || !gitObjectID(strings.ToLower(src.ResolvedCommit)) {
		return []Finding{f}
	}
	if isCommitPin(src.RequestedRef) {
		f.Outcome = OutcomeNotApplicable
		f.Explanation = "The requested commit is pinned; automatic advancement is disabled."
		return []Finding{f}
	}
	if h == nil || h.Fetch == nil {
		return []Finding{f}
	}
	status, known := localGitStatus(ctx, h, src.ManagedPath)
	if !known {
		f.Explanation = "The managed plugin checkout could not be inspected safely; no update proposal is available."
		return []Finding{f}
	}
	head, headOK := localGitHead(ctx, h, src.ManagedPath)
	if !headOK || !strings.EqualFold(head, src.ResolvedCommit) {
		f.Explanation = "The managed checkout revision does not match the registry's resolved revision; update eligibility is unknown."
		return []Finding{f}
	}
	f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Managed checkout revision", Value: head, Source: "local Git HEAD", ObservedAt: hostNow(h)})
	if gitHasWorktreeChanges(status) {
		f.Outcome = OutcomeAttention
		f.Explanation = "The managed plugin checkout contains tracked or untracked changes; replacement is disabled."
		f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Checkout status", Value: "modified", Source: "bounded local Git status", ObservedAt: hostNow(h)})
		return []Finding{f}
	}
	ref := src.RequestedRef
	if ref == "" {
		raw, e := h.Fetch(ctx, "https://api.github.com/repos/"+url.PathEscape(src.Owner)+"/"+url.PathEscape(src.Repo))
		if e != nil || len(raw) > int(DefaultLimits().MaxHTTPBytes) {
			return []Finding{f}
		}
		var repo struct {
			DefaultBranch string `json:"default_branch"`
		}
		if json.Unmarshal(raw, &repo) != nil || !publicGitComponent(repo.DefaultBranch) {
			return []Finding{f}
		}
		ref = repo.DefaultBranch
	}
	branchURL := "https://api.github.com/repos/" + url.PathEscape(src.Owner) + "/" + url.PathEscape(src.Repo) + "/branches/" + url.PathEscape(ref)
	raw, e := h.Fetch(ctx, branchURL)
	var branch struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if e != nil || len(raw) > int(DefaultLimits().MaxHTTPBytes) || json.Unmarshal(raw, &branch) != nil || !gitObjectID(strings.ToLower(branch.Commit.SHA)) {
		// A tag is a fixed source, not permission to track a moving branch.
		tagURL := "https://api.github.com/repos/" + url.PathEscape(src.Owner) + "/" + url.PathEscape(src.Repo) + "/git/ref/tags/" + url.PathEscape(ref)
		tag, tagErr := h.Fetch(ctx, tagURL)
		var tagRef struct {
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}
		if tagErr == nil && len(tag) <= int(DefaultLimits().MaxHTTPBytes) && json.Unmarshal(tag, &tagRef) == nil && gitObjectID(strings.ToLower(tagRef.Object.SHA)) {
			f.Outcome = OutcomeNotApplicable
			f.Explanation = "The requested Git tag is pinned; automatic advancement is disabled."
		}
		return []Finding{f}
	}
	upstream := strings.ToLower(branch.Commit.SHA)
	installed := strings.ToLower(src.ResolvedCommit)
	f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Requested branch revision", Value: upstream, Source: "public GitHub branch metadata", ObservedAt: hostNow(h)})
	if upstream == installed {
		f.Outcome = OutcomeOK
		f.Explanation = "The cleanly declared plugin revision matches its requested branch."
		return []Finding{f}
	}
	compareURL := "https://api.github.com/repos/" + url.PathEscape(src.Owner) + "/" + url.PathEscape(src.Repo) + "/compare/" + installed + "..." + upstream
	comparison, compareErr := h.Fetch(ctx, compareURL)
	if compareErr != nil || len(comparison) > int(DefaultLimits().MaxHTTPBytes) {
		return []Finding{f}
	}
	var relation struct {
		Status  string `json:"status"`
		AheadBy int    `json:"ahead_by"`
	}
	if json.Unmarshal(comparison, &relation) != nil {
		return []Finding{f}
	}
	if relation.Status != "ahead" || relation.AheadBy <= 0 {
		f.Explanation = "Local and remote plugin revisions do not establish a simple upstream advancement."
		return []Finding{f}
	}
	f.Outcome = OutcomeAttention
	f.Explanation = "The requested branch is ahead of the installed revision. Herdr's installer may replace the managed checkout and run plugin build commands."
	f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Upstream commits ahead", Value: fmt.Sprint(relation.AheadBy), Source: "public GitHub comparison metadata", ObservedAt: hostNow(h)})
	f.Actions = []ActionProposal{herdrPluginUpdateProposal(h, src, id, f)}
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
		f.Actions = nil
	}
	return []Finding{f}
}

func gitHasWorktreeChanges(status []byte) bool {
	for entry := range bytes.SplitSeq(status, []byte{0}) {
		if len(entry) >= 2 && !bytes.HasPrefix(entry, []byte("!!")) {
			return true
		}
	}
	return false
}

func herdrPluginUpdateProposal(h *Host, src herdrPluginSource, id string, f Finding) ActionProposal {
	target := src.Owner + "/" + src.Repo
	if src.Subdir != "" {
		target += "/" + src.Subdir
	}
	sideEffects := []string{"May replace the managed plugin checkout.", "Executes the plugin's declared build commands as the current user."}
	p := ActionProposal{ID: "update-herdr-plugin", Key: f.Key, Mode: ActionManual, Label: "Review Herdr plugin update", Reason: "Update the validated GitHub source " + target + " only after confirming its installer and build-command effects.", TargetIDs: []string{id}, TargetVersion: Fact{State: EvidenceKnown, Label: "Requested branch revision", Value: sourceFactValue(f, "Requested branch revision"), Source: "public GitHub branch metadata"}, Preconditions: []Fact{{State: EvidenceKnown, Label: "Installed plugin revision", Value: src.ResolvedCommit, Source: "validated Herdr registry"}, {State: EvidenceKnown, Label: "Requested ref", Value: src.RequestedRef, Source: "validated Herdr registry"}}, VerificationCheckID: "herdr-plugins", SideEffects: sideEffects}
	executable, ok := findExecutable(h, "herdr")
	if !ok {
		return p
	}
	args := []string{"plugin", "install", target}
	if src.RequestedRef != "" {
		args = append(args, "--ref", src.RequestedRef)
	}
	args = append(args, "--yes")
	p.Mode = ActionAutomatic
	p.Steps = []CommandStep{{Label: "Install the selected Herdr plugin source", Command: Command{Executable: executable, Args: args}}}
	return p
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func localGitHead(ctx context.Context, h *Host, root string) (string, bool) {
	dir, common, e := gitMetadataLocation(ctx, root)
	if e != nil || !gitInspectionSafe(ctx, root, common, dir) || h == nil || h.RunRead == nil {
		return "", false
	}
	exe, ok := findExecutable(h, "git")
	if !ok {
		return "", false
	}
	env := map[string]string{"GIT_OPTIONAL_LOCKS": "0", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_SYSTEM": "/dev/null", "GIT_CONFIG_COUNT": "0", "GIT_CONFIG_PARAMETERS": "", "GIT_DIR": dir, "GIT_COMMON_DIR": common, "GIT_WORK_TREE": root, "GIT_INDEX_FILE": filepath.Join(dir, "index"), "GIT_NO_REPLACE_OBJECTS": "1", "GIT_NO_LAZY_FETCH": "1", "GIT_TERMINAL_PROMPT": "0"}
	result, e := h.RunRead(ctx, Command{Executable: exe, Dir: root, Args: []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.hooksPath=/dev/null", "rev-parse", "--verify", "HEAD"}, Env: env})
	if e != nil || result.ExitCode != 0 || result.Truncated {
		return "", false
	}
	head := strings.TrimSpace(string(result.Stdout))
	return head, gitObjectID(head)
}

func sourceFactValue(f Finding, label string) string {
	for _, fact := range f.Evidence {
		if fact.Label == label {
			return fact.Value
		}
	}
	return ""
}
func isCommitPin(ref string) bool { return gitObjectID(strings.ToLower(ref)) }

func discoverHerdrIntegrations(ctx context.Context, h *Host, _ Scope) Discovery {
	if ctx.Err() != nil {
		return Discovery{Availability: AvailabilityUndetermined, Diagnostics: []string{"Herdr integration discovery was canceled."}}
	}
	if h == nil {
		return Discovery{Availability: AvailabilityUndetermined}
	}
	sum := sha256.Sum256([]byte(filepath.Clean(filepath.Join(h.Home, ".config", "herdr", "integrations"))))
	identity := fmt.Sprintf("herdr-integr:%x", sum[:])
	i := Instance{ID: identity, IntegrationID: "herdr-integr", Scope: "user", DiscoverySource: "current-user integration targets", Availability: AvailabilityPresent, ObservedAt: hostNow(h), Root: Fact{State: EvidenceKnown, Label: "Integration scope", Value: "current-user Pi and OpenCode target state", Source: "explicit integration targets"}}
	return Discovery{Availability: AvailabilityPresent, Instances: []Instance{i}}
}

func checkHerdrIntegrationTargets(ctx context.Context, h *Host, _ Scope, i Instance) (findings []Finding) {
	defer func() {
		if ctx.Err() != nil {
			for n := range findings {
				findings[n].Outcome = OutcomeCanceled
				findings[n].Actions = nil
			}
		}
	}()
	key := FindingKey{IntegrationID: "herdr-integr", CheckID: "herdr-integr", InstanceID: i.ID}
	if ctx.Err() != nil {
		return []Finding{{Key: key, Outcome: OutcomeCanceled, Question: "Are supported Herdr integration targets current?", Explanation: "Inspection was canceled."}}
	}
	herdr, ok := findExecutable(h, "herdr")
	if !ok {
		return []Finding{{Key: key, Outcome: OutcomeNotApplicable, Question: "Are supported Herdr integration targets current?", Explanation: "Herdr is not discoverable in the selected executable path."}}
	}
	targets := []string{"pi", "opencode"}
	present := map[string]bool{}
	for _, name := range targets {
		_, present[name] = findExecutable(h, name)
	}
	var presentTargets []string
	for _, name := range targets {
		if present[name] {
			presentTargets = append(presentTargets, name)
		}
	}
	if len(presentTargets) == 0 {
		return []Finding{{Key: key, Outcome: OutcomeNotApplicable, Question: "Are supported Herdr integration targets current?", Explanation: "Neither Pi nor OpenCode is discoverable in the selected executable path."}}
	}
	if h == nil || h.RunRead == nil {
		return []Finding{{Key: key, Outcome: OutcomeUnknown, Question: "Are supported Herdr integration targets current?", Explanation: "Herdr integration status cannot be inspected."}}
	}
	commandEnv := map[string]string{"HOME": h.Home}
	for _, name := range []string{"XDG_CONFIG_HOME", "HERDR_CONFIG_PATH"} {
		if value := h.Env[name]; value != "" {
			commandEnv[name] = value
		}
	}
	result, err := h.RunRead(ctx, Command{Executable: herdr, Args: []string{"integration", "status"}, Env: commandEnv})
	if err != nil || result.ExitCode != 0 || result.Truncated {
		return []Finding{{Key: key, Outcome: OutcomeUnknown, Question: "Are supported Herdr integration targets current?", Explanation: "Herdr integration status is unavailable or uses an unsupported format."}}
	}
	statuses := parseHerdrIntegrationStatus(string(result.Stdout))
	if len(statuses) == 0 {
		return []Finding{{Key: key, Outcome: OutcomeUnknown, Question: "Are supported Herdr integration targets current?", Explanation: "Herdr integration status uses an unsupported format."}}
	}
	f := Finding{Key: key, Outcome: OutcomeOK, Question: "Are supported Herdr integration targets current?", Explanation: "Supported target status was read from Herdr's documented status command."}
	for _, name := range presentTargets {
		state, known := statuses[name]
		if !known {
			return []Finding{{Key: key, Outcome: OutcomeUnknown, Question: f.Question, Explanation: "Herdr integration status omitted a selected target."}}
		}
		f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Herdr target status", Value: name + ": " + state, Source: "herdr integration status", ObservedAt: hostNow(h)})
		if state != "current" {
			f.Outcome = OutcomeAttention
		}
	}
	if f.Outcome == OutcomeAttention {
		f.Explanation = "A supported Pi or OpenCode integration is not current. Installation side effects require manual review."
	}
	return []Finding{f}
}

func parseHerdrIntegrationStatus(output string) map[string]string {
	statuses := map[string]string{}
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, state, ok := strings.Cut(line, ":")
		if !ok {
			return nil
		}
		name = strings.TrimSpace(name)
		state = strings.ToLower(strings.TrimSpace(state))
		if name == "" || len(name) > 256 || state == "" || len(state) > 256 {
			return nil
		}
		normalized := ""
		switch {
		case strings.Contains(state, "not current") || strings.Contains(state, "outdated") || strings.Contains(state, "stale"):
			normalized = "outdated"
		case strings.Contains(state, "not installed") || strings.Contains(state, "missing"):
			normalized = "missing"
		case strings.Contains(state, "unsupported"):
			normalized = "unsupported"
		case strings.Contains(state, "current"):
			normalized = "current"
		default:
			return nil
		}
		if _, dup := statuses[name]; dup {
			return nil
		}
		statuses[name] = normalized
	}
	if len(statuses) == 0 {
		return nil
	}
	return statuses
}
