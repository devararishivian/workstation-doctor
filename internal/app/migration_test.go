package app

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"workstation-doctor/internal/doctor"

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

func TestMigrationWrapperConsent(t *testing.T) {
	stateDir := t.TempDir()
	sourceDir := t.TempDir()
	destDir := t.TempDir()
	sourceDB := filepath.Join(sourceDir, "doctor.db")
	destDB := filepath.Join(destDir, "history", "doctor.db")

	createLegacyHistory(t, sourceDB)

	service := NewService(nil, nil, doctor.Scope{}, Options{
		StateDir: stateDir,
		DBPath:   destDB,
	})

	preview, err := service.PreviewMigration(t.Context(), sourceDB, destDB)
	if err != nil {
		t.Fatalf("PreviewMigration: %v", err)
	}
	if preview.Actions != 2 {
		t.Fatalf("preview actions = %d, want 2", preview.Actions)
	}

	// Without confirmation, MigrateLegacy must not perform the import
	res, err := service.MigrateLegacy(t.Context(), preview, false)
	if err != nil {
		t.Fatalf("unconfirmed MigrateLegacy returned error: %v", err)
	}
	if res.Imported != 0 {
		t.Fatalf("unconfirmed MigrateLegacy imported %d actions", res.Imported)
	}

	// With confirmation, MigrateLegacy executes import under exclusive ownership
	res, err = service.MigrateLegacy(t.Context(), preview, true)
	if err != nil {
		t.Fatalf("confirmed MigrateLegacy: %v", err)
	}
	if res.Imported != 2 {
		t.Fatalf("confirmed MigrateLegacy imported %d, want 2", res.Imported)
	}
}

func TestMigrationWrapperLockedByAnother(t *testing.T) {
	stateDir := t.TempDir()
	sourceDB := filepath.Join(t.TempDir(), "doctor.db")
	destDB := filepath.Join(t.TempDir(), "doctor.db")
	createLegacyHistory(t, sourceDB)

	// Acquire lock externally
	own, err := AcquireOwnership(t.Context(), stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = own.Release() }()

	service := NewService(nil, nil, doctor.Scope{}, Options{
		StateDir: stateDir,
		DBPath:   destDB,
	})
	preview, err := service.PreviewMigration(t.Context(), sourceDB, destDB)
	if err != nil {
		t.Fatal(err)
	}

	// Confirmed migration must fail because exclusive ownership cannot be acquired
	_, err = service.MigrateLegacy(t.Context(), preview, true)
	if !errors.Is(err, ErrMaintenanceActive) {
		t.Fatalf("expected ErrMaintenanceActive, got %v", err)
	}
}
