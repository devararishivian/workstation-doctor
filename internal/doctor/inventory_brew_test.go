package doctor

import (
	"context"
	"testing"
)

func TestBrewFormulaNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"python@3.14", "gtk+3"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			host, _ := syntheticBrewTool(t, "tool", name, "homebrew/core")
			host.RunRead = nil
			inventory, err := BrewInventory(t.Context(), host, Scope{})
			if err != nil || len(inventory.Items) != 1 || inventory.Items[0].Package != "homebrew/core/"+name {
				t.Fatalf("inventory=%+v error=%v", inventory, err)
			}
			d := discoverBrew(t.Context(), host, Scope{})
			if len(d.Instances) != 1 {
				t.Fatalf("discovery=%+v", d)
			}
			host.Fetch = func(context.Context, string) ([]byte, error) {
				return []byte(`{"name":"` + name + `","versions":{"stable":"1.2.4"}}`), nil
			}
			f := checkBrewInventory(t.Context(), host, Scope{}, d.Instances[0])[0]
			if len(f.Evidence) != 3 || f.Evidence[2].Value != "1.2.4" {
				t.Fatalf("published reference=%+v", f)
			}
			assertNoAutomatic(t, f)
		})
	}
}
