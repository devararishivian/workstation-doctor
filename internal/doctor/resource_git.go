package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// gitMetadataLocation accepts ordinary repositories and explicit worktree gitfiles.
func gitMetadataLocation(ctx context.Context, root string) (string, string, error) {
	dir := filepath.Join(root, ".git")
	info, e := os.Stat(dir)
	if e != nil {
		return "", "", fmt.Errorf("inspect checkout metadata: %w", e)
	}
	if !info.IsDir() {
		raw, e := ReadBounded(ctx, dir, DefaultLimits().MaxFileBytes)
		if e != nil {
			return "", "", e
		}
		p, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
		if !ok {
			return "", "", errors.New("unsupported gitfile")
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		dir = filepath.Clean(p)
	}
	common := dir
	raw, e := ReadBounded(ctx, filepath.Join(dir, "commondir"), 4096)
	if e == nil {
		p := strings.TrimSpace(string(raw))
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		common = filepath.Clean(p)
	} else if !errors.Is(e, os.ErrNotExist) {
		return "", "", e
	}
	return dir, common, nil
}

func gitInspectionSafe(ctx context.Context, root, common, dir string) bool {
	raw, e := ReadBounded(ctx, filepath.Join(common, "config"), DefaultLimits().MaxFileBytes)
	if e != nil {
		return false
	}
	content := strings.ToLower(string(raw))
	// Configured filters, includes, submodules, alternate worktree configuration and
	// partial clones can execute code or lazily fetch. Refuse rather than evaluate them.
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, "["); ok {
			section := strings.TrimSpace(rest)
			for _, unsafeSection := range []string{"include", "filter", "submodule"} {
				if strings.HasPrefix(section, unsafeSection) {
					return false
				}
			}
		}
		for _, word := range []string{"promisor", "partialclone", "worktreeconfig"} {
			if strings.Contains(trimmed, word) {
				return false
			}
		}
	}
	for _, p := range []string{filepath.Join(root, ".gitmodules"), filepath.Join(dir, "config.worktree"), filepath.Join(common, "objects", "info", "alternates")} {
		if _, e := os.Stat(p); !errors.Is(e, os.ErrNotExist) {
			return false
		}
	}
	return true
}

