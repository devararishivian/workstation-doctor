package doctor

import (
	"context"
	"testing"
)

func validTestDefinition() Definition {
	return Definition{
		Integration: Integration{ID: "fixture", Name: "Fixture"},
		Discover:    func(context.Context, *Host, Scope) Discovery { return Discovery{Availability: AvailabilityAbsent} },
		Checks: []CheckDefinition{{
			ID: "version", Name: "Version", Question: "Is it current?", Order: 1,
			Evaluate: func(context.Context, *Host, Scope, Instance) []Finding { return nil },
		}},
	}
}

func TestDefinitionValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		change    func([]Definition) []Definition
		wantError bool
	}{
		{"valid", func(d []Definition) []Definition { return d }, false},
		{"empty registry", func([]Definition) []Definition { return nil }, true},
		{"empty integration", func(d []Definition) []Definition { d[0].Integration.ID = ""; return d }, true},
		{"unsafe identity", func(d []Definition) []Definition { d[0].Integration.ID = "fixture\nsecret"; return d }, true},
		{"empty name", func(d []Definition) []Definition { d[0].Integration.Name = ""; return d }, true},
		{"missing discovery", func(d []Definition) []Definition { d[0].Discover = nil; return d }, true},
		{"missing checks", func(d []Definition) []Definition { d[0].Checks = nil; return d }, true},
		{"empty check", func(d []Definition) []Definition { d[0].Checks[0].ID = ""; return d }, true},
		{"missing evaluator", func(d []Definition) []Definition { d[0].Checks[0].Evaluate = nil; return d }, true},
		{"missing order", func(d []Definition) []Definition { d[0].Checks[0].Order = 0; return d }, true},
		{"duplicate integration", func(d []Definition) []Definition { return append(d, validTestDefinition()) }, true},
		{"duplicate check", func(d []Definition) []Definition {
			c := d[0].Checks[0]
			c.Order = 2
			d[0].Checks = append(d[0].Checks, c)
			return d
		}, true},
		{"duplicate order", func(d []Definition) []Definition {
			other := validTestDefinition()
			other.Integration.ID = "other"
			return append(d, other)
		}, true},
		{"same check different integrations", func(d []Definition) []Definition {
			other := validTestDefinition()
			other.Integration.ID = "other"
			other.Checks[0].Order = 2
			return append(d, other)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDefinitions(tt.change([]Definition{validTestDefinition()}))
			if (err != nil) != tt.wantError {
				t.Fatalf("validation error = %v, want error %t", err, tt.wantError)
			}
		})
	}
}
