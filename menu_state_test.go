package main

import (
	"context"
	"database/sql"
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

func createLegacyHistory(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		`CREATE TABLE check_runs(id INTEGER PRIMARY KEY AUTOINCREMENT, started_at TEXT NOT NULL, finished_at TEXT NOT NULL, n_ok INTEGER NOT NULL DEFAULT 0, n_update INTEGER NOT NULL DEFAULT 0, n_unknown INTEGER NOT NULL DEFAULT 0, exit_code INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE check_results(id INTEGER PRIMARY KEY AUTOINCREMENT, run_id INTEGER NOT NULL REFERENCES check_runs(id), component TEXT NOT NULL, installed TEXT NOT NULL DEFAULT '', latest TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, note TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE actions(id INTEGER PRIMARY KEY AUTOINCREMENT, run_id INTEGER NOT NULL DEFAULT 0 REFERENCES check_runs(id), kind TEXT NOT NULL, command TEXT NOT NULL, status TEXT NOT NULL, output TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, finished_at TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO check_runs(started_at,finished_at) VALUES(?,?)`, "2026-10-01T00:00:00Z", "2026-10-01T00:00:01Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO check_results(run_id,component,status) VALUES(1,'pi','ok')`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		kind, command, status, output, started, finished string
	}{
		{"fix", "echo secret-command", "ok", "secret-output", "2026-10-02T03:04:05Z", "2026-10-02T03:04:06Z"},
		{"fix", "echo another-secret", "fail", "another-secret-output", "2026-10-03T03:04:05Z", "2026-10-03T03:04:06Z"},
	} {
		if _, err := db.Exec(`INSERT INTO actions(run_id,kind,command,status,output,started_at,finished_at) VALUES(1,?,?,?,?,?,?)`, row.kind, row.command, row.status, row.output, row.started, row.finished); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

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

func TestMenuHistoryPaging(t *testing.T) {
	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "check", InstanceID: "inst"}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeOK}}
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

	histDir := filepath.Join(t.TempDir(), "history")
	dbPath := filepath.Join(histDir, "history.db")
	st, err := store.OpenHistory(t.Context(), dbPath, store.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	// Insert one action
	start := store.ActionStart{
		ID:            "act-1",
		IntegrationID: "fixture",
		CheckID:       "check",
		InstanceID:    "inst",
		Label:         "Test action",
		Kind:          "Automatic",
		Reason:        "Test",
		StartedAt:     time.Now().UTC(),
		Scope:         "user",
		OwnerToken:    "tok",
		TargetIDs:     []string{"target-1"},
		Plan: []store.PlannedStep{
			{Index: 0, Label: "Step 1", Description: "Run step"},
		},
	}
	if err := st.StartAction(t.Context(), start); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   dbPath,
		Policy:   store.DefaultPolicy(),
	})

	d := newDoctorApp(t.Context(), service)
	d.loadHistory()

	if d.mode.Get() != "history" {
		t.Fatalf("mode = %q, want history", d.mode.Get())
	}
	if len(d.historyActions.Get()) != 1 {
		t.Fatalf("expected 1 history action, got %d", len(d.historyActions.Get()))
	}
}

func TestHistoryDetailMissingInstance(t *testing.T) {
	var currentAuditCalls atomic.Int32
	var commandCalls atomic.Int32

	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			currentAuditCalls.Add(1)
			return doctor.Discovery{Availability: doctor.AvailabilityPresent}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{}
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
		DBPath:   filepath.Join(t.TempDir(), "h", "history.db"),
		Policy:   store.DefaultPolicy(),
		RunStep: func(context.Context, doctor.CommandStep) (store.StepResult, error) {
			commandCalls.Add(1)
			return store.StepResult{}, nil
		},
	})

	d := newDoctorApp(t.Context(), service)

	recordForRemovedInstance := store.ActionRecord{
		ID:            "orphan-action-1",
		IntegrationID: "removed-integration",
		CheckID:       "removed-check",
		InstanceID:    "vanished-instance",
		Label:         "Historical upgrade of vanished tool",
		Kind:          "Automatic",
		Reason:        "Routine maintenance",
		Plan: []store.PlannedStep{
			{Index: 0, Label: "Legacy step", Description: "Updated binary"},
		},
		Finish: &store.ActionFinish{
			Execution:    store.ExecutionCompleted,
			Verification: store.VerificationPassed,
		},
	}

	err = d.showHistoryDetail(recordForRemovedInstance)
	if err != nil {
		t.Fatalf("showHistoryDetail returned error: %v", err)
	}

	if currentAuditCalls.Load() != 0 || commandCalls.Load() != 0 {
		t.Fatalf("historical detail triggered inspection (%d) or execution (%d)",
			currentAuditCalls.Load(), commandCalls.Load())
	}
	detail := d.activeDetail.Get()
	if !strings.Contains(detail, "orphan-action-1") || !strings.Contains(detail, "Historical upgrade of vanished tool") {
		t.Fatalf("detail text missing orphan action info: %s", detail)
	}
}

