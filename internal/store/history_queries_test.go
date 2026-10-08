package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func addHistoryAction(t *testing.T, h *HistoryStore, id, integration, instance, batch string, started time.Time, execution ExecutionOutcome, verification VerificationOutcome) {
	t.Helper()
	start := testAction(id, started)
	start.IntegrationID = integration
	start.InstanceID = instance
	start.BatchID = batch
	if err := h.StartAction(context.Background(), start); err != nil {
		t.Fatalf("StartAction(%s): %v", id, err)
	}
	if execution != ExecutionUnspecified {
		finish := ActionFinish{
			FinishedAt: started.Add(time.Second), Execution: execution, Verification: verification,
			ObservedVersion: "observed",
		}
		if err := h.FinishAction(context.Background(), id, finish); err != nil {
			t.Fatalf("FinishAction(%s): %v", id, err)
		}
	}
}

func TestHistoryPagination(t *testing.T) {
	ctx := context.Background()
	path := privateHistoryPath(t, "pagination.db")
	h, err := OpenHistory(ctx, path, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	started := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for i := range 52 {
		addHistoryAction(t, h, fmt.Sprintf("item-%03d", i), "pi", "instance", "batch", started, ExecutionCompleted, VerificationUnknown)
	}

	first, err := h.ListActions(ctx, ActionQuery{})
	if err != nil {
		t.Fatalf("ListActions default page: %v", err)
	}
	if len(first.Items) != 50 || first.NextCursor == "" {
		t.Fatalf("default page has %d items and cursor %q, want 50 and next cursor", len(first.Items), first.NextCursor)
	}
	if first.Items[0].ID != "item-051" || first.Items[49].ID != "item-002" {
		t.Fatalf("same-time order = %q ... %q, want descending IDs", first.Items[0].ID, first.Items[49].ID)
	}
	if len(first.Items[0].Plan) != 0 || len(first.Items[0].Steps) != 0 {
		t.Fatal("history list loaded detailed planned/result steps")
	}

	// An action added after page one must not shift the keyset page.
	addHistoryAction(t, h, "item-newer", "pi", "instance", "batch", started.Add(time.Hour), ExecutionCompleted, VerificationUnknown)
	second, err := h.ListActions(ctx, ActionQuery{Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("ListActions next page: %v", err)
	}
	if len(second.Items) != 2 || second.Items[0].ID != "item-001" || second.Items[1].ID != "item-000" || second.NextCursor != "" {
		t.Fatalf("second page = %+v", second)
	}

	page, err := h.ListActions(ctx, ActionQuery{Limit: 200})
	if err != nil || len(page.Items) != 53 {
		t.Fatalf("200-row limit page = %d, %v", len(page.Items), err)
	}
	for _, query := range []ActionQuery{{Limit: -1}, {Limit: 201}, {Cursor: "not-a-cursor"}} {
		if _, err := h.ListActions(ctx, query); err == nil {
			t.Errorf("invalid query %+v was accepted", query)
		}
	}
	if _, err := h.ListActions(cancelledContext(t), ActionQuery{}); err == nil {
		t.Fatal("ListActions accepted a canceled context")
	}
}

func cancelledContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestHistoryFilters(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, privateHistoryPath(t, "filters.db"), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	addHistoryAction(t, h, "pi-a", "pi", "instance-a", "batch-a", base, ExecutionCompleted, VerificationPassed)
	addHistoryAction(t, h, "pi-b", "pi", "instance-b", "batch-a", base.Add(time.Second), ExecutionFailed, VerificationUnknown)
	addHistoryAction(t, h, "herdr-a", "herdr", "instance-a", "batch-b", base.Add(2*time.Second), ExecutionCanceled, VerificationNotPerformed)
	addHistoryAction(t, h, "running", "pi", "instance-a", "batch-b", base.Add(3*time.Second), ExecutionUnspecified, VerificationUnspecified)

	from, to := base.Add(time.Second), base.Add(2*time.Second)
	tests := []struct {
		name  string
		query ActionQuery
		want  []string
	}{
		{"integration", ActionQuery{IntegrationID: "pi"}, []string{"running", "pi-b", "pi-a"}},
		{"instance", ActionQuery{InstanceID: "instance-b"}, []string{"pi-b"}},
		{"execution", ActionQuery{Execution: ExecutionFailed}, []string{"pi-b"}},
		{"verification", ActionQuery{Verification: VerificationNotPerformed}, []string{"herdr-a"}},
		{"batch", ActionQuery{BatchID: "batch-a"}, []string{"pi-b", "pi-a"}},
		{"time range", ActionQuery{From: &from, To: &to}, []string{"herdr-a", "pi-b"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page, err := h.ListActions(ctx, test.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != len(test.want) {
				t.Fatalf("got %d rows, want %d: %+v", len(page.Items), len(test.want), page.Items)
			}
			for i, id := range test.want {
				if page.Items[i].ID != id {
					t.Errorf("item %d ID=%q, want %q", i, page.Items[i].ID, id)
				}
			}
		})
	}
	reversedFrom, reversedTo := base.Add(time.Hour), base
	if _, err := h.ListActions(ctx, ActionQuery{From: &reversedFrom, To: &reversedTo}); err == nil {
		t.Fatal("reversed time range was accepted")
	}
	if _, err := h.ListActions(ctx, ActionQuery{Execution: "success"}); err == nil {
		t.Fatal("unknown execution filter was accepted")
	}
	if _, err := h.ListActions(ctx, ActionQuery{IntegrationID: strings.Repeat("x", maxIdentifierBytes+1)}); err == nil {
		t.Fatal("oversized identity filter was accepted")
	}

	start := testAction("detail", base.Add(4*time.Second))
	if err := h.StartAction(ctx, start); err != nil {
		t.Fatal(err)
	}
	if err := h.SaveStep(ctx, start.ID, StepResult{Index: 0, Outcome: StepOutcomeCompleted, StartedAt: base.Add(4 * time.Second), FinishedAt: base.Add(5 * time.Second), SafeOutput: "safe"}); err != nil {
		t.Fatal(err)
	}
	record, err := h.Action(ctx, start.ID)
	if err != nil {
		t.Fatalf("Action detail: %v", err)
	}
	if record.Finish != nil || len(record.Plan) != 2 || len(record.Steps) != 1 || record.Steps[0].SafeOutput != "safe" {
		t.Fatalf("unfinished detail lost plan/checkpoint: %+v", record)
	}
	finished := ActionFinish{FinishedAt: base.Add(6 * time.Second), Execution: ExecutionCompleted, Verification: VerificationFailed, ObservedVersion: "wrong"}
	if err := h.FinishAction(ctx, start.ID, finished); err != nil {
		t.Fatal(err)
	}
	record, err = h.Action(ctx, start.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Finish == nil || record.Finish.Execution != ExecutionCompleted || record.Finish.Verification != VerificationFailed {
		t.Fatalf("detail merged execution and verification outcomes: %+v", record.Finish)
	}
	if _, err := h.Action(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing detail error = %v, want sql.ErrNoRows", err)
	}
}

func TestHistoryRetention(t *testing.T) {
	ctx := context.Background()
	policy := DefaultPolicy()
	policy.MaxAge = 10 * 24 * time.Hour
	policy.MaxTerminal = 10
	path := privateHistoryPath(t, "retention.db")
	h, err := OpenHistory(ctx, path, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-policy.MaxAge)
	addHistoryAction(t, h, "expired", "pi", "instance", "", cutoff.Add(-2*time.Second), ExecutionFailed, VerificationUnknown)
	addHistoryAction(t, h, "boundary", "pi", "instance", "", cutoff.Add(-time.Second), ExecutionCompleted, VerificationUnknown)
	addHistoryAction(t, h, "recent", "pi", "instance", "", now.Add(-time.Hour), ExecutionCompleted, VerificationPassed)
	addHistoryAction(t, h, "running-old", "pi", "instance", "", cutoff.Add(-time.Hour), ExecutionUnspecified, VerificationUnspecified)

	deleted, err := h.Prune(ctx, now)
	if err != nil {
		t.Fatalf("Prune age: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("age pruning deleted %d, want one action", deleted)
	}
	if _, err := h.Action(ctx, "expired"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired action remains: %v", err)
	}
	if _, err := h.Action(ctx, "boundary"); err != nil {
		t.Fatalf("action at exact age boundary was pruned: %v", err)
	}
	if _, err := h.Action(ctx, "running-old"); err != nil {
		t.Fatalf("unfinished action was pruned: %v", err)
	}

	policy.MaxTerminal = 3
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = OpenHistory(ctx, path, policy)
	if err != nil {
		t.Fatal(err)
	}
	addHistoryAction(t, h, "middle", "pi", "instance", "", now.Add(-30*time.Minute), ExecutionCompleted, VerificationUnknown)
	addHistoryAction(t, h, "newest", "pi", "instance", "", now.Add(-10*time.Minute), ExecutionCompleted, VerificationUnknown)
	deleted, err = h.Prune(ctx, now)
	if err != nil {
		t.Fatalf("Prune terminal count: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("count pruning deleted %d, want one action", deleted)
	}
	if _, err := h.Action(ctx, "boundary"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("oldest terminal action remains after count pruning: %v", err)
	}
	if _, err := h.Action(ctx, "newest"); err != nil {
		t.Fatalf("newest terminal action was pruned: %v", err)
	}
	var remainingSteps int
	if err := h.db.QueryRow(`SELECT count(*) FROM steps WHERE action_id='boundary'`).Scan(&remainingSteps); err != nil {
		t.Fatal(err)
	}
	if remainingSteps != 0 {
		t.Fatalf("pruned action retained %d step row(s)", remainingSteps)
	}
	if _, err := h.Prune(cancelledContext(t), now); err == nil {
		t.Fatal("Prune accepted a canceled context")
	}
	if _, err := h.Prune(ctx, time.Time{}); err == nil {
		t.Fatal("Prune accepted a zero timestamp")
	}

	zeroPolicy := DefaultPolicy()
	zeroPolicy.MaxTerminal = 0
	if err := validatePolicy(zeroPolicy); err == nil {
		t.Fatal("zero retention override was accepted")
	}
}

func TestHistoryRetentionKeepsNewestTenThousand(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, privateHistoryPath(t, "ten-thousand.db"), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO actions(id,batch_id,integration_id,check_id,instance_id,label,kind,reason,app_version,installed_version,target_version,manager,root,scope,owner_token,started_at,finished_at,execution,verification,observed_version,target_ids,metadata_version,metadata_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	const count = 10_001
	for i := range count {
		id := fmt.Sprintf("action-%05d", i)
		started := now.Add(-time.Duration(count-i) * time.Second)
		if _, err := stmt.ExecContext(ctx, id, "", "pi", "check", "instance", "label", "update", "reason", "", "", "", "", "", "user", "owner", tsHistory(started), tsHistory(started.Add(time.Second)), ExecutionCompleted, VerificationUnknown, "", `["target"]`, 0, `{}`); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			t.Fatalf("insert synthetic action %d: %v", i, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO steps(action_id,step_index,label,description) VALUES('action-00000',0,'step','description')`); err != nil {
		_ = stmt.Close()
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	deleted, err := h.Prune(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("Prune deleted %d actions, want one oldest terminal action", deleted)
	}
	var remaining int
	if err := h.db.QueryRowContext(ctx, `SELECT count(*) FROM actions WHERE finished_at IS NOT NULL`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != DefaultPolicy().MaxTerminal {
		t.Fatalf("remaining terminal actions=%d, want %d", remaining, DefaultPolicy().MaxTerminal)
	}
	if _, err := h.Action(ctx, "action-00000"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("oldest action remains: %v", err)
	}
	var newest int
	if err := h.db.QueryRowContext(ctx, `SELECT count(*) FROM actions WHERE id='action-10000'`).Scan(&newest); err != nil || newest != 1 {
		t.Fatalf("newest action count = %d, %v", newest, err)
	}
	var steps int
	if err := h.db.QueryRowContext(ctx, `SELECT count(*) FROM steps WHERE action_id='action-00000'`).Scan(&steps); err != nil || steps != 0 {
		t.Fatalf("oldest action retained %d steps: %v", steps, err)
	}
}

func TestHistoryOutputBudget(t *testing.T) {
	ctx := context.Background()
	path := privateHistoryPath(t, "output.db")
	h, err := OpenHistory(ctx, path, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	now := time.Now().UTC().Truncate(time.Second)
	start := testAction("maximum-output", now)
	start.Plan = make([]PlannedStep, maxActionSteps)
	for i := range start.Plan {
		start.Plan[i] = PlannedStep{Index: i, Label: fmt.Sprintf("Step %d", i), Description: "Synthetic safe step"}
	}
	if err := h.StartAction(ctx, start); err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("x", DefaultPolicy().MaxStepOutput)
	for i := range start.Plan {
		if err := h.SaveStep(ctx, start.ID, StepResult{Index: i, Outcome: StepOutcomeCompleted, StartedAt: now, FinishedAt: now, SafeOutput: payload}); err != nil {
			t.Fatalf("SaveStep(%d): %v", i, err)
		}
	}
	var sum, maximum, truncated int
	if err := h.db.QueryRow(`SELECT sum(length(CAST(safe_output AS BLOB))),max(length(CAST(safe_output AS BLOB))),sum(output_truncated) FROM steps WHERE action_id=?`, start.ID).Scan(&sum, &maximum, &truncated); err != nil {
		t.Fatal(err)
	}
	if sum != DefaultPolicy().MaxActionOutput || maximum > DefaultPolicy().MaxStepOutput || truncated == 0 {
		t.Fatalf("stored output sum=%d max-step=%d truncated-steps=%d", sum, maximum, truncated)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 {
		t.Fatalf("history size evidence = %v, %v", info, err)
	}
}
