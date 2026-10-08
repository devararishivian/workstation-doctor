package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
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

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestMigrationRequiresExplicitApproval(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.db")
	destination := filepath.Join(dir, "new", "history.db")
	createLegacyHistory(t, source)
	before := fileSHA256(t, source)

	preview, err := PreviewLegacyMigration(ctx, source, destination)
	if err != nil {
		t.Fatalf("PreviewLegacyMigration: %v", err)
	}
	if preview.SourceFingerprint == "" || preview.Actions != 2 || preview.CheckRuns != 1 || preview.CheckResults != 1 {
		t.Fatalf("preview lacks safe schema/count/fingerprint evidence: %+v", preview)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("preview created destination: %v", err)
	}
	if got := fileSHA256(t, source); got != before {
		t.Fatalf("preview modified source: before=%s after=%s", before, got)
	}
}

func TestLegacyImportPreservesSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.db")
	destination := filepath.Join(dir, "history", "doctor.db")
	createLegacyHistory(t, source)
	before := fileSHA256(t, source)

	preview, err := PreviewLegacyMigration(ctx, source, destination)
	if err != nil {
		t.Fatalf("PreviewLegacyMigration: %v", err)
	}
	result, err := ImportLegacyActions(ctx, preview)
	if err != nil {
		t.Fatalf("ImportLegacyActions: %v", err)
	}
	if result.Imported != 2 || result.PreservedSource != source {
		t.Fatalf("ImportLegacyActions = %+v, want two imported and preserved source %q", result, source)
	}
	if got := fileSHA256(t, source); got != before {
		t.Fatalf("import modified source: before=%s after=%s", before, got)
	}

	history, err := OpenHistory(ctx, destination, DefaultPolicy())
	if err != nil {
		t.Fatalf("OpenHistory destination: %v", err)
	}
	defer history.Close() //nolint:errcheck // test cleanup
	page, err := history.ListActions(ctx, ActionQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("imported actions = %d, want 2", len(page.Items))
	}
	for _, action := range page.Items {
		detail, err := history.Action(ctx, action.ID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Finish == nil || detail.Finish.Verification != VerificationUnknown {
			t.Fatalf("legacy action verification = %+v, want Unknown", detail.Finish)
		}
		if strings.Contains(detail.Label, "secret") || strings.Contains(detail.Reason, "secret") || strings.Contains(detail.Plan[0].Description, "secret") {
			t.Fatalf("legacy unsafe text leaked into history: %+v", detail)
		}
		if len(detail.Steps) != 1 || detail.Steps[0].Outcome == StepOutcomeNotStarted {
			t.Fatalf("legacy step not represented: %+v", detail.Steps)
		}
	}
	for _, table := range []string{"check_runs", "check_results"} {
		var count int
		if err := history.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("destination contains legacy audit table %q", table)
		}
	}
}