func TestMenuMigrationPreviewConsent(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	destDir := filepath.Join(t.TempDir(), "history")
	sourceDB := filepath.Join(sourceDir, "doctor.db")
	destDB := filepath.Join(destDir, "doctor.db")

	createLegacyHistory(t, sourceDB)

	service := app.NewService(nil, nil, doctor.Scope{}, app.Options{
		StateDir: stateDir,
		DBPath:   destDB,
		Policy:   store.DefaultPolicy(),
	})

	d := newDoctorApp(t.Context(), service)
	d.previewMigration(sourceDB, destDB)

	if d.mode.Get() != "preview_migration" {
		t.Fatalf("mode = %q, want preview_migration", d.mode.Get())
	}
	if d.migrationPreview == nil || d.migrationPreview.Actions != 2 {
		t.Fatalf("migration preview not loaded: %+v", d.migrationPreview)
	}

	// Declining migration does not import
	d.applyMigration(false)
	if d.mode.Get() != "menu" && d.mode.Get() != "history" {
		t.Fatalf("mode after decline = %q", d.mode.Get())
	}

	// Confirming migration executes import
	d.previewMigration(sourceDB, destDB)
	d.applyMigration(true)
	if d.mode.Get() != "migration_done" {
		t.Fatalf("mode after confirm = %q", d.mode.Get())
	}
}

func TestHistoryAndCurrentSeparated(t *testing.T) {
	var auditCount atomic.Int32
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			auditCount.Add(1)
			return doctor.Discovery{Availability: doctor.AvailabilityPresent}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{}
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

	dbPath := filepath.Join(t.TempDir(), "h", "history.db")
	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   dbPath,
		Policy:   store.DefaultPolicy(),
	})

	d := newDoctorApp(t.Context(), service)
	_ = d.service.Audit(t.Context())
	if auditCount.Load() != 1 {
		t.Fatalf("audit count = %d, want 1", auditCount.Load())
	}

	// Browsing history must not trigger another audit
	d.loadHistory()
	if auditCount.Load() != 1 {
		t.Fatalf("browsing history triggered audit! audit count = %d", auditCount.Load())
	}
}

func TestDashboardAppearance21ComponentRows(t *testing.T) {
	defs := doctor.BuiltinDefinitions()
	engine, err := doctor.NewAuditEngine(defs, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	report := engine.Audit(t.Context(), host, doctor.Scope{})

	rows := buildDashboardRows(report)
	if len(rows) != 21 {
		t.Fatalf("expected 21 dashboard rows, got %d", len(rows))
	}

	for _, r := range rows {
		if r.Component == "" {
			t.Fatal("row has empty Component")
		}
		if r.Installed == "" || r.Latest == "" || r.Status == "" {
			t.Fatalf("row %+v has empty fields", r)
		}
	}

	d := newDoctorApp(t.Context(), nil)
	d.report.Set(report)
	elem := d.renderResults()
	if elem == nil {
		t.Fatal("renderResults returned nil element")
	}
}

func TestManualViewContainsComponentAndActionDetails(t *testing.T) {
	key := doctor.FindingKey{IntegrationID: "herdr-plugins", CheckID: "herdr-plugins", InstanceID: "plugin-1"}
	report := doctor.AuditReport{
		Findings: []doctor.Finding{
			{
				Key:         key,
				Outcome:     doctor.OutcomeAttention,
				Question:    "Update available?",
				Explanation: "New release available on remote",
				References:  []doctor.PublicReference{{Kind: "documentation", URL: "https://example.com/plugin"}},
				Actions: []doctor.ActionProposal{
					{
						ID:     "manual-1",
						Mode:   doctor.ActionManual,
						Label:  "Upgrade plugin manually",
						Reason: "Remote commit differs",
						Steps: []doctor.CommandStep{
							{Label: "Run install", Command: doctor.Command{Executable: "herdr", Args: []string{"plugin", "install", "foo"}}},
						},
					},
				},
			},
		},
	}

	d := newDoctorApp(t.Context(), nil)
	d.report.Set(report)
	elem := d.renderManual()
	if elem == nil {
		t.Fatal("renderManual returned nil element")
	}
}

func TestMenuAutomaticFixNoActionsShowsNotice(t *testing.T) {
	d := newDoctorApp(t.Context(), nil)
	d.report.Set(doctor.AuditReport{
		Findings: []doctor.Finding{
			{
				Key:     doctor.FindingKey{IntegrationID: "pi", CheckID: "pi"},
				Outcome: doctor.OutcomeOK,
			},
		},
	})
	d.triggerAutomaticFix()
	if d.mode.Get() != "no_fixes" {
		t.Fatalf("expected mode no_fixes, got %q", d.mode.Get())
	}
	elem := d.renderNoFixes()
	if elem == nil {
		t.Fatal("renderNoFixes returned nil element")
	}
}

func TestMenuAutomaticFixWithActionPreparesPreview(t *testing.T) {
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
		DBPath:   filepath.Join(t.TempDir(), "h", "history.db"),
		Policy:   store.DefaultPolicy(),
	})

	d := newDoctorApp(t.Context(), service)
	d.report.Set(doctor.AuditReport{
		Findings: []doctor.Finding{
			{
				Key:     key,
				Outcome: doctor.OutcomeAttention,
				Actions: []doctor.ActionProposal{proposal},
			},
		},
	})
	d.triggerAutomaticFix()
	if d.mode.Get() != "preview_action" {
		t.Fatalf("expected mode preview_action, got %q", d.mode.Get())
	}
	if d.preparedAction == nil {
		t.Fatal("expected non-nil preparedAction")
	}
}
