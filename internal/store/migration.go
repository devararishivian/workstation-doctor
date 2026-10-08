package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	modernsqlite "modernc.org/sqlite"
)

const maxLegacySnapshotBytes = 64 << 20

// MigrationPreview is read-only evidence bound to a consistent source snapshot.
type MigrationPreview struct {
	Source, Destination string
	Schema              string
	Actions             int
	CheckRuns           int
	CheckResults        int
	SourceFingerprint   string
}

// MigrationResult describes the imported action count and untouched source path.
type MigrationResult struct {
	Imported        int
	PreservedSource string
}

// PreviewLegacyMigration reads a consistent online SQLite snapshot and counts
// recognized legacy rows without creating or changing the destination.
func PreviewLegacyMigration(ctx context.Context, source, destination string) (MigrationPreview, error) {
	preview, snapshot, err := inspectLegacySnapshot(ctx, source, destination, false)
	if snapshot != "" {
		if cleanupErr := os.RemoveAll(filepath.Dir(snapshot)); err == nil && cleanupErr != nil {
			err = fmt.Errorf("store: remove temporary migration snapshot: %w", cleanupErr)
		}
	}
	return preview, err
}

// ImportLegacyActions revalidates the preview fingerprint, imports only action
// outcomes into a new destination, and preserves the original database.
func ImportLegacyActions(ctx context.Context, preview MigrationPreview) (MigrationResult, error) {
	if err := ctx.Err(); err != nil {
		return MigrationResult{}, fmt.Errorf("store: legacy import canceled: %w", err)
	}
	current, snapshot, err := inspectLegacySnapshot(ctx, preview.Source, preview.Destination, true)
	if err != nil {
		return MigrationResult{}, err
	}
	defer func() {
		if err := os.RemoveAll(filepath.Dir(snapshot)); err != nil {
			// Snapshot cleanup failure does not affect the published destination.
			_ = err
		}
	}()
	if preview.SourceFingerprint == "" || current.SourceFingerprint != preview.SourceFingerprint || current.Actions != preview.Actions || current.Schema != preview.Schema {
		return MigrationResult{}, errors.New("store: legacy source changed after preview")
	}
	if _, err := os.Lstat(current.Destination); err == nil {
		return existingImport(ctx, current)
	} else if !errors.Is(err, os.ErrNotExist) {
		return MigrationResult{}, fmt.Errorf("store: inspect migration destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(current.Destination), 0o700); err != nil {
		return MigrationResult{}, fmt.Errorf("store: create migration destination directory: %w", err)
	}
	if err := prepareHistoryDirectory(filepath.Dir(current.Destination)); err != nil {
		return MigrationResult{}, err
	}
	stagingDir, err := os.MkdirTemp(filepath.Dir(current.Destination), ".workstation-doctor-import-*")
	if err != nil {
		return MigrationResult{}, fmt.Errorf("store: create migration staging directory: %w", err)
	}
	stagingPath := filepath.Join(stagingDir, "history.db")
	defer func() {
		if err := os.RemoveAll(stagingDir); err != nil {
			// Staging cleanup failure does not affect the published destination.
			_ = err
		}
	}()

	history, err := OpenHistory(ctx, stagingPath, DefaultPolicy())
	if err != nil {
		return MigrationResult{}, fmt.Errorf("store: open migration staging history: %w", err)
	}
	if err := importLegacySnapshot(ctx, history, snapshot, current); err != nil {
		_ = history.Close()
		return MigrationResult{}, err
	}
	if err := history.Close(); err != nil {
		return MigrationResult{}, fmt.Errorf("store: close imported history: %w", err)
	}
	if _, err := InspectHistory(ctx, stagingPath); err != nil {
		return MigrationResult{}, fmt.Errorf("store: validate imported history: %w", err)
	}
	if err := os.Link(stagingPath, current.Destination); err != nil {
		return MigrationResult{}, fmt.Errorf("store: publish imported history without overwrite: %w", err)
	}
	return MigrationResult{Imported: current.Actions, PreservedSource: current.Source}, nil
}

func existingImport(ctx context.Context, preview MigrationPreview) (MigrationResult, error) {
	info, err := os.Lstat(preview.Destination)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("store: inspect existing migration destination: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return MigrationResult{}, errors.New("store: migration destination already exists")
	}
	db, err := sql.Open("sqlite", historyDSN(preview.Destination, true))
	if err != nil {
		return MigrationResult{}, errors.New("store: migration destination already exists")
	}
	defer db.Close() //nolint:errcheck // read-only idempotence check
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return MigrationResult{}, errors.New("store: migration destination already exists")
	}
	var source string
	var imported int
	if err := db.QueryRowContext(ctx, `SELECT source_path,imported_count FROM legacy_imports WHERE source_fingerprint=?`, preview.SourceFingerprint).Scan(&source, &imported); err != nil {
		return MigrationResult{}, errors.New("store: migration destination already exists")
	}
	if source != preview.Source || imported < 0 || imported != preview.Actions {
		return MigrationResult{}, errors.New("store: migration destination already exists")
	}
	return MigrationResult{Imported: imported, PreservedSource: source}, nil
}

