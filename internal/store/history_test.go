package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func privateHistoryPath(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

func testAction(id string, now time.Time) ActionStart {
	return ActionStart{
		ID: id, IntegrationID: "pi", CheckID: "version", InstanceID: "user-install",
		Label: "Update Pi", Kind: "update", Reason: "A newer version is available",
		AppVersion: "0.1.0", InstalledVersion: "1.0.0", TargetVersion: "1.1.0",
		Manager: "npm", Root: "/tmp/fixture", Scope: "user", OwnerToken: "owner-token",
		StartedAt: now.UTC(), Plan: []PlannedStep{
			{Index: 0, Label: "Update", Description: "Run the selected updater"},
			{Index: 1, Label: "Verify", Description: "Inspect the installed version"},
		}, TargetIDs: []string{"pi:user-install"},
		MetadataVersion: 1, Metadata: map[string]string{"installed_evidence_source": "fixture receipt"},
	}
}

func TestHistoryIndependentOfAudit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "history.db")
	kind, err := InspectHistory(ctx, path)
	if err != nil || kind != DBKindMissing {
		t.Fatalf("InspectHistory missing = %q, %v", kind, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspection created parent directory: %v", err)
	}

	h, err := OpenHistory(ctx, path, DefaultPolicy())
	if err != nil {
		t.Fatalf("OpenHistory: %v", err)
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 123, time.UTC)
	start := testAction("action-independent", now)
	if err := h.StartAction(ctx, start); err != nil {
		t.Fatalf("StartAction without audit run: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	kind, err = InspectHistory(ctx, path)
	if err != nil || kind != DBKindActionOnly {
		t.Fatalf("InspectHistory action-only = %q, %v", kind, err)
	}
	h, err = OpenHistory(ctx, path, DefaultPolicy())
	if err != nil {
		t.Fatalf("reopen unfinished action: %v", err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	var count, plans int
	if err := h.db.QueryRow(`SELECT count(*) FROM actions WHERE id=? AND finished_at IS NULL`, start.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRow(`SELECT count(*) FROM steps WHERE action_id=?`, start.ID).Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if count != 1 || plans != len(start.Plan) {
		t.Fatalf("durable action/steps = %d/%d, want 1/%d", count, plans, len(start.Plan))
	}
	var runIDColumn int
	if err := h.db.QueryRow(`SELECT count(*) FROM pragma_table_info('actions') WHERE name='run_id'`).Scan(&runIDColumn); err != nil {
		t.Fatal(err)
	}
	if runIDColumn != 0 {
		t.Fatal("maintenance action history unexpectedly depends on an audit run")
	}
	for _, table := range []string{"check_runs", "check_results"} {
		var exists int
		if err := h.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != 0 {
			t.Fatalf("new action history created legacy audit table %q", table)
		}
	}
	if got := DefaultPolicy(); got.MaxAge != 90*24*time.Hour || got.MaxTerminal != 10_000 || got.MaxStepOutput != 16<<10 || got.MaxActionOutput != 64<<10 || got.MaxMetadata != 8<<10 {
		t.Fatalf("DefaultPolicy = %+v", got)
	}
}

func TestHistoryStateTransitions(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, privateHistoryPath(t, "history.db"), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := h.db.Exec(`CREATE TRIGGER reject_second_planned_step BEFORE INSERT ON steps WHEN NEW.step_index=1 BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	failedStart := testAction("atomic-trigger", now)
	if err := h.StartAction(ctx, failedStart); err == nil {
		t.Fatal("expected injected planned-step insertion failure")
	}
	var partial int
	if err := h.db.QueryRow(`SELECT count(*) FROM actions WHERE id=?`, failedStart.ID).Scan(&partial); err != nil {
		t.Fatal(err)
	}
	if partial != 0 {
		t.Fatal("failed action start left a partial action row")
	}
	if _, err := h.db.Exec(`DROP TRIGGER reject_second_planned_step`); err != nil {
		t.Fatal(err)
	}
	start := testAction("transition", now)
	if err := h.StartAction(ctx, start); err != nil {
		t.Fatalf("StartAction: %v", err)
	}
	if err := h.StartAction(ctx, start); err == nil {
		t.Fatal("duplicate action ID was accepted")
	}

	step := StepResult{Index: 0, Outcome: StepOutcomeCompleted, StartedAt: now, FinishedAt: now.Add(time.Second), SafeOutput: "safe summary\ncontinued"}
	if err := h.SaveStep(ctx, start.ID, step); err != nil {
		t.Fatalf("SaveStep: %v", err)
	}
	if err := h.SaveStep(ctx, start.ID, step); err == nil {
		t.Fatal("duplicate step checkpoint was accepted")
	}
	if err := h.SaveStep(ctx, start.ID, StepResult{Index: 99, Outcome: StepOutcomeCompleted, StartedAt: now, FinishedAt: now}); err == nil {
		t.Fatal("out-of-range step checkpoint was accepted")
	}
	if err := h.SaveStep(ctx, start.ID, StepResult{Index: 1, Outcome: StepOutcomeUnspecified, StartedAt: now, FinishedAt: now}); err == nil {
		t.Fatal("unspecified step outcome was accepted")
	}
	if err := h.SaveStep(ctx, start.ID, StepResult{Index: 1, Outcome: StepOutcomeCompleted, StartedAt: now.Add(time.Second), FinishedAt: now}); err == nil {
		t.Fatal("reverse step timestamps were accepted")
	}

	finish := ActionFinish{FinishedAt: now.Add(2 * time.Second), Execution: ExecutionCompleted, Verification: VerificationPassed, ObservedVersion: "1.1.0"}
	if err := h.FinishAction(ctx, start.ID, finish); err != nil {
		t.Fatalf("FinishAction: %v", err)
	}
	if err := h.FinishAction(ctx, start.ID, finish); err == nil {
		t.Fatal("action was finalized twice")
	}
	if err := h.SaveStep(ctx, start.ID, StepResult{Index: 1, Outcome: StepOutcomeCompleted, StartedAt: now, FinishedAt: now}); err == nil {
		t.Fatal("finished action accepted another checkpoint")
	}
	var finished string
	if err := h.db.QueryRow(`SELECT execution, verification FROM actions WHERE id=?`, start.ID).Scan(&finished, new(string)); err != nil {
		t.Fatal(err)
	}
	if finished != string(ExecutionCompleted) {
		t.Fatalf("execution = %q", finished)
	}
}

func TestHistoryBounds(t *testing.T) {
	ctx := context.Background()
	path := privateHistoryPath(t, "history.db")
	h, err := OpenHistory(ctx, path, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	now := time.Now().UTC().Truncate(time.Second)
	for name, mutate := range map[string]func(*ActionStart){
		"identifier":     func(a *ActionStart) { a.ID = strings.Repeat("x", 257) },
		"critical scope": func(a *ActionStart) { a.Scope = strings.Repeat("x", 4097) },
		"plan size":      func(a *ActionStart) { a.Plan = make([]PlannedStep, 33) },
		"metadata key":   func(a *ActionStart) { a.Metadata = map[string]string{"secret": "must not persist"} },
		"metadata bytes": func(a *ActionStart) {
			a.Metadata = map[string]string{"precondition_summary": strings.Repeat("x", 9<<10)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := testAction("invalid-"+strings.ReplaceAll(name, " ", "-"), now)
			mutate(&a)
			if err := h.StartAction(ctx, a); err == nil {
				t.Fatal("oversized or non-allowlisted critical data was accepted")
			}
		})
	}

	start := testAction("truncation", now)
	if err := h.StartAction(ctx, start); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("safe-output-", 2000)
	if err := h.SaveStep(ctx, start.ID, StepResult{Index: 0, Outcome: StepOutcomeCompleted, StartedAt: now, FinishedAt: now, SafeOutput: long}); err != nil {
		t.Fatalf("SaveStep with display output: %v", err)
	}
	var retained string
	var truncated bool
	if err := h.db.QueryRow(`SELECT safe_output, output_truncated FROM steps WHERE action_id=? AND step_index=0`, start.ID).Scan(&retained, &truncated); err != nil {
		t.Fatal(err)
	}
	if len(retained) > DefaultPolicy().MaxStepOutput || !truncated || !strings.HasPrefix(long, retained) {
		t.Fatalf("retained output bytes=%d truncated=%t", len(retained), truncated)
	}
}

func TestHistoryConnectionPragmas(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "history.db")
	h, err := OpenHistory(ctx, path, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	h.db.SetMaxOpenConns(4)
	first, err := h.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close() //nolint:errcheck // close errors need no action in a test
	second, err := h.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close() //nolint:errcheck // close errors need no action in a test
	for i, conn := range []*sql.Conn{first, second} {
		var foreignKeys, timeout int
		if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 || timeout < 1000 {
			t.Errorf("connection %d pragmas foreign_keys=%d busy_timeout=%d", i, foreignKeys, timeout)
		}
	}
	if h.db.Stats().MaxOpenConnections <= 0 || h.db.Stats().MaxOpenConnections > 8 {
		t.Fatalf("unbounded or invalid connection pool: %+v", h.db.Stats())
	}
	if _, err := h.db.Exec(`INSERT INTO steps(action_id,step_index,label,description) VALUES('missing',0,'x','y')`); err == nil {
		t.Fatal("step foreign key did not reject an unknown action")
	}

	if runtime.GOOS != "windows" {
		for file, maxPerm := range map[string]os.FileMode{filepath.Dir(path): 0o700, path: 0o600} {
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&^maxPerm != 0 {
				t.Errorf("%s permissions %04o exceed %04o", file, info.Mode().Perm(), maxPerm)
			}
		}
	}
}

func TestHistoryCanceledWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h, err := OpenHistory(context.Background(), privateHistoryPath(t, "history.db"), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	if err := h.StartAction(ctx, testAction("canceled-start", time.Now().UTC())); err == nil {
		t.Fatal("StartAction accepted canceled context")
	}
	var count int
	if err := h.db.QueryRow(`SELECT count(*) FROM actions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("canceled write persisted %d action(s)", count)
	}
}

func TestHistorySchemaClassification(t *testing.T) {
	ctx := context.Background()
	legacy := privateHistoryPath(t, "legacy.db")
	db, err := sql.Open("sqlite", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE check_runs(id INTEGER PRIMARY KEY); CREATE TABLE actions(id INTEGER PRIMARY KEY, run_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	kind, err := InspectHistory(ctx, legacy)
	if err != nil || kind != DBKindLegacy {
		t.Fatalf("legacy kind = %q, %v", kind, err)
	}
	if _, err := OpenHistory(ctx, legacy, DefaultPolicy()); !errors.Is(err, ErrMigrationRequired) {
		t.Fatalf("OpenHistory legacy error = %v, want ErrMigrationRequired", err)
	}

	unknown := privateHistoryPath(t, "unknown.db")
	if err := os.WriteFile(unknown, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	kind, err = InspectHistory(ctx, unknown)
	if err == nil || kind != DBKindUnsupported {
		t.Fatalf("unsupported kind = %q, %v", kind, err)
	}
	if _, err := OpenHistory(ctx, unknown, DefaultPolicy()); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("OpenHistory unsupported error = %v, want ErrUnsupportedSchema", err)
	}
	wrongID := privateHistoryPath(t, "wrong-app.db")
	db, err = sql.Open("sqlite", wrongID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE actions(id TEXT); CREATE TABLE steps(action_id TEXT); PRAGMA user_version=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenHistory(ctx, wrongID, DefaultPolicy()); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("OpenHistory mismatched app id error = %v, want ErrUnsupportedSchema", err)
	}
	contents, err := os.ReadFile(unknown)
	if err != nil || string(contents) != "not sqlite" {
		t.Fatalf("unsupported destination changed: %q, %v", contents, err)
	}

	_, err = OpenHistory(ctx, filepath.Join(t.TempDir(), "no-parent", "x.db"), Policy{})
	if err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("invalid policy error = %v", err)
	}
}

func TestHistoryStartAtomicOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h, err := OpenHistory(context.Background(), privateHistoryPath(t, "history.db"), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	if err := h.StartAction(ctx, testAction("atomic-cancel", time.Now().UTC())); err == nil {
		t.Fatal("expected canceled transaction")
	}
	var actions, steps int
	if err := h.db.QueryRow(`SELECT count(*) FROM actions`).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRow(`SELECT count(*) FROM steps`).Scan(&steps); err != nil {
		t.Fatal(err)
	}
	if actions != 0 || steps != 0 {
		t.Fatalf("partial canceled start persisted action=%d steps=%d", actions, steps)
	}
}

func TestHistoryStepOutputBudget(t *testing.T) {
	ctx := context.Background()
	policy := DefaultPolicy()
	policy.MaxStepOutput = 8
	policy.MaxActionOutput = 12
	h, err := OpenHistory(ctx, privateHistoryPath(t, "history.db"), policy)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	now := time.Now().UTC()
	start := testAction("budget", now)
	if err := h.StartAction(ctx, start); err != nil {
		t.Fatal(err)
	}
	for i, output := range []string{"123456789", fmt.Sprintf("%020d", 1)} {
		if err := h.SaveStep(ctx, start.ID, StepResult{Index: i, Outcome: StepOutcomeCompleted, StartedAt: now, FinishedAt: now, SafeOutput: output}); err != nil {
			t.Fatal(err)
		}
	}
	var total int
	if err := h.db.QueryRow(`SELECT sum(length(CAST(safe_output AS BLOB))) FROM steps WHERE action_id=?`, start.ID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != policy.MaxActionOutput {
		t.Fatalf("action output bytes = %d, want cap %d", total, policy.MaxActionOutput)
	}
}
