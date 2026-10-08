package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"workstation-doctor/internal/app"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"
)

func syntheticServiceForMenu(t *testing.T) (*app.Service, *atomic.Int32) {
	t.Helper()
	var openCalls atomic.Int32
	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "check", InstanceID: "inst"}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
			}}}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeOK, Explanation: "Fixture check OK"}}
			},
		}},
	}}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   t.TempDir() + "/db.sqlite",
		OpenHistory: func(context.Context, string, store.Policy) (app.History, error) {
			openCalls.Add(1)
			return nil, errors.New("open history unexpected")
		},
	})
	return service, &openCalls
}

func TestMenuAuditUsesService(t *testing.T) {
	service, openCalls := syntheticServiceForMenu(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d := newDoctorApp(ctx, service)
	if d.service != service {
		t.Fatal("doctorApp does not retain service")
	}

	d.startAudit()

	// Wait for audit to complete
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.mode.Get() == "results" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if d.mode.Get() != "results" {
		t.Fatalf("expected mode results after audit, got %q", d.mode.Get())
	}
	report := d.report.Get()
	if len(report.Findings) != 1 || report.Findings[0].Outcome != doctor.OutcomeOK {
		t.Fatalf("expected 1 OK finding from service, got %+v", report)
	}
	if openCalls.Load() != 0 {
		t.Fatalf("audit opened history %d times", openCalls.Load())
	}
}

func TestMenuGenerationGuardsLateResults(t *testing.T) {
	// First audit blocks until unblocked; second audit runs immediately
	blockFirst := make(chan struct{})
	firstStarted := make(chan struct{})
	var auditCount atomic.Int32

	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "check", InstanceID: "inst"}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
			}}}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(_ context.Context, _ *doctor.Host, _ doctor.Scope, _ doctor.Instance) []doctor.Finding {
				count := auditCount.Add(1)
				if count == 1 {
					close(firstStarted)
					<-blockFirst
					return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeAttention, Explanation: "Slow first audit"}}
				}
				return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeOK, Explanation: "Fast second audit"}}
			},
		}},
	}}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   t.TempDir() + "/db.sqlite",
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := newDoctorApp(ctx, service)

	// Start first audit (slow)
	d.startAudit()
	<-firstStarted

	// Start second audit (fast)
	d.startAudit()

	// Wait for second audit to complete
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.mode.Get() == "results" && len(d.report.Get().Findings) > 0 && d.report.Get().Findings[0].Outcome == doctor.OutcomeOK {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if d.report.Get().Findings[0].Outcome != doctor.OutcomeOK {
		t.Fatalf("expected second audit outcome OK, got %v", d.report.Get().Findings[0].Outcome)
	}

	// Now unblock the first audit. Since its generation is older, it must not overwrite the second audit results!
	close(blockFirst)
	time.Sleep(50 * time.Millisecond)

	if d.report.Get().Findings[0].Outcome != doctor.OutcomeOK {
		t.Fatalf("late first audit overwrote newer generation results: %+v", d.report.Get().Findings[0])
	}
}

func TestMenuQuitCancelsWork(t *testing.T) {
	blockAudit := make(chan struct{})
	auditStarted := make(chan struct{})

	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "check", InstanceID: "inst"}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
			}}}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(ctx context.Context, _ *doctor.Host, _ doctor.Scope, _ doctor.Instance) []doctor.Finding {
				close(auditStarted)
				select {
				case <-ctx.Done():
					return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeCanceled}}
				case <-blockAudit:
					return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeOK}}
				}
			},
		}},
	}}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   t.TempDir() + "/db.sqlite",
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := newDoctorApp(ctx, service)

	d.startAudit()
	<-auditStarted

	// Calling stop() must cancel running audit work promptly
	d.stop()

	select {
	case <-d.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("doctorApp context was not canceled on stop()")
	}
}

func TestMenuActionPreview(t *testing.T) {
	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "update", InstanceID: "inst"}
	proposal := doctor.ActionProposal{
		ID:                  "fix-1",
		Key:                 key,
		Mode:                doctor.ActionAutomatic,
		Label:               "Upgrade fixture",
		Reason:              "Outdated component",
		TargetIDs:           []string{"target-1"},
		TargetVersion:       doctor.Fact{State: doctor.EvidenceKnown, Value: "2.0.0"},
		Steps:               []doctor.CommandStep{{Label: "Run upgrade", Command: doctor.Command{Executable: "/usr/bin/true", Dir: "/tmp"}}},
		Preconditions:       []doctor.Fact{{State: doctor.EvidenceKnown, Label: "installed", Value: "1.0.0"}},
		VerificationCheckID: "verify-check",
		SideEffects:         []string{"Will update local files"},
	}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
				Version: doctor.Fact{State: doctor.EvidenceKnown, Value: "1.0.0"},
			}}}
		},
		Checks: []doctor.CheckDefinition{
			{ID: "update", Name: "Update", Question: "Update?", Order: 1, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeAttention, Actions: []doctor.ActionProposal{proposal}}}
			}},
			{ID: "verify-check", Name: "Verify", Question: "Verified?", Order: 2, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Outcome: doctor.OutcomeOK}}
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
	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   t.TempDir() + "/db.sqlite",
	})

	d := newDoctorApp(t.Context(), service)
	d.prepareAction(key, "fix-1")

	if d.mode.Get() != "preview_action" {
		t.Fatalf("mode = %q, want preview_action", d.mode.Get())
	}
	if d.preparedAction == nil {
		t.Fatal("preparedAction is nil")
	}
	detail := d.activeDetail.Get()
	if !strings.Contains(detail, "Upgrade fixture") || !strings.Contains(detail, "Outdated component") {
		t.Fatalf("preview detail missing label or reason: %s", detail)
	}
}

