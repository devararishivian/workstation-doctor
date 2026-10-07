package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type serenaVersionChecker struct{}

func (c *serenaVersionChecker) Name() string { return "serena" }

func (c *serenaVersionChecker) Category() Category { return CategoryTool }

func (c *serenaVersionChecker) Check(ctx context.Context) Result {
	return checkSerena(ctx)
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