func localGitStatus(ctx context.Context, h *Host, root string) ([]byte, bool) {
	if h == nil || h.RunRead == nil || !filepath.IsAbs(root) {
		return nil, false
	}
	dir, common, e := gitMetadataLocation(ctx, root)
	if e != nil || !gitInspectionSafe(ctx, root, common, dir) {
		return nil, false
	}
	exe, ok := findExecutable(h, "git")
	if !ok {
		return nil, false
	}
	args := []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.hooksPath=/dev/null", "-c", "core.pager=cat", "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=matching", "--ignore-submodules=all"}
	result, e := h.RunRead(ctx, Command{Executable: exe, Dir: root, Args: args, Env: map[string]string{"GIT_OPTIONAL_LOCKS": "0", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_SYSTEM": "/dev/null", "GIT_CONFIG_COUNT": "0", "GIT_CONFIG_PARAMETERS": "", "GIT_DIR": dir, "GIT_COMMON_DIR": common, "GIT_WORK_TREE": root, "GIT_INDEX_FILE": filepath.Join(dir, "index"), "GIT_NO_REPLACE_OBJECTS": "1", "GIT_NO_LAZY_FETCH": "1", "GIT_TERMINAL_PROMPT": "0"}})
	if e != nil || result.ExitCode != 0 || result.Truncated {
		return nil, false
	}
	return result.Stdout, true
}

func inspectGitResource(ctx context.Context, h *Host, _ Instance, r ResourceSource, f Finding) Finding {
	if r.Pinned {
		f.Outcome = OutcomeNotApplicable
		f.Explanation = "The requested Git ref is constrained; automatic advancement is disabled."
		return f
	}
	if h == nil || h.RunRead == nil {
		return f
	}
	dir, common, e := gitMetadataLocation(ctx, r.Path)
	if e != nil || !gitInspectionSafe(ctx, r.Path, common, dir) {
		f.Explanation = "Git inspection safety or checkout metadata is unsupported; no Git command ran."
		return f
	}
	exe, ok := findExecutable(h, "git")
	if !ok {
		return f
	}
	run := func(args ...string) (CommandResult, error) {
		return h.RunRead(ctx, Command{Executable: exe, Dir: r.Path, Args: append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.hooksPath=/dev/null", "-c", "core.pager=cat"}, args...), Env: map[string]string{"GIT_OPTIONAL_LOCKS": "0", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_SYSTEM": "/dev/null", "GIT_CONFIG_COUNT": "0", "GIT_CONFIG_PARAMETERS": "", "GIT_DIR": dir, "GIT_COMMON_DIR": common, "GIT_WORK_TREE": r.Path, "GIT_INDEX_FILE": filepath.Join(dir, "index"), "GIT_NO_REPLACE_OBJECTS": "1", "GIT_NO_LAZY_FETCH": "1", "GIT_TERMINAL_PROMPT": "0"}})
	}
	head, e := run("rev-parse", "--verify", "HEAD")
	local := strings.TrimSpace(string(head.Stdout))
	if e != nil || head.Truncated || !gitObjectID(local) {
		return f
	}
	f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Installed Git revision", Value: local, Source: "local Git HEAD", ObservedAt: hostNow(h)})
	branch, e := run("symbolic-ref", "--short", "HEAD")
	name := strings.TrimSpace(string(branch.Stdout))
	if e != nil || branch.Truncated || !validText(name, 256) || name == "" {
		f.Explanation = "Detached or unreadable Git branch prevents automatic advancement."
		return f
	}
	f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Current Git branch", Value: name, Source: "local Git metadata", ObservedAt: hostNow(h)})
	status, e := run("status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=all")
	if e != nil || status.Truncated {
		return f
	}
	if len(status.Stdout) > 0 {
		f.Outcome = OutcomeAttention
		f.Explanation = "The checkout contains tracked or untracked changes. No update is proposed."
		return f
	}
	targetRef := r.RequestedRef
	if targetRef == "" {
		if up, upErr := run("rev-parse", "--abbrev-ref", "@{upstream}"); upErr == nil && up.ExitCode == 0 && !up.Truncated {
			u := strings.TrimSpace(string(up.Stdout))
			if _, after, ok := strings.Cut(u, "/"); ok {
				targetRef = after
			}
		}
		if targetRef == "" {
			if sym, symErr := run("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); symErr == nil && sym.ExitCode == 0 && !sym.Truncated {
				u := strings.TrimSpace(string(sym.Stdout))
				if _, after, ok := strings.Cut(u, "/"); ok {
					targetRef = after
				}
			}
		}
		if targetRef == "" {
			if refCheck, rcErr := run("rev-parse", "--verify", "refs/remotes/origin/"+name); rcErr == nil && refCheck.ExitCode == 0 && !refCheck.Truncated {
				targetRef = name
			}
		}
		if targetRef == "" && name != "" {
			targetRef = name
		}
	}

	if targetRef == "" || (r.RequestedRef != "" && r.RequestedRef != name) {
		f.Explanation = "Requested tracking branch is not established; no default remote branch is guessed."
		return f
	}
	parts := strings.Split(r.Identity, "/")
	if len(parts) != 3 || parts[0] != "github.com" || h.Fetch == nil {
		return f
	}
	remote, e := h.Fetch(ctx, "https://api.github.com/repos/"+parts[1]+"/"+parts[2]+"/commits/"+url.PathEscape(targetRef))
	if e != nil || len(remote) > int(DefaultLimits().MaxHTTPBytes) {
		f.Explanation = "Remote lookup is unavailable. Local revision evidence is retained."
		return f
	}
	var metadata struct{ SHA string }
	if json.Unmarshal(remote, &metadata) != nil || !gitObjectID(metadata.SHA) {
		f.Explanation = "Remote repository returned unsupported metadata."
		return f
	}
	f.Evidence = append(f.Evidence, Fact{State: EvidenceKnown, Label: "Requested remote revision", Value: metadata.SHA, Source: "public GitHub commit metadata", ObservedAt: hostNow(h)})
	if local == metadata.SHA {
		f.Outcome = OutcomeOK
		f.Explanation = "Clean checkout matches the requested remote revision."
		return f
	}
	// Without fetching, only objects already present locally establish ancestry.
	ancestor, e := run("merge-base", "--is-ancestor", local, metadata.SHA)
	if e == nil && ancestor.ExitCode == 0 && !ancestor.Truncated {
		f.Outcome = OutcomeAttention
		f.Explanation = "Local ancestry establishes an upstream advancement. No supported automatic updater is established."
	} else {
		f.Explanation = "Revisions differ, but missing ancestry, aheadness or divergence prevents an established update. No fetch or pull ran."
	}
	if ctx.Err() != nil {
		f.Outcome = OutcomeCanceled
	}
	return f
}

func gitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