func inspectLegacySnapshot(ctx context.Context, source, destination string, allowDestination bool) (MigrationPreview, string, error) {
	if err := ctx.Err(); err != nil {
		return MigrationPreview{}, "", fmt.Errorf("store: legacy migration canceled: %w", err)
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return MigrationPreview{}, "", fmt.Errorf("store: resolve legacy source: %w", err)
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return MigrationPreview{}, "", fmt.Errorf("store: resolve migration destination: %w", err)
	}
	if source == destination {
		return MigrationPreview{}, "", errors.New("store: migration source and destination must differ")
	}
	if _, err := os.Lstat(destination); err == nil && !allowDestination {
		return MigrationPreview{}, "", errors.New("store: migration destination already exists")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return MigrationPreview{}, "", fmt.Errorf("store: inspect migration destination: %w", err)
	}
	if len(source) > maxPathBytes || len(destination) > maxPathBytes {
		return MigrationPreview{}, "", errors.New("store: migration path exceeds limit")
	}
	info, err := os.Lstat(source)
	if err != nil {
		return MigrationPreview{}, "", fmt.Errorf("store: inspect legacy source: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return MigrationPreview{}, "", errors.New("store: legacy source must be a regular file")
	}
	if info.Size() > maxLegacySnapshotBytes {
		return MigrationPreview{}, "", errors.New("store: legacy database exceeds migration size limit")
	}
	walInfo, walErr := os.Stat(source + "-wal")
	if walErr == nil && walInfo.Size() > 0 {
		if walInfo.Size() > maxLegacySnapshotBytes {
			return MigrationPreview{}, "", errors.New("store: legacy WAL exceeds migration size limit")
		}
		if _, err := os.Stat(source + "-shm"); err != nil {
			return MigrationPreview{}, "", errors.New("store: active WAL source lacks readable shared-memory companion")
		}
	} else if walErr != nil && !errors.Is(walErr, os.ErrNotExist) {
		return MigrationPreview{}, "", fmt.Errorf("store: inspect legacy WAL: %w", walErr)
	}

	snapshotDir, err := os.MkdirTemp("", "workstation-doctor-legacy-*")
	if err != nil {
		return MigrationPreview{}, "", fmt.Errorf("store: create temporary migration directory: %w", err)
	}
	snapshot := filepath.Join(snapshotDir, "snapshot.db")
	snapshotFile, err := os.OpenFile(snapshot, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = os.RemoveAll(snapshotDir)
		return MigrationPreview{}, "", fmt.Errorf("store: create temporary migration snapshot: %w", err)
	}
	if err := snapshotFile.Close(); err != nil {
		_ = os.RemoveAll(snapshotDir)
		return MigrationPreview{}, "", fmt.Errorf("store: close temporary migration snapshot: %w", err)
	}
	cleanup := func(err error) (MigrationPreview, string, error) {
		_ = os.RemoveAll(snapshotDir)
		return MigrationPreview{}, "", err
	}
	if err := sqliteSnapshot(ctx, source, snapshot); err != nil {
		return cleanup(err)
	}
	if err := boundSnapshot(snapshot); err != nil {
		return cleanup(err)
	}
	fingerprint, err := hashFile(snapshot)
	if err != nil {
		return cleanup(err)
	}
	preview, err := countLegacySnapshot(ctx, source, destination, fingerprint, snapshot)
	if err != nil {
		return cleanup(err)
	}
	preview.SourceFingerprint = migrationFingerprint(fingerprint, preview)
	return preview, snapshot, nil
}

func sqliteSnapshot(ctx context.Context, source, destination string) error {
	db, err := sql.Open("sqlite", historyDSN(source, true))
	if err != nil {
		return fmt.Errorf("store: open legacy source read-only: %w", err)
	}
	defer db.Close() //nolint:errcheck // read-only source
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("store: read legacy source: %w", err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: acquire legacy source connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // source connection has no writes
	if err := conn.Raw(func(driverConn any) error {
		sourceConn, ok := driverConn.(interface {
			NewBackup(string) (*modernsqlite.Backup, error)
		})
		if !ok {
			return errors.New("sqlite connection lacks online backup support")
		}
		backup, err := sourceConn.NewBackup(sqliteURI(destination, "rw"))
		if err != nil {
			return fmt.Errorf("initialize consistent legacy snapshot: %w", err)
		}
		var stepErr error
		for {
			more, err := backup.Step(128)
			if err != nil {
				stepErr = fmt.Errorf("copy consistent legacy snapshot: %w", err)
				break
			}
			if err := ctx.Err(); err != nil {
				stepErr = fmt.Errorf("snapshot canceled: %w", err)
				break
			}
			if err := boundSnapshot(destination); err != nil {
				stepErr = err
				break
			}
			if !more {
				break
			}
		}
		finishErr := backup.Finish()
		if stepErr != nil {
			if finishErr != nil {
				return errors.Join(stepErr, fmt.Errorf("finish legacy snapshot: %w", finishErr))
			}
			return stepErr
		}
		if finishErr != nil {
			return fmt.Errorf("finish legacy snapshot: %w", finishErr)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("store: create online legacy snapshot: %w", err)
	}
	return nil
}

func sqliteURI(path, mode string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := url.Values{}
	query.Set("mode", mode)
	return u.String() + "?" + query.Encode()
}

func countLegacySnapshot(ctx context.Context, source, destination, fingerprint, snapshot string) (MigrationPreview, error) {
	db, err := sql.Open("sqlite", historyDSN(snapshot, true))
	if err != nil {
		return MigrationPreview{}, fmt.Errorf("store: open legacy snapshot: %w", err)
	}
	defer db.Close() //nolint:errcheck // read-only snapshot
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return MigrationPreview{}, fmt.Errorf("store: validate legacy snapshot: %w", err)
	}
	tables := map[string]bool{}
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' LIMIT 4`)
	if err != nil {
		return MigrationPreview{}, fmt.Errorf("store: inspect legacy snapshot schema: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return MigrationPreview{}, fmt.Errorf("store: read legacy snapshot schema: %w", err)
		}
		tables[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return MigrationPreview{}, fmt.Errorf("store: iterate legacy snapshot schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return MigrationPreview{}, fmt.Errorf("store: close legacy schema rows: %w", err)
	}
	if len(tables) != 3 || !tables["actions"] || !tables["check_runs"] || !tables["check_results"] {
		return MigrationPreview{}, errors.New("store: legacy schema lacks expected history tables")
	}
	var applicationID, userVersion int
	if err := db.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&applicationID); err != nil {
		return MigrationPreview{}, fmt.Errorf("store: read legacy application id: %w", err)
	}
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&userVersion); err != nil {
		return MigrationPreview{}, fmt.Errorf("store: read legacy schema version: %w", err)
	}
	if applicationID != 0 || userVersion != 0 {
		return MigrationPreview{}, errors.New("store: source is not a recognized legacy database")
	}
	if err := validateLegacyActionColumns(ctx, db); err != nil {
		return MigrationPreview{}, err
	}
	var actions, runs, results int
	for table, target := range map[string]*int{"actions": &actions, "check_runs": &runs, "check_results": &results} {
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(target); err != nil {
			return MigrationPreview{}, fmt.Errorf("store: count legacy %s: %w", table, err)
		}
	}
	if actions > 10_000 || runs > 100_000 || results > 100_000 {
		return MigrationPreview{}, errors.New("store: legacy history exceeds migration row limits")
	}
	return MigrationPreview{Source: source, Destination: destination, Schema: "legacy-v1", Actions: actions, CheckRuns: runs, CheckResults: results, SourceFingerprint: fingerprint}, nil
}

func migrationFingerprint(snapshotHash string, preview MigrationPreview) string {
	payload := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%d", snapshotHash, preview.Source, preview.Destination, preview.Schema, preview.Actions, preview.CheckRuns, preview.CheckResults)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func validateLegacyActionColumns(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(actions)`)
	if err != nil {
		return fmt.Errorf("store: inspect legacy action columns: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only snapshot
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notnull, primary int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notnull, &defaultValue, &primary); err != nil {
			return fmt.Errorf("store: inspect legacy action columns: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: read legacy action columns: %w", err)
	}
	for _, name := range []string{"id", "kind", "status", "started_at", "finished_at"} {
		if !columns[name] {
			return fmt.Errorf("store: unsupported legacy schema: actions missing %s", name)
		}
	}
	return nil
}

func boundSnapshot(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("store: inspect migration snapshot: %w", err)
	}
	if info.Size() > maxLegacySnapshotBytes {
		return fmt.Errorf("store: legacy snapshot exceeds %d-byte limit", maxLegacySnapshotBytes)
	}
	return nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("store: open migration snapshot for fingerprint: %w", err)
	}
	defer file.Close() //nolint:errcheck // read-only snapshot
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, maxLegacySnapshotBytes+1)); err != nil {
		return "", fmt.Errorf("store: fingerprint migration snapshot: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func importLegacySnapshot(ctx context.Context, history *HistoryStore, snapshot string, preview MigrationPreview) error {
	db, err := sql.Open("sqlite", historyDSN(snapshot, true))
	if err != nil {
		return fmt.Errorf("store: open legacy snapshot for import: %w", err)
	}
	defer db.Close() //nolint:errcheck // read-only snapshot
	db.SetMaxOpenConns(1)
	rows, err := db.QueryContext(ctx, `SELECT id,CASE WHEN length(CAST(kind AS BLOB))<=32 THEN kind ELSE '' END,CASE WHEN length(CAST(status AS BLOB))<=32 THEN status ELSE '' END,CASE WHEN length(CAST(started_at AS BLOB))<=64 THEN started_at END,CASE WHEN length(CAST(finished_at AS BLOB))<=64 THEN finished_at END FROM actions ORDER BY id`)
	if err != nil {
		return fmt.Errorf("store: read legacy action rows: %w", err)
	}
	type legacyAction struct {
		id                int64
		kind, status      string
		started, finished sql.NullString
	}
	actions := make([]legacyAction, 0, preview.Actions)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: read legacy actions canceled: %w", err)
		}
		var row legacyAction
		if err := rows.Scan(&row.id, &row.kind, &row.status, &row.started, &row.finished); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: scan legacy action row: %w", err)
		}
		actions = append(actions, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("store: iterate legacy action rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("store: close legacy action rows: %w", err)
	}

	tx, err := history.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin atomic legacy import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, legacy := range actions {
		if legacy.id <= 0 || !legacy.started.Valid || !legacy.finished.Valid {
			return errors.New("store: legacy action has invalid identity or timestamps")
		}
		started, err := parseHistoryTime(legacy.started.String)
		if err != nil {
			return fmt.Errorf("store: parse legacy action start: %w", err)
		}
		finished, err := parseHistoryTime(legacy.finished.String)
		if err != nil || finished.Before(started) {
			return errors.New("store: legacy action has invalid timestamps")
		}
		id := fmt.Sprintf("legacy-%s-%d", preview.SourceFingerprint, legacy.id)
		kind := safeLegacyKind(legacy.kind)
		outcome, stepOutcome, knownOutcome := legacyOutcomes(legacy.status)
		if !knownOutcome {
			return fmt.Errorf("store: unsupported legacy action outcome %q", legacy.status)
		}
		plan := PlannedStep{Index: 0, Label: "Legacy action", Description: "Historical outcome imported; original command and output were discarded"}
		start := ActionStart{
			ID: id, IntegrationID: "legacy", CheckID: "legacy-action", InstanceID: "legacy-unknown",
			Label: "Imported legacy maintenance action", Kind: kind, Reason: "Imported from preserved legacy history",
			StartedAt: started, Scope: "legacy-unknown", OwnerToken: "legacy-import",
			Plan: []PlannedStep{plan}, TargetIDs: []string{"legacy-unknown"}, MetadataVersion: 0,
		}
		targets, metadata, err := validateActionStart(start, history.policy)
		if err != nil {
			return fmt.Errorf("store: validate imported legacy action: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO actions(id,batch_id,integration_id,check_id,instance_id,label,kind,reason,app_version,installed_version,target_version,manager,root,scope,owner_token,started_at,target_ids,metadata_version,metadata_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			start.ID, start.BatchID, start.IntegrationID, start.CheckID, start.InstanceID, start.Label, start.Kind, start.Reason, start.AppVersion, start.InstalledVersion, start.TargetVersion, start.Manager, start.Root, start.Scope, start.OwnerToken, tsHistory(start.StartedAt), string(targets), start.MetadataVersion, string(metadata)); err != nil {
			return fmt.Errorf("store: write imported legacy action: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO steps(action_id,step_index,label,description) VALUES(?,?,?,?)`, id, plan.Index, plan.Label, plan.Description); err != nil {
			return fmt.Errorf("store: write imported legacy step: %w", err)
		}
		if stepOutcome != StepOutcomeUnspecified {
			if _, err := tx.ExecContext(ctx, `UPDATE steps SET outcome=?,started_at=?,finished_at=? WHERE action_id=? AND step_index=?`, stepOutcome, tsHistory(started), tsHistory(finished), id, plan.Index); err != nil {
				return fmt.Errorf("store: write imported legacy step outcome: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE actions SET finished_at=?,execution=?,verification=?,observed_version='',safe_error='' WHERE id=?`, tsHistory(finished), outcome, VerificationUnknown, id); err != nil {
			return fmt.Errorf("store: finish imported legacy action: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_imports(source_fingerprint,source_path,imported_at,imported_count) VALUES(?,?,?,?)`, preview.SourceFingerprint, preview.Source, tsHistory(time.Now()), len(actions)); err != nil {
		return fmt.Errorf("store: save import marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit legacy import: %w", err)
	}
	return nil
}

func safeLegacyKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fix":
		return "fix"
	default:
		return "legacy-action"
	}
}

func legacyOutcomes(status string) (ExecutionOutcome, StepOutcome, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "success", "completed":
		return ExecutionCompleted, StepOutcomeCompleted, true
	case "fail", "failed", "error":
		return ExecutionFailed, StepOutcomeFailed, true
	default:
		return ExecutionUnspecified, StepOutcomeUnspecified, false
	}
}
