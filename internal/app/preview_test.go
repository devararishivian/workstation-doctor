package app

import (
	"testing"

	"github.com/devararishivian/workstation-doctor/internal/doctor"
)

func TestPreviewFrozenScope(t *testing.T) {
	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "update", InstanceID: "fixture-instance"}
	proposal := doctor.ActionProposal{
		ID:                  "safe-update",
		Key:                 key,
		Mode:                doctor.ActionAutomatic,
		Label:               "Update fixture",
		Reason:              "A fixture update is available.",
		TargetIDs:           []string{"fixture-target"},
		TargetVersion:       doctor.Fact{State: doctor.EvidenceKnown, Value: "2.0", Source: "fixture"},
		Steps:               []doctor.CommandStep{{Label: "Update", Command: doctor.Command{Executable: "/usr/bin/true", Args: []string{"--version"}, Dir: "/tmp"}}},
		Preconditions:       []doctor.Fact{{State: doctor.EvidenceKnown, Label: "installed version", Value: "1.0", Source: "fixture"}},
		VerificationCheckID: "verify",
		SideEffects:         []string{"Changes the selected fixture package."},
	}
	scope := doctor.Scope{
		ProjectDir: "/tmp/project",
		Locations:  map[string][]string{"fixture": {"/tmp/loc"}},
	}
	prepared := PreparedAction{
		actionID: "safe-update",
		key:      key,
		proposal: freezeProposal(proposal),
		scope:    freezeScope(scope),
	}

	preview := prepared.Preview()
	if preview.ActionID != "safe-update" || preview.Label != "Update fixture" || len(preview.TargetIDs) != 1 || len(preview.Steps) != 1 {
		t.Fatalf("unexpected frozen preview: %+v", preview)
	}

	// Verify step description does not expose raw command execution details
	if preview.Steps[0].Description != "Update" {
		t.Fatalf("step description exposed raw data: %q", preview.Steps[0].Description)
	}

	// Mutating preview does not affect another preview
	preview.TargetIDs[0] = "mutated"
	preview.Steps[0].Label = "mutated"
	again := prepared.Preview()
	if again.TargetIDs[0] != "fixture-target" || again.Steps[0].Label != "Update" {
		t.Fatalf("Preview exposed mutable prepared data: %+v", again)
	}
}

func TestPrepareDeduplicatesAndRejectsOverlappingTargets(t *testing.T) {
	limits := doctor.DefaultLimits()
	invalid := doctor.ActionProposal{
		ID: "dup-target", Key: doctor.FindingKey{IntegrationID: "fixture", CheckID: "update", InstanceID: "fixture-instance"},
		Mode: doctor.ActionAutomatic, Label: "Duplicate targets", Reason: "Test duplicate",
		TargetIDs: []string{"target-a", "target-a"}, VerificationCheckID: "verify",
		Steps: []doctor.CommandStep{{Label: "Run", Command: doctor.Command{Executable: "/usr/bin/true", Dir: "/tmp"}}},
	}
	if err := doctor.ValidateProposal(invalid, limits); err == nil {
		t.Fatal("ValidateProposal allowed duplicate target IDs")
	}
}

func TestPrepareRefusesPrivilegeEscalation(t *testing.T) {
	limits := doctor.DefaultLimits()
	for _, bin := range []string{"/bin/sh", "/usr/bin/sudo", "/usr/bin/bash", "/usr/bin/doas"} {
		bad := doctor.ActionProposal{
			ID: "bad-exec", Key: doctor.FindingKey{IntegrationID: "fixture", CheckID: "update", InstanceID: "fixture-instance"},
			Mode: doctor.ActionAutomatic, Label: "Unsafe", Reason: "Unsafe", TargetIDs: []string{"t1"},
			Steps:               []doctor.CommandStep{{Label: "Run", Command: doctor.Command{Executable: bin, Dir: "/tmp"}}},
			VerificationCheckID: "verify",
		}
		if err := doctor.ValidateProposal(bad, limits); err == nil {
			t.Fatalf("ValidateProposal accepted prohibited executable %q", bin)
		}
	}
}
