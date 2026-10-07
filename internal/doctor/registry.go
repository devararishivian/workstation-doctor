package doctor

import (
	"context"
	"errors"
	"fmt"
)

// CheckDefinition registers a focused algorithm and authoritative metadata.
type CheckDefinition struct {
	ID, Name, Question string
	Order              int
	Evaluate           func(context.Context, *Host, Scope, Instance) []Finding
}

// Definition explicitly registers one integration, not its installed instances.
type Definition struct {
	Integration Integration
	Discover    func(context.Context, *Host, Scope) Discovery
	Checks      []CheckDefinition
}

// ValidateDefinitions rejects ambiguous identities, missing algorithms, and order.
func ValidateDefinitions(defs []Definition) error {
	if len(defs) == 0 {
		return errors.New("registry requires at least one integration")
	}
	limits := DefaultLimits()
	integrations := map[string]bool{}
	orders := map[int]bool{}
	for _, def := range defs {
		id := def.Integration.ID
		if !validIdentifier(id, limits.MaxIdentifierBytes) {
			return errors.New("integration identity is invalid")
		}
		if integrations[id] {
			return fmt.Errorf("duplicate integration identity %q", id)
		}
		integrations[id] = true
		if def.Integration.Name == "" || !validText(def.Integration.Name, limits.MaxLabelBytes) || !validText(def.Integration.Description, limits.MaxNoteBytes) {
			return fmt.Errorf("integration %q has invalid display metadata", id)
		}
		if def.Discover == nil || len(def.Checks) == 0 {
			return fmt.Errorf("integration %q requires discovery and checks", id)
		}
		checks := map[string]bool{}
		for _, check := range def.Checks {
			if !validIdentifier(check.ID, limits.MaxIdentifierBytes) || check.Name == "" || check.Question == "" ||
				!validText(check.Name, limits.MaxLabelBytes) || !validText(check.Question, limits.MaxNoteBytes) {
				return fmt.Errorf("integration %q has invalid check metadata", id)
			}
			if checks[check.ID] {
				return fmt.Errorf("integration %q has duplicate check %q", id, check.ID)
			}
			checks[check.ID] = true
			if check.Evaluate == nil {
				return fmt.Errorf("check %q requires an evaluator", check.ID)
			}
			if check.Order <= 0 || orders[check.Order] {
				return fmt.Errorf("check %q requires a unique positive order", check.ID)
			}
			orders[check.Order] = true
		}
	}
	return nil
}
