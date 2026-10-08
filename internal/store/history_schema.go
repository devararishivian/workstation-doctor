package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const historyMigrationBatch = 256

// migrateHistorySchema applies only additive internal schema upgrades to recognized action history.
func migrateHistorySchema(ctx context.Context, db *sql.DB) error {
	var current int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("read history schema version: %w", err)
	}
	if current == historySchemaVersion {
		return nil
	}
	if current < 1 || current > historySchemaVersion {
		return fmt.Errorf("unsupported history schema version %d", current)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin history schema upgrade: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read history schema version: %w", err)
	}
	if version == historySchemaVersion {
		return nil
	}
	if version < 1 || version > historySchemaVersion {
		return fmt.Errorf("unsupported history schema version %d", version)
	}
	if version == 1 {
		if err := normalizeActionTimes(ctx, tx); err != nil {
			return err
		}
		if err := normalizeStepTimes(ctx, tx); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_actions_batch_started ON actions(batch_id, started_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_actions_execution_started ON actions(execution, started_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_actions_verification_started ON actions(verification, started_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_actions_finished_started ON actions(finished_at, started_at DESC, id DESC)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create history filter index: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 2`); err != nil {
		return fmt.Errorf("set history schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit history schema upgrade: %w", err)
	}
	return nil
}

type actionTimeRow struct {
	id, started string
	finished    sql.NullString
}

func normalizeActionTimes(ctx context.Context, tx *sql.Tx) error {
	lastID := ""
	for {
		rows, err := tx.QueryContext(ctx, `SELECT id,started_at,finished_at FROM actions WHERE id>? ORDER BY id LIMIT ?`, lastID, historyMigrationBatch)
		if err != nil {
			return fmt.Errorf("read action timestamps: %w", err)
		}
		batch := make([]actionTimeRow, 0, historyMigrationBatch)
		for rows.Next() {
			var row actionTimeRow
			if err := rows.Scan(&row.id, &row.started, &row.finished); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan action timestamps: %w", err)
			}
			batch = append(batch, row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate action timestamps: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close action timestamp read: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		for _, row := range batch {
			started, err := parseHistoryTime(row.started)
			if err != nil {
				return fmt.Errorf("normalize action %q start time: %w", row.id, err)
			}
			var finished any
			if row.finished.Valid {
				value, err := parseHistoryTime(row.finished.String)
				if err != nil {
					return fmt.Errorf("normalize action %q finish time: %w", row.id, err)
				}
				finished = tsHistory(value)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE actions SET started_at=?,finished_at=? WHERE id=?`, tsHistory(started), finished, row.id); err != nil {
				return fmt.Errorf("write normalized action time: %w", err)
			}
			lastID = row.id
		}
	}
}

type stepTimeRow struct {
	rowID    int64
	started  string
	finished sql.NullString
}

func normalizeStepTimes(ctx context.Context, tx *sql.Tx) error {
	var lastRowID int64
	for {
		rows, err := tx.QueryContext(ctx, `SELECT rowid,started_at,finished_at FROM steps WHERE rowid>? AND started_at IS NOT NULL ORDER BY rowid LIMIT ?`, lastRowID, historyMigrationBatch)
		if err != nil {
			return fmt.Errorf("read step timestamps: %w", err)
		}
		batch := make([]stepTimeRow, 0, historyMigrationBatch)
		for rows.Next() {
			var row stepTimeRow
			if err := rows.Scan(&row.rowID, &row.started, &row.finished); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan step timestamps: %w", err)
			}
			batch = append(batch, row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate step timestamps: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close step timestamp read: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		for _, row := range batch {
			started, err := parseHistoryTime(row.started)
			if err != nil {
				return fmt.Errorf("normalize step start time: %w", err)
			}
			var finished any
			if row.finished.Valid {
				value, err := parseHistoryTime(row.finished.String)
				if err != nil {
					return fmt.Errorf("normalize step finish time: %w", err)
				}
				finished = tsHistory(value)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE steps SET started_at=?,finished_at=? WHERE rowid=?`, tsHistory(started), finished, row.rowID); err != nil {
				return fmt.Errorf("write normalized step time: %w", err)
			}
			lastRowID = row.rowID
		}
	}
}

func parseHistoryTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp: %w", err)
	}
	return parsed.UTC(), nil
}