func TestLegacyImportFailureRecovery(t *testing.T) {
	t.Run("modified source is rejected without publishing destination", func(t *testing.T) {
		ctx := context.Background()
		dir := t.TempDir()
		source := filepath.Join(dir, "legacy.db")
		destination := filepath.Join(dir, "history.db")
		createLegacyHistory(t, source)
		preview, err := PreviewLegacyMigration(ctx, source, destination)
		if err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO actions(run_id,kind,command,status,output,started_at,finished_at) VALUES(1,'fix','secret','ok','secret','2026-10-04T00:00:00Z','2026-10-04T00:00:01Z')`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := ImportLegacyActions(ctx, preview); err == nil {
			t.Fatal("ImportLegacyActions accepted a source changed after preview")
		}
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			t.Fatalf("failed import published destination: %v", err)
		}
	})

	t.Run("invalid later row rolls back without publishing destination", func(t *testing.T) {
		ctx := context.Background()
		dir := t.TempDir()
		source := filepath.Join(dir, "legacy.db")
		destination := filepath.Join(dir, "destination", "history.db")
		createLegacyHistory(t, source)
		db, err := sql.Open("sqlite", source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO actions(run_id,kind,command,status,output,started_at,finished_at) VALUES(1,'fix','private','ok','private','2026-10-04T00:00:00Z','invalid-time')`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		preview, err := PreviewLegacyMigration(ctx, source, destination)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ImportLegacyActions(ctx, preview); err == nil {
			t.Fatal("ImportLegacyActions accepted an invalid later action row")
		}
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			t.Fatalf("failed import published destination: %v", err)
		}
	})

	t.Run("unknown legacy outcome is rejected", func(t *testing.T) {
		ctx := context.Background()
		dir := t.TempDir()
		source := filepath.Join(dir, "legacy.db")
		destination := filepath.Join(dir, "destination", "history.db")
		createLegacyHistory(t, source)
		db, err := sql.Open("sqlite", source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE actions SET status='mystery' WHERE id=1`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		preview, err := PreviewLegacyMigration(ctx, source, destination)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ImportLegacyActions(ctx, preview); err == nil {
			t.Fatal("ImportLegacyActions assigned an invented outcome to an unknown legacy status")
		}
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			t.Fatalf("failed import published destination: %v", err)
		}
	})

	t.Run("destination collision is not overwritten", func(t *testing.T) {
		ctx := context.Background()
		dir := t.TempDir()
		source := filepath.Join(dir, "legacy.db")
		destination := filepath.Join(dir, "existing.db")
		createLegacyHistory(t, source)
		const original = "leave-existing-destination-alone"
		if err := os.WriteFile(destination, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := PreviewLegacyMigration(ctx, source, destination); err == nil {
			t.Fatal("PreviewLegacyMigration accepted an existing destination")
		}
		got, err := os.ReadFile(destination)
		if err != nil || string(got) != original {
			t.Fatalf("destination changed after collision: %q, %v", got, err)
		}
	})

	t.Run("repeat import is idempotent", func(t *testing.T) {
		ctx := context.Background()
		dir := t.TempDir()
		source := filepath.Join(dir, "legacy.db")
		destination := filepath.Join(dir, "destination", "history.db")
		createLegacyHistory(t, source)
		preview, err := PreviewLegacyMigration(ctx, source, destination)
		if err != nil {
			t.Fatal(err)
		}
		first, err := ImportLegacyActions(ctx, preview)
		if err != nil {
			t.Fatal(err)
		}
		second, err := ImportLegacyActions(ctx, preview)
		if err != nil {
			t.Fatalf("repeat ImportLegacyActions: %v", err)
		}
		if first.Imported != 2 || second.Imported != 2 {
			t.Fatalf("first/repeat import counts = %d/%d, want 2/2", first.Imported, second.Imported)
		}
		history, err := OpenHistory(ctx, destination, DefaultPolicy())
		if err != nil {
			t.Fatal(err)
		}
		defer history.Close() //nolint:errcheck // test cleanup
		page, err := history.ListActions(ctx, ActionQuery{Limit: 10})
		if err != nil || len(page.Items) != 2 {
			t.Fatalf("after repeat import actions=%d err=%v, want exactly 2", len(page.Items), err)
		}
	})
}

func TestLegacyImportWALSnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.db")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE check_runs(id INTEGER PRIMARY KEY, started_at TEXT NOT NULL, finished_at TEXT NOT NULL, n_ok INTEGER NOT NULL DEFAULT 0, n_update INTEGER NOT NULL DEFAULT 0, n_unknown INTEGER NOT NULL DEFAULT 0, exit_code INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE check_results(id INTEGER PRIMARY KEY, run_id INTEGER NOT NULL, component TEXT NOT NULL, installed TEXT NOT NULL DEFAULT '', latest TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, note TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE actions(id INTEGER PRIMARY KEY, run_id INTEGER NOT NULL DEFAULT 0, kind TEXT NOT NULL, command TEXT NOT NULL, status TEXT NOT NULL, output TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, finished_at TEXT NOT NULL)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("prepare WAL source: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO actions(kind,command,status,output,started_at,finished_at) VALUES('fix','secret-command','ok','secret-output','2026-10-02T03:04:05Z','2026-10-02T03:04:06Z')`); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(source + suffix); err != nil {
			t.Fatalf("WAL companion %s unavailable: %v", suffix, err)
		}
	}

	mainBefore, walBefore := fileSHA256(t, source), fileSHA256(t, source+"-wal")
	destination := filepath.Join(dir, "new-history", "doctor.db")
	preview, err := PreviewLegacyMigration(ctx, source, destination)
	if err != nil {
		t.Fatalf("PreviewLegacyMigration with active WAL: %v", err)
	}
	if preview.Actions != 1 {
		t.Fatalf("WAL snapshot action count = %d, want 1", preview.Actions)
	}
	if _, err := ImportLegacyActions(ctx, preview); err != nil {
		t.Fatalf("ImportLegacyActions with active WAL: %v", err)
	}
	if got := fileSHA256(t, source); got != mainBefore {
		t.Fatalf("WAL import modified legacy main database: before=%s after=%s", mainBefore, got)
	}
	if got := fileSHA256(t, source+"-wal"); got != walBefore {
		t.Fatalf("WAL import modified legacy WAL: before=%s after=%s", walBefore, got)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationSchemaV2AddsLegacyImportMarker(t *testing.T) {
	path := privateHistoryPath(t, "schema-v2.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(actionSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE legacy_imports`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA application_id=%d; PRAGMA user_version=2`, historyApplicationID)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	history, err := OpenHistory(context.Background(), path, DefaultPolicy())
	if err != nil {
		t.Fatalf("OpenHistory v2: %v", err)
	}
	defer history.Close() //nolint:errcheck // test cleanup
	var version, marker int
	if err := history.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := history.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='legacy_imports'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if version != historySchemaVersion || marker != 1 {
		t.Fatalf("upgraded history version/marker = %d/%d, want %d/1", version, marker, historySchemaVersion)
	}
}
