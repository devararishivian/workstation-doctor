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

type mockHistory struct {
	startErr      error
	saveStepErr   error
	finishErr     error
	started       []store.ActionStart
	savedSteps    []store.StepResult
	finished      []store.ActionFinish
	listActionsFn func(ctx context.Context, query store.ActionQuery) (store.ActionPage, error)
	actionFn      func(ctx context.Context, id string) (store.ActionRecord, error)
	closed        bool
}

func (m *mockHistory) StartAction(_ context.Context, start store.ActionStart) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.started = append(m.started, start)
	return nil
}

func (m *mockHistory) SaveStep(_ context.Context, _ string, step store.StepResult) error {
	if m.saveStepErr != nil {
		return m.saveStepErr
	}
	m.savedSteps = append(m.savedSteps, step)
	return nil
}

func (m *mockHistory) FinishAction(_ context.Context, _ string, finish store.ActionFinish) error {
	if m.finishErr != nil {
		return m.finishErr
	}
	m.finished = append(m.finished, finish)
	return nil
}

func (m *mockHistory) ListActions(ctx context.Context, query store.ActionQuery) (store.ActionPage, error) {
	if m.listActionsFn != nil {
		return m.listActionsFn(ctx, query)
	}
	return store.ActionPage{}, nil
}

func (m *mockHistory) Action(ctx context.Context, id string) (store.ActionRecord, error) {
	if m.actionFn != nil {
		return m.actionFn(ctx, id)
	}
	return store.ActionRecord{}, nil
}

func (m *mockHistory) Prune(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (m *mockHistory) Close() error {
	m.closed = true
	return nil
}

func TestApplyStartFailure(t *testing.T) {
	injectedStartFailure := errors.New("injected start failure")
	mock := &mockHistory{startErr: injectedStartFailure}
	var commandCalls atomic.Int32

	service, key, _ := syntheticService(t, t.TempDir(), new(atomic.Int32))
	service.options.OpenHistory = func(context.Context, string, store.Policy) (History, error) {
		return mock, nil
	}
	service.options.RunStep = func(context.Context, doctor.CommandStep) (store.StepResult, error) {
		commandCalls.Add(1)
		exit0 := 0
		return store.StepResult{Outcome: store.StepOutcomeCompleted, ExitCode: &exit0}, nil
	}

	prepared, err := service.Prepare(t.Context(), key, "safe-update")
	if err != nil {
		t.Fatal(err)
	}
	approval, err := service.Confirm(prepared)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Apply(t.Context(), prepared, approval)
	if !errors.Is(err, injectedStartFailure) {
		t.Fatalf("expected injectedStartFailure, got %v", err)
	}
	if commandCalls.Load() != 0 {
		t.Fatalf("command ran %d times despite start failure", commandCalls.Load())
	}
}

func TestApplyStalePreconditions(t *testing.T) {
	mock := &mockHistory{}
	var commandCalls atomic.Int32

	service, key, proposal := syntheticService(t, t.TempDir(), new(atomic.Int32))
	service.options.OpenHistory = func(context.Context, string, store.Policy) (History, error) {
		return mock, nil
	}
	service.options.RunStep = func(context.Context, doctor.CommandStep) (store.StepResult, error) {
		commandCalls.Add(1)
		exit0 := 0
		return store.StepResult{Outcome: store.StepOutcomeCompleted, ExitCode: &exit0}, nil
	}

	prepared, err := service.Prepare(t.Context(), key, "safe-update")
	if err != nil {
		t.Fatal(err)
	}
	approval, err := service.Confirm(prepared)
	if err != nil {
		t.Fatal(err)
	}

	// Change host or discover definition so precondition changes on recheck
	proposal.Preconditions[0].Value = "changed-precondition"
	service.engine, err = doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Synthetic test integration."},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: key.InstanceID, IntegrationID: key.IntegrationID, Availability: doctor.AvailabilityPresent,
				Version: doctor.Fact{State: doctor.EvidenceKnown, Value: "changed-precondition", Source: "fixture"},
			}}}
		},
		Checks: []doctor.CheckDefinition{
			{ID: "update", Name: "Update", Question: "Is an update available?", Order: 1, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Outcome: doctor.OutcomeAttention, Explanation: "Changed", Actions: []doctor.ActionProposal{proposal}}}
			}},
			{ID: "verify", Name: "Verify", Question: "Is the target version installed?", Order: 2, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Outcome: doctor.OutcomeOK, Explanation: "Verified"}}
			}},
		},
	}}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.Apply(t.Context(), prepared, approval)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if report.Execution != store.ExecutionBlocked {
		t.Fatalf("expected ExecutionBlocked on stale precondition, got %v", report.Execution)
	}
	if commandCalls.Load() != 0 {
		t.Fatalf("command ran despite stale precondition")
	}
}

