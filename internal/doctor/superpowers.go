package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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

type superpowersChecker struct{}

func (c *superpowersChecker) Name() string { return "superpowers" }

func (c *superpowersChecker) Category() Category { return CategorySystem }

func (c *superpowersChecker) Check(ctx context.Context) Result {
	return checkSuperpowers(ctx)
}