func TestMenuDeclineNoHistory(t *testing.T) {
	var historyStartCalls atomic.Int32
	var commandCalls atomic.Int32

	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "update", InstanceID: "inst"}
	proposal := doctor.ActionProposal{
		ID:                  "fix-1",
		Key:                 key,
		Mode:                doctor.ActionAutomatic,
		Label:               "Upgrade fixture",
		Reason:              "Outdated component",
		TargetIDs:           []string{"target-1"},
		TargetVersion:       doctor.Fact{State: doctor.EvidenceKnown, Value: "2.0.0"},
		Steps:               []doctor.CommandStep{{Label: "Run upgrade", Command: doctor.Command{Executable: "/usr/bin/true", Dir: "/tmp"}}},
		Preconditions:       []doctor.Fact{{State: doctor.EvidenceKnown, Label: "installed", Value: "1.0.0"}},
		VerificationCheckID: "verify-check",
	}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
			}}}
		},
		Checks: []doctor.CheckDefinition{
			{ID: "update", Name: "Update", Question: "Update?", Order: 1, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeAttention, Actions: []doctor.ActionProposal{proposal}}}
			}},
			{ID: "verify-check", Name: "Verify", Question: "Verified?", Order: 2, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Outcome: doctor.OutcomeOK}}
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

	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   t.TempDir() + "/db.sqlite",
		OpenHistory: func(context.Context, string, store.Policy) (app.History, error) {
			historyStartCalls.Add(1)
			return nil, errors.New("unexpected history open")
		},
		RunStep: func(context.Context, doctor.CommandStep) (store.StepResult, error) {
			commandCalls.Add(1)
			return store.StepResult{}, nil
		},
	})

	d := newDoctorApp(t.Context(), service)
	d.prepareAction(key, "fix-1")
	d.declinePreparedAction()

	if d.mode.Get() != "results" && d.mode.Get() != "menu" {
		t.Fatalf("mode after decline = %q", d.mode.Get())
	}
	if historyStartCalls.Load() != 0 || commandCalls.Load() != 0 {
		t.Fatalf("decline had maintenance effects: historyOpens=%d, commands=%d",
			historyStartCalls.Load(), commandCalls.Load())
	}
	if d.preparedAction != nil {
		t.Fatal("preparedAction not cleared after decline")
	}
}

func TestMenuStaleApproval(t *testing.T) {
	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "update", InstanceID: "inst"}
	proposal := doctor.ActionProposal{
		ID:                  "fix-1",
		Key:                 key,
		Mode:                doctor.ActionAutomatic,
		Label:               "Upgrade fixture",
		Reason:              "Outdated component",
		TargetIDs:           []string{"target-1"},
		TargetVersion:       doctor.Fact{State: doctor.EvidenceKnown, Value: "2.0.0"},
		Steps:               []doctor.CommandStep{{Label: "Run upgrade", Command: doctor.Command{Executable: "/usr/bin/true", Dir: "/tmp"}}},
		Preconditions:       []doctor.Fact{{State: doctor.EvidenceKnown, Label: "installed", Value: "1.0.0"}},
		VerificationCheckID: "verify-check",
	}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
				Version: doctor.Fact{State: doctor.EvidenceKnown, Value: "1.0.0"},
			}}}
		},
		Checks: []doctor.CheckDefinition{
			{ID: "update", Name: "Update", Question: "Update?", Order: 1, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeAttention, Actions: []doctor.ActionProposal{proposal}}}
			}},
			{ID: "verify-check", Name: "Verify", Question: "Verified?", Order: 2, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Outcome: doctor.OutcomeOK}}
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

	histDir := filepath.Join(t.TempDir(), "history")
	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   filepath.Join(histDir, "db.sqlite"),
		Policy:   store.DefaultPolicy(),
	})

	d := newDoctorApp(t.Context(), service)
	d.prepareAction(key, "fix-1")
	if d.preparedAction == nil {
		t.Fatal("expected preparedAction to be non-nil")
	}

	// First execution succeeds
	d.applyConfirmedAction()

	// Wait for action to complete
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.mode.Get() == "action_done" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if d.mode.Get() != "action_done" {
		t.Fatalf("expected mode action_done, got %q (status: %s)", d.mode.Get(), d.statusMsg.Get())
	}

	// Prepared action was consumed, subsequent apply must decline safely
	d.applyConfirmedAction()
	if d.preparedAction != nil {
		t.Fatal("prepared action should remain nil after consumption")
	}
}

func TestMenuActionOutcomeLabels(t *testing.T) {
	// Completed execution with Unknown verification must not render "verified repair"
	rep := app.ActionReport{
		RecordID:     "action-123",
		Execution:    store.ExecutionCompleted,
		Verification: store.VerificationUnknown,
		SafeError:    "",
	}
	text := actionReportText(rep)
	if strings.Contains(strings.ToLower(text), "verified repair") {
		t.Fatalf("completed with unknown verification falsely claimed verified repair: %s", text)
	}
	if !strings.Contains(text, "Completed") || !strings.Contains(text, "Unknown") {
		t.Fatalf("missing honest outcome labels in report text: %s", text)
	}
}
