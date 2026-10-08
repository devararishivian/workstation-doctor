package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"workstation-doctor/internal/doctor"
)

// PreviewStep represents a safe presentation of a planned step without runnable raw commands.
type PreviewStep struct {
	Label       string
	Description string
}

// Preview represents a safe, immutable display view of a prepared action.
type Preview struct {
	ActionID            string
	IntegrationID       string
	CheckID             string
	InstanceID          string
	Mode                doctor.ActionMode
	Label               string
	Reason              string
	DisabledReason      string
	TargetIDs           []string
	TargetVersion       doctor.Fact
	Steps               []PreviewStep
	Preconditions       []doctor.Fact
	VerificationCheckID string
	SideEffects         []string
}

// PreparedAction holds an immutable, frozen action ready for user confirmation.
type PreparedAction struct {
	actionID    string
	key         doctor.FindingKey
	proposal    doctor.ActionProposal
	fingerprint string
	scope       doctor.Scope
}

// Preview returns an isolated clone of the safe presentation data.
func (p PreparedAction) Preview() Preview {
	steps := make([]PreviewStep, len(p.proposal.Steps))
	for i, step := range p.proposal.Steps {
		steps[i] = PreviewStep{
			Label:       step.Label,
			Description: step.Label, // runnable command is hidden from presentation
		}
	}
	return Preview{
		ActionID:            p.actionID,
		IntegrationID:       p.key.IntegrationID,
		CheckID:             p.key.CheckID,
		InstanceID:          p.key.InstanceID,
		Mode:                p.proposal.Mode,
		Label:               p.proposal.Label,
		Reason:              p.proposal.Reason,
		DisabledReason:      p.proposal.DisabledReason,
		TargetIDs:           slices.Clone(p.proposal.TargetIDs),
		TargetVersion:       p.proposal.TargetVersion,
		Steps:               steps,
		Preconditions:       slices.Clone(p.proposal.Preconditions),
		VerificationCheckID: p.proposal.VerificationCheckID,
		SideEffects:         slices.Clone(p.proposal.SideEffects),
	}
}

// Approval represents a single-use consent token bound to a prepared action fingerprint.
type Approval struct {
	token       string
	fingerprint string
}

func computeActionFingerprint(scope doctor.Scope, key doctor.FindingKey, p doctor.ActionProposal) (string, error) {
	hasher := sha256.New()
	// Scope elements
	hasher.Write([]byte(scope.ProjectDir))
	hasher.Write([]byte{0})
	for _, root := range scope.SkillRoots {
		hasher.Write([]byte(root))
		hasher.Write([]byte{0})
	}
	for _, k := range slices.Sorted(slices.Values(slices.Collect(func(yield func(string) bool) {
		for locKey := range scope.Locations {
			if !yield(locKey) {
				return
			}
		}
	}))) {
		hasher.Write([]byte(k))
		hasher.Write([]byte{0})
		for _, loc := range scope.Locations[k] {
			hasher.Write([]byte(loc))
			hasher.Write([]byte{0})
		}
	}

	// Action identity
	hasher.Write([]byte(key.IntegrationID))
	hasher.Write([]byte{0})
	hasher.Write([]byte(key.CheckID))
	hasher.Write([]byte{0})
	hasher.Write([]byte(key.InstanceID))
	hasher.Write([]byte{0})
	hasher.Write([]byte(p.ID))
	hasher.Write([]byte{0})
	hasher.Write([]byte(p.Mode))
	hasher.Write([]byte{0})
	hasher.Write([]byte(p.Label))
	hasher.Write([]byte{0})
	hasher.Write([]byte(p.TargetVersion.Value))
	hasher.Write([]byte{0})
	hasher.Write([]byte(p.VerificationCheckID))
	hasher.Write([]byte{0})

	// Targets
	for _, tid := range p.TargetIDs {
		hasher.Write([]byte(tid))
		hasher.Write([]byte{0})
	}

	// Steps (executable, dir, args, env)
	for _, step := range p.Steps {
		hasher.Write([]byte(step.Label))
		hasher.Write([]byte{0})
		hasher.Write([]byte(step.Command.Executable))
		hasher.Write([]byte{0})
		hasher.Write([]byte(step.Command.Dir))
		hasher.Write([]byte{0})
		for _, arg := range step.Command.Args {
			hasher.Write([]byte(arg))
			hasher.Write([]byte{0})
		}
		for _, envKey := range slices.Sorted(slices.Values(slices.Collect(func(yield func(string) bool) {
			for ek := range step.Command.Env {
				if !yield(ek) {
					return
				}
			}
		}))) {
			hasher.Write([]byte(envKey))
			hasher.Write([]byte("="))
			hasher.Write([]byte(step.Command.Env[envKey]))
			hasher.Write([]byte{0})
		}
	}

	// Preconditions
	for _, pre := range p.Preconditions {
		hasher.Write([]byte(pre.Label))
		hasher.Write([]byte{0})
		hasher.Write([]byte(pre.Value))
		hasher.Write([]byte{0})
	}

	sum := hasher.Sum(nil)
	if len(sum) == 0 {
		return "", errors.New("failed to hash action fingerprint")
	}
	return hex.EncodeToString(sum), nil
}

func freezeProposal(p doctor.ActionProposal) doctor.ActionProposal {
	frozen := p
	frozen.TargetIDs = slices.Clone(p.TargetIDs)
	frozen.Preconditions = slices.Clone(p.Preconditions)
	frozen.SideEffects = slices.Clone(p.SideEffects)
	frozen.Steps = make([]doctor.CommandStep, len(p.Steps))
	for i, step := range p.Steps {
		frozen.Steps[i] = doctor.CommandStep{
			Label: step.Label,
			Command: doctor.Command{
				Executable: step.Command.Executable,
				Dir:        step.Command.Dir,
				Args:       slices.Clone(step.Command.Args),
				Env:        maps.Clone(step.Command.Env),
			},
		}
	}
	return frozen
}

func freezeScope(s doctor.Scope) doctor.Scope {
	frozen := doctor.Scope{
		ProjectDir: s.ProjectDir,
		Locations:  make(map[string][]string, len(s.Locations)),
		SkillRoots: slices.Clone(s.SkillRoots),
	}
	for k, v := range s.Locations {
		frozen.Locations[k] = slices.Clone(v)
	}
	return frozen
}

func validateFingerprint(prepared PreparedAction) error {
	expected, err := computeActionFingerprint(prepared.scope, prepared.key, prepared.proposal)
	if err != nil {
		return fmt.Errorf("recompute fingerprint: %w", err)
	}
	if !strings.EqualFold(expected, prepared.fingerprint) {
		return errors.New("action fingerprint mismatch or modified")
	}
	return nil
}
