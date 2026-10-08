package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNpmGlobalScope(t *testing.T) {
	t.Parallel()
	host, _ := syntheticNpmTool(t, "tokenjuice", "tokenjuice")
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"dependencies":{"local-only":"9.9.9"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	scope := Scope{ProjectDir: project}
	d := discoverNpm(t.Context(), host, scope)
	if len(d.Instances) != 1 || d.Instances[0].Provenance.Package != "tokenjuice" {
		t.Fatalf("global discovery=%+v", d)
	}
	i := d.Instances[0]
	f := checkNpmGlobal(t.Context(), host, scope, i)[0]
	if f.Outcome != OutcomeAttention || f.Evidence[0].Value != "1.2.3" {
		t.Fatalf("global finding=%+v", f)
	}
	assertNpmProposal(t, host, f, "tokenjuice@1.2.4")
	tool := discoverTokenjuice(t.Context(), host, scope).Instances[0]
	specific := checkTokenjuiceInstance(t.Context(), host, scope, tool)[0]
	if specific.Actions[0].TargetIDs[0] != f.Actions[0].TargetIDs[0] {
		t.Fatal("aggregate and tool update target identities differ")
	}
	inventory, err := NpmInventory(t.Context(), host, scope)
	if err != nil {
		t.Fatal(err)
	}
	inventory.Items[0].RequestedPin = "1.2.3"
	if err := cacheInventory(host.inventory, scope, inventory); err != nil {
		t.Fatal(err)
	}
	assertNoAutomatic(t, checkNpmGlobal(t.Context(), host, scope, i)[0])
	host.Fetch = func(context.Context, string) ([]byte, error) { return []byte(`{"version":"1.2.3"}`), nil }
	pinned := checkNpmGlobal(t.Context(), host, scope, i)[0]
	if pinned.Outcome == OutcomeOK {
		t.Fatal("pin constraints were silently classified as globally current")
	}
}
