// Package store manages SQLite action history persistence.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const actionSchema = `
CREATE TABLE actions (
  id TEXT PRIMARY KEY NOT NULL,
  batch_id TEXT NOT NULL,
  integration_id TEXT NOT NULL,
  check_id TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  label TEXT NOT NULL,
  kind TEXT NOT NULL,
  reason TEXT NOT NULL,
  app_version TEXT NOT NULL,
  installed_version TEXT NOT NULL,
  target_version TEXT NOT NULL,
  manager TEXT NOT NULL,
  root TEXT NOT NULL,
  scope TEXT NOT NULL,
  owner_token TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  execution TEXT CHECK (execution IS NULL OR execution IN ('Completed','Failed','Canceled','Blocked','Interrupted')),
  verification TEXT CHECK (verification IS NULL OR verification IN ('Passed','Failed','Unknown','NotPerformed')),
  observed_version TEXT,
  safe_error TEXT NOT NULL DEFAULT '',
  target_ids TEXT NOT NULL,
  metadata_version INTEGER NOT NULL,
  metadata_json TEXT NOT NULL,
  CHECK ((finished_at IS NULL AND execution IS NULL AND verification IS NULL AND observed_version IS NULL)
      OR (finished_at IS NOT NULL AND execution IS NOT NULL AND verification IS NOT NULL AND observed_version IS NOT NULL))
);
CREATE TABLE steps (
  action_id TEXT NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
  step_index INTEGER NOT NULL CHECK (step_index >= 0 AND step_index < 32),
  label TEXT NOT NULL,
  description TEXT NOT NULL,
  outcome TEXT CHECK (outcome IS NULL OR outcome IN ('Completed','Failed','Canceled','NotStarted')),
  exit_code INTEGER,
  safe_error TEXT NOT NULL DEFAULT '',
  safe_output TEXT NOT NULL DEFAULT '',
  output_truncated INTEGER NOT NULL DEFAULT 0 CHECK (output_truncated IN (0,1)),
  started_at TEXT,
  finished_at TEXT,
  PRIMARY KEY (action_id, step_index)
);
CREATE INDEX idx_actions_started_id ON actions(started_at DESC, id DESC);
CREATE INDEX idx_actions_integration_started ON actions(integration_id, started_at DESC, id DESC);
CREATE INDEX idx_actions_instance_started ON actions(instance_id, started_at DESC, id DESC);
CREATE INDEX idx_actions_batch_started ON actions(batch_id, started_at DESC, id DESC);
CREATE INDEX idx_actions_execution_started ON actions(execution, started_at DESC, id DESC);
CREATE INDEX idx_actions_verification_started ON actions(verification, started_at DESC, id DESC);
CREATE INDEX idx_actions_finished_started ON actions(finished_at, started_at DESC, id DESC);
CREATE TABLE legacy_imports (
  source_fingerprint TEXT PRIMARY KEY NOT NULL,
  source_path TEXT NOT NULL,
  imported_at TEXT NOT NULL,
  imported_count INTEGER NOT NULL CHECK (imported_count >= 0)
);
`

// HistoryStore owns a bounded SQLite pool for maintenance history only.
type HistoryStore struct {
	db     *sql.DB
	policy Policy
}

// HistorySchemaError reports a recognized schema that must not be opened implicitly.
type HistorySchemaError struct{ Kind DBKind }

func (e *HistorySchemaError) Error() string {
	switch e.Kind {
	case DBKindLegacy:
		return ErrMigrationRequired.Error()
	default:
		return ErrUnsupportedSchema.Error()
	}
}

// Is matches the migration or unsupported-schema sentinel for this history kind.
func (e *HistorySchemaError) Is(target error) bool {
	return e.Kind == DBKindLegacy && target == ErrMigrationRequired || e.Kind == DBKindUnsupported && target == ErrUnsupportedSchema
}

// InspectHistory classifies an existing database without creating or modifying it.
func InspectHistory(ctx context.Context, path string) (DBKind, error) {
	if err := ctx.Err(); err != nil {
		return DBKindUnsupported, fmt.Errorf("store: inspect history canceled: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil || strings.TrimSpace(path) == "" {
		return DBKindUnsupported, fmt.Errorf("store: invalid history path")
	}
	info, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return DBKindMissing, nil
	}
	if err != nil {
		return DBKindUnsupported, fmt.Errorf("store: inspect history path: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return DBKindUnsupported, fmt.Errorf("store: history path is not a regular file")
	}
	db, err := sql.Open("sqlite", historyDSN(abs, true))
	if err != nil {
		return DBKindUnsupported, fmt.Errorf("store: open history read-only: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close() //nolint:errcheck // read-only inspection has no pending writes
	if err := db.PingContext(ctx); err != nil {
		return DBKindUnsupported, fmt.Errorf("store: inspect history database: %w", err)
	}
	var applicationID, userVersion int
	if err := db.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&applicationID); err != nil {
		return DBKindUnsupported, fmt.Errorf("store: read history application id: %w", err)
	}
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&userVersion); err != nil {
		return DBKindUnsupported, fmt.Errorf("store: read history schema version: %w", err)
	}
	tables := make(map[string]bool)
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return DBKindUnsupported, fmt.Errorf("store: inspect history tables: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return DBKindUnsupported, fmt.Errorf("store: inspect history tables: %w", err)
		}
		tables[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return DBKindUnsupported, fmt.Errorf("store: inspect history tables: %w", err)
	}
	if err := rows.Close(); err != nil {
		return DBKindUnsupported, fmt.Errorf("store: close history table inspection: %w", err)
	}
	if tables["check_runs"] || tables["check_results"] || tables["actions"] && userVersion == 0 {
		return DBKindLegacy, nil
	}
	expectedTables := 2
	if userVersion >= 3 {
		expectedTables = 3
	}
	if applicationID != historyApplicationID || userVersion < 1 || userVersion > historySchemaVersion || !tables["actions"] || !tables["steps"] || userVersion >= 3 && !tables["legacy_imports"] || len(tables) != expectedTables {
		return DBKindUnsupported, fmt.Errorf("store: unrecognized history schema")
	}
	if err := validateHistoryColumns(ctx, db, userVersion); err != nil {
		return DBKindUnsupported, err
	}
	return DBKindActionOnly, nil
}

func validateHistoryColumns(ctx context.Context, db *sql.DB, version int) error {
	for table, required := range map[string][]string{
		"actions": {"id", "batch_id", "integration_id", "check_id", "instance_id", "label", "kind", "reason", "app_version", "installed_version", "target_version", "manager", "root", "scope", "owner_token", "started_at", "finished_at", "execution", "verification", "observed_version", "safe_error", "target_ids", "metadata_version", "metadata_json"},
		"steps":   {"action_id", "step_index", "label", "description", "outcome", "exit_code", "safe_error", "safe_output", "output_truncated", "started_at", "finished_at"},
	} {
		rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
		if err != nil {
			return fmt.Errorf("store: inspect %s columns: %w", table, err)
		}
		columns := make(map[string]bool)
		for rows.Next() {
			var cid, notnull, pk int
			var name, dataType string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &dataType, &notnull, &defaultValue, &pk); err != nil {
				_ = rows.Close()
				return fmt.Errorf("store: inspect %s columns: %w", table, err)
			}
			columns[name] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: inspect %s columns: %w", table, err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("store: close %s column inspection: %w", table, err)
		}
		for _, name := range required {
			if !columns[name] {
				return fmt.Errorf("store: unrecognized history schema: missing %s.%s", table, name)
			}
		}
	}
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_list(steps)`)
	if err != nil {
		return fmt.Errorf("store: inspect step foreign key: %w", err)
	}
	defer rows.Close() //nolint:errcheck // schema inspection has no pending writes
	found := false
	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return fmt.Errorf("store: inspect step foreign key: %w", err)
		}
		if table == "actions" && from == "action_id" && to == "id" && strings.EqualFold(onDelete, "CASCADE") {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: inspect step foreign key: %w", err)
	}
	if !found {
		return errors.New("store: unrecognized history schema: steps lack the action foreign key")
	}
	indexes := map[string]bool{}
	indexRows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='actions'`)
	if err != nil {
		return fmt.Errorf("store: inspect history indexes: %w", err)
	}
	for indexRows.Next() {
		var name string
		if err := indexRows.Scan(&name); err != nil {
			_ = indexRows.Close()
			return fmt.Errorf("store: inspect history indexes: %w", err)
		}
		indexes[name] = true
	}
	if err := indexRows.Err(); err != nil {
		_ = indexRows.Close()
		return fmt.Errorf("store: inspect history indexes: %w", err)
	}
	if err := indexRows.Close(); err != nil {
		return fmt.Errorf("store: close history index inspection: %w", err)
	}
	requiredIndexes := []string{"idx_actions_started_id", "idx_actions_integration_started", "idx_actions_instance_started"}
	if version >= 2 {
		requiredIndexes = append(requiredIndexes, "idx_actions_batch_started", "idx_actions_execution_started", "idx_actions_verification_started", "idx_actions_finished_started")
	}
	for _, name := range requiredIndexes {
		if !indexes[name] {
			return fmt.Errorf("store: unrecognized history schema: missing index %s", name)
		}
	}
	if version >= 3 {
		rows, err := db.QueryContext(ctx, `PRAGMA table_info(legacy_imports)`)
		if err != nil {
			return fmt.Errorf("store: inspect legacy import marker: %w", err)
		}
		columns := map[string]bool{}
		for rows.Next() {
			var cid, notnull, pk int
			var name, dataType string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &dataType, &notnull, &defaultValue, &pk); err != nil {
				_ = rows.Close()
				return fmt.Errorf("store: inspect legacy import marker: %w", err)
			}
			columns[name] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: inspect legacy import marker: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("store: close legacy import marker: %w", err)
		}
		for _, name := range []string{"source_fingerprint", "source_path", "imported_at", "imported_count"} {
			if !columns[name] {
				return fmt.Errorf("store: unrecognized history schema: missing legacy_imports.%s", name)
			}
		}
	}
	return nil
}

// OpenHistory opens a validated action-only database or creates a new private one.
// Legacy and unknown schemas are never altered or migrated implicitly.
func OpenHistory(ctx context.Context, path string, policy Policy) (*HistoryStore, error) {
	if err := validatePolicy(policy); err != nil {
		return nil, fmt.Errorf("store: invalid policy: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("store: open history canceled: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil || strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("store: invalid history path")
	}
	if err := prepareHistoryDirectory(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	kind, err := InspectHistory(ctx, abs)
	if err != nil && kind != DBKindUnsupported {
		return nil, err
	}
	if kind == DBKindLegacy || kind == DBKindUnsupported {
		return nil, &HistorySchemaError{Kind: kind}
	}
	created := false
	if kind == DBKindMissing {
		file, createErr := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case errors.Is(createErr, os.ErrExist):
			// Another opener won the race; accept only its complete action-only schema.
			kind, err = InspectHistory(ctx, abs)
			if err != nil || kind != DBKindActionOnly {
				if kind == DBKindLegacy {
					return nil, &HistorySchemaError{Kind: kind}
				}
				return nil, &HistorySchemaError{Kind: DBKindUnsupported}
			}
		case createErr != nil:
			return nil, fmt.Errorf("store: create private history file: %w", createErr)
		default:
			created = true
			if closeErr := file.Close(); closeErr != nil {
				return nil, fmt.Errorf("store: close new history file: %w", closeErr)
			}
		}
	}
	db, err := sql.Open("sqlite", historyDSN(abs, false))
	if err != nil {
		return nil, fmt.Errorf("store: open history database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		return nil, closeHistoryOnError(db, fmt.Errorf("store: ping history database: %w", err), abs, created)
	}
	if created {
		if err := createHistorySchema(ctx, db); err != nil {
			return nil, closeHistoryOnError(db, fmt.Errorf("store: initialize action history: %w", err), abs, true)
		}
	} else {
		kind, err := InspectHistory(ctx, abs)
		if err != nil || kind != DBKindActionOnly {
			if kind == DBKindLegacy {
				return nil, closeHistoryOnError(db, &HistorySchemaError{Kind: kind}, abs, false)
			}
			return nil, closeHistoryOnError(db, &HistorySchemaError{Kind: DBKindUnsupported}, abs, false)
		}
		if err := migrateHistorySchema(ctx, db); err != nil {
			return nil, closeHistoryOnError(db, fmt.Errorf("store: upgrade action history schema: %w", err), abs, false)
		}
		kind, err = InspectHistory(ctx, abs)
		if err != nil || kind != DBKindActionOnly {
			return nil, closeHistoryOnError(db, &HistorySchemaError{Kind: DBKindUnsupported}, abs, false)
		}
	}
	if err := os.Chmod(abs, 0o600); err != nil {
		return nil, closeHistoryOnError(db, fmt.Errorf("store: secure history file permissions: %w", err), abs, created)
	}
	return &HistoryStore{db: db, policy: policy}, nil
}

func validatePolicy(policy Policy) error {
	if policy.MaxAge <= 0 || policy.MaxTerminal <= 0 || policy.MaxStepOutput <= 0 || policy.MaxActionOutput <= 0 || policy.MaxMetadata <= 0 {
		return errors.New("all history policy limits must be positive")
	}
	if policy.MaxStepOutput > maxStepOutputHard || policy.MaxActionOutput > maxActionOutputHard || policy.MaxMetadata > maxMetadataHard || policy.MaxActionOutput < policy.MaxStepOutput {
		return errors.New("history policy exceeds hard limits or has inconsistent output caps")
	}
	return nil
}

func prepareHistoryDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("store: create private history directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("store: inspect history directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("store: history parent is not a directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("store: history directory permissions are not private")
	}
	return nil
}

func closeHistoryOnError(db *sql.DB, cause error, path string, created bool) error {
	if err := db.Close(); err != nil {
		cause = errors.Join(cause, fmt.Errorf("store: close history database: %w", err))
	}
	if created {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cause = errors.Join(cause, fmt.Errorf("store: remove incomplete history database: %w", err))
		}
	}
	return cause
}

func createHistorySchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, actionSchema); err != nil {
		return fmt.Errorf("create action tables: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA application_id = %d`, historyApplicationID)); err != nil {
		return fmt.Errorf("set application id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, historySchemaVersion)); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema transaction: %w", err)
	}
	return nil
}

