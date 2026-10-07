package doctor

import (
	"context"
	"os"
	"path/filepath"
)

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

type piPackagesChecker struct{}

func (c *piPackagesChecker) Name() string { return "pi-packages" }

func (c *piPackagesChecker) Category() Category { return CategorySystem }

func (c *piPackagesChecker) Check(ctx context.Context) Result {
	return checkPiPackages(ctx)
}
