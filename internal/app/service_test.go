package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"
)

func syntheticServiceWithProposals(t *testing.T, stateDir string, openCalls *atomic.Int32, proposals ...doctor.ActionProposal) (*Service, doctor.FindingKey, doctor.ActionProposal) {
	t.Helper()
	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "update", InstanceID: "fixture-instance"}
	var activeProposals []doctor.ActionProposal
	if len(proposals) == 0 {
		activeProposals = []doctor.ActionProposal{{
			ID: "safe-update", Key: key, Mode: doctor.ActionAutomatic, Label: "Update fixture", Reason: "A fixture update is available.",
			TargetIDs: []string{"fixture-target"}, TargetVersion: doctor.Fact{State: doctor.EvidenceKnown, Value: "2.0", Source: "fixture"},
			Steps:               []doctor.CommandStep{{Label: "Update", Command: doctor.Command{Executable: "/usr/bin/true", Args: []string{"--version"}, Dir: "/tmp"}}},
			Preconditions:       []doctor.Fact{{State: doctor.EvidenceKnown, Label: "installed version", Value: "1.0", Source: "fixture"}},
			VerificationCheckID: "verify", SideEffects: []string{"Changes the selected fixture package."},
		}}
	} else {
		activeProposals = proposals
	}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Synthetic test integration."},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{ID: key.InstanceID, IntegrationID: key.IntegrationID, Availability: doctor.AvailabilityPresent, Version: doctor.Fact{State: doctor.EvidenceKnown, Value: "1.0", Source: "fixture"}, Provenance: doctor.Provenance{State: doctor.EvidenceKnown, Manager: "fixture-manager", Package: "fixture-pkg", Root: "/tmp/fixture"}}}}
		},
		Checks: []doctor.CheckDefinition{
			{ID: "update", Name: "Update", Question: "Is an update available?", Order: 1, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Outcome: doctor.OutcomeAttention, Explanation: "An update is available.", Actions: activeProposals}}
			}},
			{ID: "verify", Name: "Verify", Question: "Is the target version installed?", Order: 2, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Outcome: doctor.OutcomeOK, Explanation: "The target version is installed."}}
			}},
		},
	}}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(engine, host, doctor.Scope{}, Options{
		Version: "test", DBPath: "/tmp/history.db", StateDir: stateDir, Policy: store.DefaultPolicy(),
		OpenHistory: func(context.Context, string, store.Policy) (History, error) {
			openCalls.Add(1)
			return nil, errors.New("unexpected history open")
		},
	})
	return service, key, activeProposals[0]
}

func syntheticService(t *testing.T, stateDir string, openCalls *atomic.Int32) (*Service, doctor.FindingKey, doctor.ActionProposal) {
	return syntheticServiceWithProposals(t, stateDir, openCalls)
}

func TestServiceAuditNoHistory(t *testing.T) {
	var opens atomic.Int32
	service, _, _ := syntheticService(t, t.TempDir(), &opens)
	_ = service.Audit(t.Context())
	_ = service.Current()
	if opens.Load() != 0 {
		t.Fatalf("read-only service opened history %d times", opens.Load())
	}
}

func TestCurrentSnapshotIsolation(t *testing.T) {
	var opens atomic.Int32
	service, _, _ := syntheticService(t, t.TempDir(), &opens)
	first := service.Audit(t.Context())
	first.Scope.Locations["fixture"] = []string{"changed"}
	first.Findings[0].Actions[0].Steps[0].Command.Args[0] = "changed"
	second := service.Current()
	if got := second.Findings[0].Actions[0].Steps[0].Command.Args[0]; got != "--version" {
		t.Fatalf("Current exposed mutable command args %q", got)
	}
	if got := second.Scope.Locations["fixture"]; len(got) != 0 {
		t.Fatalf("Current exposed mutable scope: %v", got)
	}
}

func TestPrepareFrozenScope(t *testing.T) {
	var opens atomic.Int32
	service, key, _ := syntheticService(t, t.TempDir(), &opens)
	_ = service.Audit(t.Context())
	prepared, err := service.Prepare(t.Context(), key, "safe-update")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	preview := prepared.Preview()
	if preview.ActionID != "safe-update" || preview.Label != "Update fixture" || len(preview.TargetIDs) != 1 || len(preview.Steps) != 1 {
		t.Fatalf("unexpected frozen preview: %+v", preview)
	}
	preview.TargetIDs[0] = "mutated"
	preview.Steps[0].Label = "mutated"
	again := prepared.Preview()
	if again.TargetIDs[0] != "fixture-target" || again.Steps[0].Label != "Update" {
		t.Fatalf("Preview exposed mutable prepared data: %+v", again)
	}
}

func TestConfirmationBinding(t *testing.T) {
	var opens atomic.Int32
	_, _, p1 := syntheticService(t, t.TempDir(), &opens)
	p2 := p1
	p2.ID = "safe-update-2"
	p2.TargetIDs = []string{"fixture-target-2"}
	service, key, _ := syntheticServiceWithProposals(t, t.TempDir(), &opens, p1, p2)

	prepared, err := service.Prepare(t.Context(), key, "safe-update")
	if err != nil {
		t.Fatal(err)
	}
	approval, err := service.Confirm(prepared)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if _, err := service.Confirm(prepared); err == nil {
		t.Fatal("Confirm issued multiple approvals for one prepared action")
	}
	if err := service.consumeApproval(prepared, approval); err != nil {
		t.Fatalf("first approval consumption: %v", err)
	}
	if err := service.consumeApproval(prepared, approval); err == nil {
		t.Fatal("reused approval was accepted")
	}
	other, err := service.Prepare(t.Context(), key, "safe-update-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.consumeApproval(other, approval); err == nil {
		t.Fatal("approval was accepted for a different action fingerprint")
	}
}

func TestPrepareRejectsStaleFingerprint(t *testing.T) {
	var opens atomic.Int32
	service, key, _ := syntheticService(t, t.TempDir(), &opens)
	first, err := service.Prepare(t.Context(), key, "safe-update")
	if err != nil {
		t.Fatal(err)
	}
	if first.fingerprint == "" {
		t.Fatal("prepared action has no fingerprint")
	}
	first.fingerprint = "stale"
	if _, err := service.Confirm(first); err == nil {
		t.Fatal("confirmation accepted a modified prepared fingerprint")
	}
}

func TestServiceAuditSnapshotCanBeRepeated(t *testing.T) {
	var opens atomic.Int32
	service, _, _ := syntheticService(t, t.TempDir(), &opens)
	for range 3 {
		_ = service.Audit(t.Context())
	}
	if report := service.Current(); report.FinishedAt.IsZero() {
		t.Fatal("Current did not return latest audit snapshot")
	}
}

var _ = time.Second