func historyDSN(path string, readOnly bool) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := url.Values{}
	query.Set("_busy_timeout", "5000")
	query.Set("_foreign_keys", "on")
	if readOnly {
		query.Set("mode", "ro")
		query.Set("_query_only", "on")
	}
	return u.String() + "?" + query.Encode()
}

func validateActionStart(start ActionStart, policy Policy) ([]byte, []byte, error) {
	return validateActionContext(start, policy, true)
}

func validateActionContext(start ActionStart, policy Policy, requirePlan bool) ([]byte, []byte, error) {
	fixed := []struct {
		name, value string
		max         int
		required    bool
	}{
		{"id", start.ID, maxIdentifierBytes, true},
		{"batch id", start.BatchID, maxIdentifierBytes, false},
		{"integration id", start.IntegrationID, maxIdentifierBytes, true},
		{"check id", start.CheckID, maxIdentifierBytes, true},
		{"instance id", start.InstanceID, maxIdentifierBytes, true},
		{"label", start.Label, maxLabelBytes, true},
		{"kind", start.Kind, maxIdentifierBytes, true},
		{"reason", start.Reason, maxNoteBytes, true},
		{"app version", start.AppVersion, maxIdentifierBytes, false},
		{"installed version", start.InstalledVersion, maxIdentifierBytes, false},
		{"target version", start.TargetVersion, maxIdentifierBytes, false},
		{"manager", start.Manager, maxIdentifierBytes, false},
		{"root", start.Root, maxPathBytes, false},
		{"scope", start.Scope, maxIdentifierBytes, true},
		{"owner token", start.OwnerToken, maxIdentifierBytes, true},
	}
	for _, field := range fixed {
		if err := validateText(field.value, field.name, field.max, field.required); err != nil {
			return nil, nil, err
		}
	}
	if start.StartedAt.IsZero() {
		return nil, nil, errors.New("start time is required")
	}
	if len(start.Plan) > maxActionSteps {
		return nil, nil, fmt.Errorf("plan may contain at most %d steps", maxActionSteps)
	}
	if requirePlan && len(start.Plan) == 0 {
		return nil, nil, errors.New("plan must contain at least one step")
	}
	for i, step := range start.Plan {
		if step.Index != i {
			return nil, nil, fmt.Errorf("plan step index %d is not contiguous at position %d", step.Index, i)
		}
		if err := validateText(step.Label, "step label", maxLabelBytes, true); err != nil {
			return nil, nil, err
		}
		if err := validateText(step.Description, "step description", maxNoteBytes, true); err != nil {
			return nil, nil, err
		}
	}
	if len(start.TargetIDs) == 0 || len(start.TargetIDs) > maxActionTargets {
		return nil, nil, fmt.Errorf("target count must be 1 to %d", maxActionTargets)
	}
	seen := make(map[string]bool, len(start.TargetIDs))
	for _, id := range start.TargetIDs {
		if err := validateText(id, "target id", maxIdentifierBytes, true); err != nil {
			return nil, nil, err
		}
		if seen[id] {
			return nil, nil, fmt.Errorf("duplicate target id %q", id)
		}
		seen[id] = true
	}
	if start.MetadataVersion == 0 && len(start.Metadata) != 0 || start.MetadataVersion != 0 && start.MetadataVersion != 1 {
		return nil, nil, errors.New("unsupported metadata version")
	}
	allowed := map[string]bool{
		"installed_evidence_source": true, "intended_evidence_source": true, "observed_evidence_source": true,
		"precondition_summary": true, "installation_time_source": true, "installation_time_meaning": true,
		"installation_time_precision": true,
	}
	for key, value := range start.Metadata {
		if !allowed[key] {
			return nil, nil, fmt.Errorf("metadata key %q is not allowlisted", key)
		}
		if err := validateText(value, "metadata value", maxNoteBytes, true); err != nil {
			return nil, nil, err
		}
	}
	targets, err := json.Marshal(start.TargetIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("encode target ids: %w", err)
	}
	metadataValues := start.Metadata
	if metadataValues == nil {
		metadataValues = map[string]string{}
	}
	metadata, err := json.Marshal(metadataValues)
	if err != nil {
		return nil, nil, fmt.Errorf("encode metadata: %w", err)
	}
	if len(metadata) > policy.MaxMetadata {
		return nil, nil, fmt.Errorf("metadata exceeds %d-byte limit", policy.MaxMetadata)
	}
	return targets, metadata, nil
}

