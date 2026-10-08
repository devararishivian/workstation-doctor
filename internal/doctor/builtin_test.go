package doctor

import "testing"

func TestBuiltinToolFindings(t *testing.T) {
	t.Parallel()
	defs := BuiltinDefinitions()
	if err := ValidateDefinitions(defs); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"pi", "opencode", "tokenjuice"} {
		t.Run(id, func(t *testing.T) {
			found := false
			for _, d := range defs {
				if d.Integration.ID == id {
					found = true
					if len(d.Checks) != 1 || d.Checks[0].ID != id || len(d.Integration.References) == 0 {
						t.Fatalf("definition=%+v", d)
					}
				}
			}
			if !found {
				t.Fatalf("missing %s", id)
			}
		})
	}
}