func TestApplyStepCheckpointFailure(t *testing.T) {
	checkpointErr := errors.New("save step failed")
	mock := &mockHistory{saveStepErr: checkpointErr}
	var commandCalls atomic.Int32

	p := doctor.ActionProposal{
		ID: "multi-step", Key: doctor.FindingKey{IntegrationID: "fixture", CheckID: "update", InstanceID: "fixture-instance"},
		Mode: doctor.ActionAutomatic, Label: "Multi-step", Reason: "Test multi-step",
		TargetIDs: []string{"fixture-target"}, TargetVersion: doctor.Fact{State: doctor.EvidenceKnown, Value: "2.0"},
		Steps: []doctor.CommandStep{
			{Label: "Step 1", Command: doctor.Command{Executable: "/usr/bin/true", Dir: "/tmp"}},
			{Label: "Step 2", Command: doctor.Command{Executable: "/usr/bin/true", Dir: "/tmp"}},
		},
		Preconditions:       []doctor.Fact{{State: doctor.EvidenceKnown, Label: "installed version", Value: "1.0"}},
		VerificationCheckID: "verify",
	}

	service, key, _ := syntheticServiceWithProposals(t, t.TempDir(), new(atomic.Int32), p)
	service.options.OpenHistory = func(context.Context, string, store.Policy) (History, error) {
		return mock, nil
	}
	service.options.RunStep = func(context.Context, doctor.CommandStep) (store.StepResult, error) {
		commandCalls.Add(1)
		exit0 := 0
		return store.StepResult{Outcome: store.StepOutcomeCompleted, ExitCode: &exit0}, nil
	}

	prepared, err := service.Prepare(t.Context(), key, "multi-step")
	if err != nil {
		t.Fatal(err)
	}
	approval, err := service.Confirm(prepared)
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.Apply(t.Context(), prepared, approval)
	if err != nil {
		t.Fatalf("Apply returned unexpected top-level error: %v", err)
	}
	if commandCalls.Load() != 1 {
		t.Fatalf("expected only 1 command call before stopping, got %d", commandCalls.Load())
	}
	if report.Execution != store.ExecutionFailed {
		t.Fatalf("expected ExecutionFailed on step checkpoint failure, got %v", report.Execution)
	}
}

func TestApplyVerificationOutcomes(t *testing.T) {
	cases := []struct {
		name        string
		verOutcome  doctor.Outcome
		expectedVer store.VerificationOutcome
	}{
		{"Passed", doctor.OutcomeOK, store.VerificationPassed},
		{"Failed", doctor.OutcomeAttention, store.VerificationFailed},
		{"Unknown", doctor.OutcomeUnknown, store.VerificationUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockHistory{}
			proposal := doctor.ActionProposal{
				ID: "verify-test", Key: doctor.FindingKey{IntegrationID: "fixture", CheckID: "update", InstanceID: "fixture-instance"},
				Mode: doctor.ActionAutomatic, Label: "Verify test", Reason: "Reason",
				TargetIDs: []string{"fixture-target"}, TargetVersion: doctor.Fact{State: doctor.EvidenceKnown, Value: "2.0"},
				Steps:               []doctor.CommandStep{{Label: "Run", Command: doctor.Command{Executable: "/usr/bin/true", Dir: "/tmp"}}},
				Preconditions:       []doctor.Fact{{State: doctor.EvidenceKnown, Label: "installed version", Value: "1.0"}},
				VerificationCheckID: "verify",
			}
			engine, err := doctor.NewAuditEngine([]doctor.Definition{{
				Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Fixture"},
				Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
					return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
						ID: "fixture-instance", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
						Version: doctor.Fact{State: doctor.EvidenceKnown, Value: "1.0"},
					}}}
				},
				Checks: []doctor.CheckDefinition{
					{ID: "update", Name: "Update", Question: "Update?", Order: 1, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
						return []doctor.Finding{{Outcome: doctor.OutcomeAttention, Actions: []doctor.ActionProposal{proposal}}}
					}},
					{ID: "verify", Name: "Verify", Question: "Verify?", Order: 2, Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
						return []doctor.Finding{{Outcome: tc.verOutcome, Explanation: "Verification run"}}
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
				StateDir: t.TempDir(), DBPath: "/tmp/db",
				OpenHistory: func(context.Context, string, store.Policy) (History, error) { return mock, nil },
				RunStep: func(context.Context, doctor.CommandStep) (store.StepResult, error) {
					exit0 := 0
					return store.StepResult{Outcome: store.StepOutcomeCompleted, ExitCode: &exit0}, nil
				},
			})

			prepared, err := service.Prepare(t.Context(), proposal.Key, proposal.ID)
			if err != nil {
				t.Fatal(err)
			}
			approval, err := service.Confirm(prepared)
			if err != nil {
				t.Fatal(err)
			}

			report, err := service.Apply(t.Context(), prepared, approval)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if report.Execution != store.ExecutionCompleted {
				t.Fatalf("execution = %v, want Completed", report.Execution)
			}
			if report.Verification != tc.expectedVer {
				t.Fatalf("verification = %v, want %v", report.Verification, tc.expectedVer)
			}
		})
	}
}