func validateText(value, field string, limit int, required bool) error {
	if required && value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > limit {
		return fmt.Errorf("%s exceeds %d bytes", field, limit)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", field)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s contains a control character", field)
		}
	}
	return nil
}

func validateStepResult(step StepResult) error {
	if step.Index < 0 || step.Index >= maxActionSteps {
		return fmt.Errorf("step index must be between 0 and %d", maxActionSteps-1)
	}
	switch step.Outcome {
	case StepOutcomeCompleted, StepOutcomeFailed, StepOutcomeCanceled:
		if step.StartedAt.IsZero() || step.FinishedAt.IsZero() || step.FinishedAt.Before(step.StartedAt) {
			return errors.New("started step requires valid ordered timestamps")
		}
	case StepOutcomeNotStarted:
		if !step.StartedAt.IsZero() || !step.FinishedAt.IsZero() || step.ExitCode != nil || step.SafeOutput != "" || step.OutputTruncated {
			return errors.New("not-started step cannot contain execution evidence")
		}
	default:
		return errors.New("step outcome is unspecified or invalid")
	}
	if err := validateSafeExcerpt(step.SafeError, "safe step error", maxSafeErrorBytes); err != nil {
		return err
	}
	if err := validateSafeExcerpt(step.SafeOutput, "safe step output", maxActionOutputHard); err != nil {
		return err
	}
	return nil
}

