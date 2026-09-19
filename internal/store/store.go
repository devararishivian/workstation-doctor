// Package store keeps the history of checks, updates, and actions
// in an embedded SQLite database.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const schema = `
CREATE TABLE IF NOT EXISTS check_runs(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at TEXT NOT NULL,
  finished_at TEXT NOT NULL,
  n_ok INTEGER NOT NULL DEFAULT 0,
  n_update INTEGER NOT NULL DEFAULT 0,
  n_unknown INTEGER NOT NULL DEFAULT 0,
  exit_code INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS check_results(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id INTEGER NOT NULL REFERENCES check_runs(id),
  component TEXT NOT NULL,
  installed TEXT NOT NULL DEFAULT '',
  latest TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS actions(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id INTEGER NOT NULL DEFAULT 0 REFERENCES check_runs(id),
  kind TEXT NOT NULL,
  command TEXT NOT NULL,
  status TEXT NOT NULL,
  output TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  finished_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_results_run ON check_results(run_id);
CREATE INDEX IF NOT EXISTS idx_actions_run ON actions(run_id);
`

// Run is one row of check history.
type Run struct {
	ID         int64
	StartedAt  string
	FinishedAt string
	NOk        int
	NUpdate    int
	NUnknown   int
	ExitCode   int
}

// ResultRow is one component result in one run.
type ResultRow struct {
	Component string
	Installed string
	Latest    string
	Status    string
	Note      string
}

// ActionRow is one recorded fix action.
type ActionRow struct {
	ID        int64
	RunID     int64
	Kind      string
	Command   string
	Status    string
	Output    string
	StartedAt string
	Finished  string
}

// Store wraps the database connection.
type Store struct {
	db *sql.DB
}

// DefaultPath returns the default database location.
func DefaultPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "workstation-doctor", "doctor.db")
}

// Open opens the database at path and creates it when necessary.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: siapkan direktori %s: %w", filepath.Dir(path), err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: buka %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrasi %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func ts(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// RecordRun stores one check run with its results and returns the run ID.
func (s *Store) RecordRun(start, end time.Time, nOk, nUpdate, nUnknown, exitCode int, results []ResultRow) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("store: mulai transaksi run: %w", err)
	}
	// Rollback is best effort. Commit decides the final result.
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(
		`INSERT INTO check_runs(started_at, finished_at, n_ok, n_update, n_unknown, exit_code)
		 VALUES(?,?,?,?,?,?)`,
		ts(start), ts(end), nOk, nUpdate, nUnknown, exitCode)
	if err != nil {
		return 0, fmt.Errorf("store: simpan run: %w", err)
	}
	runID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: id run: %w", err)
	}
	for _, r := range results {
		if _, err := tx.Exec(
			`INSERT INTO check_results(run_id, component, installed, latest, status, note)
			 VALUES(?,?,?,?,?,?)`,
			runID, r.Component, r.Installed, r.Latest, r.Status, r.Note); err != nil {
			return 0, fmt.Errorf("store: simpan hasil %s: %w", r.Component, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit run: %w", err)
	}
	return runID, nil
}

// RecordAction records one action (for example a fix command) in the history.
func (s *Store) RecordAction(runID int64, kind, command, status, output string, start, end time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO actions(run_id, kind, command, status, output, started_at, finished_at)
		 VALUES(?,?,?,?,?,?,?)`,
		runID, kind, command, status, output, ts(start), ts(end))
	if err != nil {
		return fmt.Errorf("store: simpan aksi %q: %w", command, err)
	}
	return nil
}

// ListRuns returns the newest N runs (newest first).
func (s *Store) ListRuns(limit int) ([]Run, error) {
	rows, err := s.db.Query(
		`SELECT id, started_at, finished_at, n_ok, n_update, n_unknown, exit_code
		 FROM check_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // close errors need no action after a full read
	out := []Run{}
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.FinishedAt, &r.NOk, &r.NUpdate, &r.NUnknown, &r.ExitCode); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunResults returns the component results of one run.
func (s *Store) RunResults(runID int64) ([]ResultRow, error) {
	rows, err := s.db.Query(
		`SELECT component, installed, latest, status, note
		 FROM check_results WHERE run_id=? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // close errors need no action after a full read
	out := []ResultRow{}
	for rows.Next() {
		var r ResultRow
		if err := rows.Scan(&r.Component, &r.Installed, &r.Latest, &r.Status, &r.Note); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunActions returns the actions of one run.
func (s *Store) RunActions(runID int64) ([]ActionRow, error) {
	rows, err := s.db.Query(
		`SELECT id, run_id, kind, command, status, output, started_at, finished_at
		 FROM actions WHERE run_id=? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // close errors need no action after a full read
	out := []ActionRow{}
	for rows.Next() {
		var a ActionRow
		if err := rows.Scan(&a.ID, &a.RunID, &a.Kind, &a.Command, &a.Status, &a.Output, &a.StartedAt, &a.Finished); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