func TestApplyFinalizationAfterCancel(t *testing.T) {
	mock := &mockHistory{}
	service, key, _ := syntheticService(t, t.TempDir(), new(atomic.Int32))
	service.options.OpenHistory = func(context.Context, string, store.Policy) (History, error) {
		return mock, nil
	}

	ctx, cancel := context.WithCancel(t.Context())
	service.options.RunStep = func(_ context.Context, _ doctor.CommandStep) (store.StepResult, error) {
		cancel() // Cancel during step execution
		return store.StepResult{Outcome: store.StepOutcomeCanceled}, context.Canceled
	}

	prepared, err := service.Prepare(t.Context(), key, "safe-update")
	if err != nil {
		t.Fatal(err)
	}
	approval, err := service.Confirm(prepared)
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.Apply(ctx, prepared, approval)
	if err != nil {
		t.Fatalf("Apply returned error on cancel: %v", err)
	}
	if report.Execution != store.ExecutionCanceled {
		t.Fatalf("expected ExecutionCanceled, got %v", report.Execution)
	}
	if len(mock.finished) != 1 || mock.finished[0].Execution != store.ExecutionCanceled {
		t.Fatalf("finalization did not record Canceled execution: %+v", mock.finished)
	}
}

func TestApplyNoShellOrReplay(t *testing.T) {
	mock := &mockHistory{}
	proposal := doctor.ActionProposal{
		ID: "shell-prop", Key: doctor.FindingKey{IntegrationID: "fixture", CheckID: "update", InstanceID: "fixture-instance"},
		Mode: doctor.ActionAutomatic, Label: "Shell proposal", Reason: "Shell",
		TargetIDs: []string{"fixture-target"}, TargetVersion: doctor.Fact{State: doctor.EvidenceKnown, Value: "2.0"},
		Steps:               []doctor.CommandStep{{Label: "Run", Command: doctor.Command{Executable: "/bin/sh", Dir: "/tmp"}}},
		Preconditions:       []doctor.Fact{{State: doctor.EvidenceKnown, Label: "installed version", Value: "1.0"}},
		VerificationCheckID: "verify",
	}
	service, key, _ := syntheticServiceWithProposals(t, t.TempDir(), new(atomic.Int32), proposal)
	service.options.OpenHistory = func(context.Context, string, store.Policy) (History, error) {
		return mock, nil
	}

	// Prepare should fail because shell is rejected
	_, err := service.Prepare(t.Context(), key, "shell-prop")
	if err == nil {
		t.Fatal("Prepare did not reject shell executable")
	}
}

func TestHistoryAndDetail(t *testing.T) {
	errNotFound := errors.New("not found")
	mock := &mockHistory{
		listActionsFn: func(_ context.Context, _ store.ActionQuery) (store.ActionPage, error) {
			return store.ActionPage{
				Items: []store.ActionRecord{{ID: "action-1", Label: "Action 1"}},
			}, nil
		},
		actionFn: func(_ context.Context, id string) (store.ActionRecord, error) {
			if id == "action-1" {
				return store.ActionRecord{ID: "action-1", Label: "Action 1"}, nil
			}
			return store.ActionRecord{}, errNotFound
		},
	}

	service, _, _ := syntheticService(t, t.TempDir(), new(atomic.Int32))
	service.options.OpenHistory = func(context.Context, string, store.Policy) (History, error) {
		return mock, nil
	}

	page, err := service.History(t.Context(), store.ActionQuery{Limit: 10})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "action-1" {
		t.Fatalf("unexpected history page: %+v", page)
	}

	rec, err := service.HistoryDetail(t.Context(), "action-1")
	if err != nil {
		t.Fatalf("HistoryDetail: %v", err)
	}
	if rec.ID != "action-1" {
		t.Fatalf("unexpected record: %+v", rec)
	}

	_, err = service.HistoryDetail(t.Context(), "non-existent")
	if !errors.Is(err, errNotFound) {
		t.Fatalf("expected errNotFound, got %v", err)
	}
}