func validateFinish(finish ActionFinish) error {
	if finish.FinishedAt.IsZero() {
		return errors.New("finish time is required")
	}
	switch finish.Execution {
	case ExecutionCompleted, ExecutionFailed, ExecutionCanceled, ExecutionBlocked, ExecutionInterrupted:
	default:
		return errors.New("execution outcome is unspecified or invalid")
	}
	switch finish.Verification {
	case VerificationPassed, VerificationFailed, VerificationUnknown, VerificationNotPerformed:
	default:
		return errors.New("verification outcome is unspecified or invalid")
	}
	if err := validateText(finish.ObservedVersion, "observed version", maxIdentifierBytes, false); err != nil {
		return err
	}
	return validateSafeExcerpt(finish.SafeError, "safe action error", maxSafeErrorBytes)
}

func validateSafeExcerpt(value, field string, limit int) error {
	if len(value) > limit {
		return fmt.Errorf("%s exceeds %d bytes", field, limit)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", field)
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return fmt.Errorf("%s contains a control character", field)
		}
	}
	return nil
}

func tsHistory(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

// StartAction durably stores an action and all planned steps atomically.
func (s *HistoryStore) StartAction(ctx context.Context, start ActionStart) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("store: start action canceled: %w", err)
	}
	targets, metadata, err := validateActionStart(start, s.policy)
	if err != nil {
		return fmt.Errorf("store: invalid action start: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin action start: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO actions(
		id,batch_id,integration_id,check_id,instance_id,label,kind,reason,app_version,installed_version,target_version,
		manager,root,scope,owner_token,started_at,target_ids,metadata_version,metadata_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		start.ID, start.BatchID, start.IntegrationID, start.CheckID, start.InstanceID, start.Label, start.Kind, start.Reason,
		start.AppVersion, start.InstalledVersion, start.TargetVersion, start.Manager, start.Root, start.Scope, start.OwnerToken,
		tsHistory(start.StartedAt), string(targets), start.MetadataVersion, string(metadata))
	if err != nil {
		return fmt.Errorf("store: save action start: %w", err)
	}
	for _, step := range start.Plan {
		if _, err := tx.ExecContext(ctx, `INSERT INTO steps(action_id,step_index,label,description) VALUES(?,?,?,?)`, start.ID, step.Index, step.Label, step.Description); err != nil {
			return fmt.Errorf("store: save planned step %d: %w", step.Index, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit action start: %w", err)
	}
	return nil
}

// SaveStep checkpoints exactly one planned step without allowing replay or duplication.
func (s *HistoryStore) SaveStep(ctx context.Context, actionID string, step StepResult) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("store: checkpoint step canceled: %w", err)
	}
	if err := validateText(actionID, "action id", maxIdentifierBytes, true); err != nil {
		return fmt.Errorf("store: invalid action id: %w", err)
	}
	if err := validateStepResult(step); err != nil {
		return fmt.Errorf("store: invalid step result: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin step checkpoint: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var finished sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT finished_at FROM actions WHERE id=?`, actionID).Scan(&finished); err != nil {
		return fmt.Errorf("store: find action for step: %w", err)
	}
	if finished.Valid {
		return errors.New("store: cannot checkpoint a finished action")
	}
	var oldOutcome sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT outcome FROM steps WHERE action_id=? AND step_index=?`, actionID, step.Index).Scan(&oldOutcome); err != nil {
		return fmt.Errorf("store: find planned step: %w", err)
	}
	if oldOutcome.Valid {
		return errors.New("store: step already checkpointed")
	}
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(length(CAST(safe_output AS BLOB))),0) FROM steps WHERE action_id=? AND outcome IS NOT NULL`, actionID).Scan(&existing); err != nil {
		return fmt.Errorf("store: measure retained action output: %w", err)
	}
	output := step.SafeOutput
	truncated := step.OutputTruncated
	if len(output) > s.policy.MaxStepOutput {
		output = safePrefix(output, s.policy.MaxStepOutput)
		truncated = true
	}
	remaining := s.policy.MaxActionOutput - existing
	remaining = max(remaining, 0)
	if len(output) > remaining {
		output = safePrefix(output, remaining)
		truncated = true
	}
	var exitCode any
	if step.ExitCode != nil {
		exitCode = *step.ExitCode
	}
	var startedAt, finishedAt any
	if !step.StartedAt.IsZero() {
		startedAt = tsHistory(step.StartedAt)
	}
	if !step.FinishedAt.IsZero() {
		finishedAt = tsHistory(step.FinishedAt)
	}
	res, err := tx.ExecContext(ctx, `UPDATE steps SET outcome=?,exit_code=?,safe_error=?,safe_output=?,output_truncated=?,started_at=?,finished_at=?
		WHERE action_id=? AND step_index=? AND outcome IS NULL`,
		step.Outcome, exitCode, step.SafeError, output, truncated, startedAt, finishedAt, actionID, step.Index)
	if err != nil {
		return fmt.Errorf("store: save step checkpoint: %w", err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: inspect step checkpoint: %w", err)
	}
	if changed != 1 {
		return errors.New("store: planned step not found or already checkpointed")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit step checkpoint: %w", err)
	}
	return nil
}

func safePrefix(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	prefix := value[:limit]
	for !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix
}

// FinishAction records execution and verification outcomes once for an action.
func (s *HistoryStore) FinishAction(ctx context.Context, actionID string, finish ActionFinish) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("store: finish action canceled: %w", err)
	}
	if err := validateText(actionID, "action id", maxIdentifierBytes, true); err != nil {
		return fmt.Errorf("store: invalid action id: %w", err)
	}
	if err := validateFinish(finish); err != nil {
		return fmt.Errorf("store: invalid action finish: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin action finish: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var started string
	if err := tx.QueryRowContext(ctx, `SELECT started_at FROM actions WHERE id=? AND finished_at IS NULL`, actionID).Scan(&started); err != nil {
		return fmt.Errorf("store: find unfinished action: %w", err)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, started)
	if err != nil {
		return fmt.Errorf("store: parse action start time: %w", err)
	}
	if finish.FinishedAt.Before(startedAt) {
		return errors.New("store: action finish precedes start")
	}
	res, err := tx.ExecContext(ctx, `UPDATE actions SET finished_at=?,execution=?,verification=?,observed_version=?,safe_error=?
		WHERE id=? AND finished_at IS NULL`, tsHistory(finish.FinishedAt), finish.Execution, finish.Verification,
		finish.ObservedVersion, finish.SafeError, actionID)
	if err != nil {
		return fmt.Errorf("store: finish action: %w", err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: inspect action finish: %w", err)
	}
	if changed != 1 {
		return errors.New("store: action is already finished or missing")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit action finish: %w", err)
	}
	return nil
}

// Close closes the history connection pool.
func (s *HistoryStore) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("store: close action history: %w", err)
	}
	return nil
}
