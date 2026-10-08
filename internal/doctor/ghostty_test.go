package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGhosttyCandidates(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	root := filepath.Join(home, "Applications", "Ghostty.app", "Contents")
	if err := os.MkdirAll(filepath.Join(root, "MacOS"), 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "MacOS", "ghostty")
	if err := os.WriteFile(exe, []byte("synthetic; never execute"), 0o700); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.mitchellh.ghostty</string><key>CFBundleShortVersionString</key><string>1.2.3</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(root, "Info.plist"), []byte(plist), 0o600); err != nil {
		t.Fatal(err)
	}
	host := &Host{OS: "darwin", Home: home, Path: t.TempDir(), Env: map[string]string{}}
	host.RunRead = func(context.Context, Command) (CommandResult, error) {
		t.Error("app discovery executed a process")
		return CommandResult{}, errors.New("forbidden")
	}
	// Explicit candidate keeps this test away from real system applications.
	scope := Scope{Locations: map[string][]string{"ghostty": {exe}}}
	d := discoverGhostty(t.Context(), host, scope)
	if d.Availability != AvailabilityPresent || len(d.Instances) != 1 || d.Instances[0].Active || d.Instances[0].Version.Value != "1.2.3" || d.Instances[0].Provenance.State == EvidenceKnown {
		t.Fatalf("inactive app=%+v", d)
	}
	host.Fetch = func(context.Context, string) ([]byte, error) {
		return []byte(`{"tag_name":"v1.2.4","html_url":"https://github.com/ghostty-org/ghostty/releases/tag/v1.2.4","prerelease":false,"draft":false}`), nil
	}
	f := checkGhosttyInstance(t.Context(), host, scope, d.Instances[0])[0]
	if f.Outcome != OutcomeAttention || len(f.References) < 2 {
		t.Fatalf("release=%+v", f)
	}
	assertNoAutomatic(t, f)
	// Linux does not inherit macOS app candidates.
	host.OS = "linux"
	host.Path = filepath.Join(root, "MacOS")
	d = discoverGhostty(t.Context(), host, Scope{})
	if len(d.Instances) != 1 || !d.Instances[0].Active {
		t.Fatalf("PATH=%+v", d)
	}
}
