package doctor

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateProposal rejects invalid executable scope rather than truncating it.
func ValidateProposal(p ActionProposal, limits Limits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	for _, id := range []string{p.ID, p.Key.IntegrationID, p.Key.CheckID, p.Key.InstanceID} {
		if !validIdentifier(id, limits.MaxIdentifierBytes) {
			return errors.New("action identity is invalid")
		}
	}
	if p.Label == "" || !validText(p.Label, limits.MaxLabelBytes) || !validText(p.Reason, limits.MaxNoteBytes) || !validText(p.DisabledReason, limits.MaxNoteBytes) {
		return errors.New("action display metadata is invalid")
	}
	if len(p.Steps) > limits.MaxSteps || len(p.TargetIDs) > limits.MaxTargets {
		return errors.New("action exceeds step or target limits; select a smaller batch")
	}
	switch p.Mode {
	case ActionManual:
		if len(p.Steps) > 0 {
			return errors.New("manual guidance cannot contain executable steps")
		}
		return nil
	case ActionAutomatic, ActionInspection:
	default:
		return errors.New("action mode is unspecified or unsupported")
	}
	if len(p.Steps) == 0 || len(p.TargetIDs) == 0 {
		return errors.New("executable action requires steps and explicit targets")
	}
	if p.Mode == ActionAutomatic && !validIdentifier(p.VerificationCheckID, limits.MaxIdentifierBytes) {
		return errors.New("automatic action requires registered verification")
	}
	seen := map[string]bool{}
	for _, id := range p.TargetIDs {
		if !validIdentifier(id, limits.MaxIdentifierBytes) || seen[id] {
			return errors.New("action targets are invalid or duplicated")
		}
		seen[id] = true
	}
	for _, step := range p.Steps {
		if step.Label == "" || !validText(step.Label, limits.MaxLabelBytes) {
			return errors.New("action step label is invalid")
		}
		if err := validateCommand(step.Command, limits); err != nil {
			return err
		}
	}
	for _, effect := range p.SideEffects {
		if !validText(effect, limits.MaxNoteBytes) {
			return errors.New("action side-effect description is invalid")
		}
	}
	return nil
}

func validateCommand(command Command, limits Limits) error {
	if !filepath.IsAbs(command.Executable) || !validText(command.Executable, limits.MaxPathBytes) {
		return errors.New("command requires a bounded absolute executable")
	}
	if command.Dir != "" && (!filepath.IsAbs(command.Dir) || !validText(command.Dir, limits.MaxPathBytes)) {
		return errors.New("command directory must be absolute and bounded")
	}
	switch filepath.Base(command.Executable) {
	case "sh", "bash", "dash", "zsh", "fish", "sudo", "doas":
		return errors.New("shell interpretation and implicit privilege escalation are unsupported")
	}
	if len(command.Args) > limits.MaxArguments || len(command.Env) > limits.MaxArguments {
		return errors.New("command exceeds argument or environment limits")
	}
	remaining := limits.MaxArgumentBytes
	for _, arg := range command.Args {
		if strings.ContainsRune(arg, 0) || len(arg) > remaining {
			return errors.New("command argument bytes exceed safe limits")
		}
		remaining -= len(arg)
	}
	remaining = limits.MaxArgumentBytes
	for key, value := range command.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) || len(key) > remaining {
			return errors.New("command environment is invalid or oversized")
		}
		remaining -= len(key)
		if len(value) > remaining {
			return errors.New("command environment is oversized")
		}
		remaining -= len(value)
	}
	if command.Executable == "" {
		return fmt.Errorf("command executable is empty")
	}
	return nil
}
