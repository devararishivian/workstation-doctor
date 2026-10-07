package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

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

type herdrPluginsChecker struct{}

func (c *herdrPluginsChecker) Name() string { return "herdr-plugins" }

func (c *herdrPluginsChecker) Category() Category { return CategorySystem }

func (c *herdrPluginsChecker) Check(ctx context.Context) Result {
	return checkHerdrPlugins(ctx)
}
