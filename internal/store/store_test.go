package store

import (
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

	start := time.Now()
	id, err := st.RecordRun(start, start.Add(time.Second), 14, 1, 0, 1, []ResultRow{
		{Component: "pi", Installed: "0.85.0", Latest: "0.85.1", Status: "UPDATE", Note: "npm tertinggal"},
		{Component: "herdr", Installed: "0.9.1", Latest: "0.9.1", Status: "OK", Note: "brew"},
	})
	if err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	if id != 1 {
		t.Fatalf("run id = %d, want 1", id)
	}

	if err := st.RecordAction(id, "fix", "brew upgrade herdr", "ok", "ok", start, start); err != nil {
		t.Fatalf("RecordAction: %v", err)
	}

	runs, err := st.ListRuns(10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].NUpdate != 1 || runs[0].ExitCode != 1 {
		t.Fatalf("runs = %+v, want 1 run with NUpdate=1 ExitCode=1", runs)
	}

	results, err := st.RunResults(id)
	if err != nil {
		t.Fatalf("RunResults: %v", err)
	}
	if len(results) != 2 || results[0].Component != "pi" {
		t.Fatalf("results = %+v", results)
	}

	actions, err := st.RunActions(id)
	if err != nil {
		t.Fatalf("RunActions: %v", err)
	}
	if len(actions) != 1 || actions[0].Status != "ok" {
		t.Fatalf("actions = %+v", actions)
	}
}
