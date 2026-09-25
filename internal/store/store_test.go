package store

import (
	"context"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestRecordAndListRuns(t *testing.T) {
	st, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close() //nolint:errcheck // close errors need no action in a test

	ctx := context.Background()
	start := time.Now()
	id, err := st.RecordRun(ctx, start, start.Add(time.Second), 14, 1, 0, 1, []ResultRow{
		{Component: "pi", Installed: "0.85.0", Latest: "0.85.1", Status: "UPDATE", Note: "npm package is outdated"},
		{Component: "herdr", Installed: "0.9.1", Latest: "0.9.1", Status: "OK", Note: "brew"},
	})
	if err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	if id != 1 {
		t.Fatalf("run id = %d, want 1", id)
	}

	if err := st.RecordAction(ctx, id, "fix", "brew upgrade herdr", "ok", "ok", start, start); err != nil {
		t.Fatalf("RecordAction: %v", err)
	}

	runs, err := st.ListRuns(ctx, 10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].NUpdate != 1 || runs[0].ExitCode != 1 {
		t.Fatalf("runs = %+v, want 1 run with NUpdate=1 ExitCode=1", runs)
	}

	results, err := st.RunResults(ctx, id)
	if err != nil {
		t.Fatalf("RunResults: %v", err)
	}
	if len(results) != 2 || results[0].Component != "pi" {
		t.Fatalf("results = %+v", results)
	}

	actions, err := st.RunActions(ctx, id)
	if err != nil {
		t.Fatalf("RunActions: %v", err)
	}
	if len(actions) != 1 || actions[0].Status != "ok" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestStoreContextCancellation(t *testing.T) {
	st, err := Open(t.TempDir() + "/ctx_test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close() //nolint:errcheck // close errors need no action in a test

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	start := time.Now()
	_, err = st.RecordRun(ctx, start, start, 1, 0, 0, 0, []ResultRow{
		{Component: "test", Status: "OK"},
	})
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

func TestStoreForeignKeysEnforced(t *testing.T) {
	st, err := Open(t.TempDir() + "/fk_test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close() //nolint:errcheck // close errors need no action in a test

	ctx := context.Background()
	// Attempting to record action with non-existent run_id 99999 must fail if FK is enforced.
	err = st.RecordAction(ctx, 99999, "fix", "brew upgrade", "ok", "out", time.Now(), time.Now())
	if err == nil {
		t.Fatal("expected foreign key error for non-existent run_id, got nil")
	}
}
