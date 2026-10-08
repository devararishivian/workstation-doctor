package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const historyActionColumns = `id,batch_id,integration_id,check_id,instance_id,label,kind,reason,app_version,installed_version,target_version,manager,root,scope,owner_token,started_at,finished_at,execution,verification,observed_version,safe_error,target_ids,metadata_version,metadata_json`

type historyCursor struct {
	startedAt string
	id        string
}

type historyScanner interface {
	Scan(dest ...any) error
}

// ListActions returns a bounded newest-first page and omits step bodies.
func (s *HistoryStore) ListActions(ctx context.Context, query ActionQuery) (ActionPage, error) {
	if err := ctx.Err(); err != nil {
		return ActionPage{}, fmt.Errorf("store: list actions canceled: %w", err)
	}
	if query.Limit == 0 {
		query.Limit = 50
	}
	if query.Limit < 1 || query.Limit > 200 {
		return ActionPage{}, errors.New("store: history page limit must be between 1 and 200")
	}
	for name, value := range map[string]string{
		"integration id": query.IntegrationID, "instance id": query.InstanceID, "batch id": query.BatchID,
	} {
		if err := validateText(value, name, maxIdentifierBytes, false); err != nil {
			return ActionPage{}, fmt.Errorf("store: invalid history filter: %w", err)
		}
	}
	if query.Execution != ExecutionUnspecified && !validExecution(query.Execution) {
		return ActionPage{}, errors.New("store: invalid execution filter")
	}
	if query.Verification != VerificationUnspecified && !validVerification(query.Verification) {
		return ActionPage{}, errors.New("store: invalid verification filter")
	}
	if query.From != nil && query.From.IsZero() || query.To != nil && query.To.IsZero() {
		return ActionPage{}, errors.New("store: history time filters must be non-zero")
	}
	if query.From != nil && query.To != nil && query.From.After(*query.To) {
		return ActionPage{}, errors.New("store: history start time filter is after end time")
	}
	var cursor historyCursor
	if query.Cursor != "" {
		var err error
		cursor, err = decodeHistoryCursor(query.Cursor)
		if err != nil {
			return ActionPage{}, err
		}
	}
	conditions := make([]string, 0, 8)
	args := make([]any, 0, 11)
	if query.IntegrationID != "" {
		conditions = append(conditions, "integration_id=?")
		args = append(args, query.IntegrationID)
	}
	if query.InstanceID != "" {
		conditions = append(conditions, "instance_id=?")
		args = append(args, query.InstanceID)
	}
	if query.BatchID != "" {
		conditions = append(conditions, "batch_id=?")
		args = append(args, query.BatchID)
	}
	if query.Execution != ExecutionUnspecified {
		conditions = append(conditions, "execution=?")
		args = append(args, query.Execution)
	}
	if query.Verification != VerificationUnspecified {
		conditions = append(conditions, "verification=?")
		args = append(args, query.Verification)
	}
	if query.From != nil {
		conditions = append(conditions, "started_at>=?")
		args = append(args, tsHistory(*query.From))
	}
	if query.To != nil {
		conditions = append(conditions, "started_at<=?")
		args = append(args, tsHistory(*query.To))
	}
	if query.Cursor != "" {
		conditions = append(conditions, "(started_at<? OR (started_at=? AND id<?))")
		args = append(args, cursor.startedAt, cursor.startedAt, cursor.id)
	}
	statement := `SELECT ` + historyActionColumns + ` FROM actions`
	if len(conditions) != 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += " ORDER BY started_at DESC,id DESC LIMIT ?"
	args = append(args, query.Limit+1)
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return ActionPage{}, fmt.Errorf("store: list actions: %w", err)
	}
	defer rows.Close() //nolint:errcheck // completed read-only query
	page := ActionPage{Items: make([]ActionRecord, 0, query.Limit)}
	for rows.Next() {
		record, err := scanHistoryAction(rows, s.policy)
		if err != nil {
			return ActionPage{}, err
		}
		if len(page.Items) == query.Limit {
			last := page.Items[len(page.Items)-1]
			page.NextCursor, err = encodeHistoryCursor(historyCursor{startedAt: tsHistory(last.StartedAt), id: last.ID})
			if err != nil {
				return ActionPage{}, err
			}
			break
		}
		page.Items = append(page.Items, record)
	}
	if err := rows.Err(); err != nil {
		return ActionPage{}, fmt.Errorf("store: iterate action page: %w", err)
	}
	return page, nil
}

func scanHistoryAction(scanner historyScanner, policy Policy) (ActionRecord, error) {
	var record ActionRecord
	var startedAt string
	var finished, execution, verification, observed sql.NullString
	var safeError, targetsJSON, metadataJSON string
	if err := scanner.Scan(
		&record.ID, &record.BatchID, &record.IntegrationID, &record.CheckID, &record.InstanceID,
		&record.Label, &record.Kind, &record.Reason, &record.AppVersion, &record.InstalledVersion,
		&record.TargetVersion, &record.Manager, &record.Root, &record.Scope, &record.OwnerToken,
		&startedAt, &finished, &execution, &verification, &observed, &safeError,
		&targetsJSON, &record.MetadataVersion, &metadataJSON,
	); err != nil {
		return ActionRecord{}, fmt.Errorf("store: scan action: %w", err)
	}
	var err error
	record.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return ActionRecord{}, fmt.Errorf("store: parse action start time: %w", err)
	}
	if finished.Valid {
		if !execution.Valid || !verification.Valid || !observed.Valid {
			return ActionRecord{}, errors.New("store: inconsistent action finish fields")
		}
		finishedAt, err := time.Parse(time.RFC3339Nano, finished.String)
		if err != nil {
			return ActionRecord{}, fmt.Errorf("store: parse action finish time: %w", err)
		}
		record.Finish = &ActionFinish{
			FinishedAt: finishedAt, Execution: ExecutionOutcome(execution.String), Verification: VerificationOutcome(verification.String),
			ObservedVersion: observed.String, SafeError: safeError,
		}
		if err := validateFinish(*record.Finish); err != nil {
			return ActionRecord{}, fmt.Errorf("store: invalid stored action finish: %w", err)
		}
		if finishedAt.Before(record.StartedAt) {
			return ActionRecord{}, errors.New("store: action finish precedes start")
		}
	} else if execution.Valid || verification.Valid || observed.Valid || safeError != "" {
		return ActionRecord{}, errors.New("store: unfinished action contains finish fields")
	}
	if err := decodeActionJSON(targetsJSON, metadataJSON, &record.ActionStart); err != nil {
		return ActionRecord{}, err
	}
	if _, _, err := validateActionContext(record.ActionStart, policy, false); err != nil {
		return ActionRecord{}, fmt.Errorf("store: invalid stored action context: %w", err)
	}
	return record, nil
}

// Action returns one full action and its planned steps and checkpoints.
func (s *HistoryStore) Action(ctx context.Context, id string) (ActionRecord, error) {
	if err := ctx.Err(); err != nil {
		return ActionRecord{}, fmt.Errorf("store: action detail canceled: %w", err)
	}
	if err := validateText(id, "action id", maxIdentifierBytes, true); err != nil {
		return ActionRecord{}, fmt.Errorf("store: invalid action id: %w", err)
	}
	record, err := scanHistoryAction(s.db.QueryRowContext(ctx, `SELECT `+historyActionColumns+` FROM actions WHERE id=?`, id), s.policy)
	if err != nil {
		return ActionRecord{}, fmt.Errorf("store: read action detail: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT step_index,label,description,outcome,exit_code,safe_error,safe_output,output_truncated,started_at,finished_at FROM steps WHERE action_id=? ORDER BY step_index LIMIT 33`, id)
	if err != nil {
		return ActionRecord{}, fmt.Errorf("store: read action steps: %w", err)
	}
	defer rows.Close() //nolint:errcheck // completed read-only query
	var retainedOutput int
	for rows.Next() {
		var step StepResult
		var label, description string
		var outcome, started, finished sql.NullString
		var exitCode sql.NullInt64
		var outputTruncated bool
		if err := rows.Scan(&step.Index, &label, &description, &outcome, &exitCode, &step.SafeError, &step.SafeOutput, &outputTruncated, &started, &finished); err != nil {
			return ActionRecord{}, fmt.Errorf("store: scan action step: %w", err)
		}
		if step.Index != len(record.Plan) {
			return ActionRecord{}, errors.New("store: stored action step order is not contiguous")
		}
		record.Plan = append(record.Plan, PlannedStep{Index: step.Index, Label: label, Description: description})
		if !outcome.Valid {
			if exitCode.Valid || started.Valid || finished.Valid || step.SafeError != "" || step.SafeOutput != "" || outputTruncated {
				return ActionRecord{}, errors.New("store: uncheckpointed step contains result fields")
			}
			continue
		}
		step.Outcome = StepOutcome(outcome.String)
		if exitCode.Valid {
			value := int(exitCode.Int64)
			step.ExitCode = &value
		}
		if started.Valid {
			step.StartedAt, err = time.Parse(time.RFC3339Nano, started.String)
			if err != nil {
				return ActionRecord{}, fmt.Errorf("store: parse step start time: %w", err)
			}
		}
		if finished.Valid {
			step.FinishedAt, err = time.Parse(time.RFC3339Nano, finished.String)
			if err != nil {
				return ActionRecord{}, fmt.Errorf("store: parse step finish time: %w", err)
			}
		}
		step.OutputTruncated = outputTruncated
		if len(step.SafeOutput) > s.policy.MaxStepOutput {
			return ActionRecord{}, errors.New("store: stored step output exceeds its policy limit")
		}
		retainedOutput += len(step.SafeOutput)
		if retainedOutput > s.policy.MaxActionOutput {
			return ActionRecord{}, errors.New("store: stored action output exceeds its policy limit")
		}
		if err := validateStepResult(step); err != nil {
			return ActionRecord{}, fmt.Errorf("store: invalid stored step result: %w", err)
		}
		record.Steps = append(record.Steps, step)
	}
	if err := rows.Err(); err != nil {
		return ActionRecord{}, fmt.Errorf("store: iterate action steps: %w", err)
	}
	if _, _, err := validateActionContext(record.ActionStart, s.policy, true); err != nil {
		return ActionRecord{}, fmt.Errorf("store: invalid stored action plan: %w", err)
	}
	return record, nil
}

func decodeActionJSON(targetsJSON, metadataJSON string, start *ActionStart) error {
	if err := json.Unmarshal([]byte(targetsJSON), &start.TargetIDs); err != nil {
		return fmt.Errorf("store: decode action targets: %w", err)
	}
	if err := json.Unmarshal([]byte(metadataJSON), &start.Metadata); err != nil {
		return fmt.Errorf("store: decode action metadata: %w", err)
	}
	if start.Metadata == nil {
		start.Metadata = map[string]string{}
	}
	return nil
}

func encodeHistoryCursor(cursor historyCursor) (string, error) {
	if err := validateText(cursor.id, "cursor action id", maxIdentifierBytes, true); err != nil {
		return "", fmt.Errorf("store: invalid cursor: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, cursor.startedAt)
	if err != nil || tsHistory(parsed) != cursor.startedAt {
		return "", errors.New("store: invalid cursor time")
	}
	payload := cursor.startedAt + "\x00" + cursor.id
	checksum := sha256.Sum256([]byte(payload))
	payload += string(checksum[:8])
	return base64.RawURLEncoding.EncodeToString([]byte(payload)), nil
}

func decodeHistoryCursor(value string) (historyCursor, error) {
	if len(value) == 0 || len(value) > 1024 {
		return historyCursor{}, errors.New("store: history cursor has an invalid length")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != value || len(decoded) < 9 {
		return historyCursor{}, errors.New("store: malformed history cursor")
	}
	payload, checksum := decoded[:len(decoded)-8], decoded[len(decoded)-8:]
	want := sha256.Sum256(payload)
	if !bytes.Equal(checksum, want[:8]) {
		return historyCursor{}, errors.New("store: invalid history cursor checksum")
	}
	separator := bytes.IndexByte(payload, 0)
	if separator <= 0 || separator == len(payload)-1 {
		return historyCursor{}, errors.New("store: malformed history cursor")
	}
	cursor := historyCursor{startedAt: string(payload[:separator]), id: string(payload[separator+1:])}
	parsed, err := time.Parse(time.RFC3339Nano, cursor.startedAt)
	if err != nil || tsHistory(parsed) != cursor.startedAt {
		return historyCursor{}, errors.New("store: malformed history cursor time")
	}
	if err := validateText(cursor.id, "cursor action id", maxIdentifierBytes, true); err != nil {
		return historyCursor{}, errors.New("store: malformed history cursor ID")
	}
	return cursor, nil
}

func validExecution(value ExecutionOutcome) bool {
	switch value {
	case ExecutionCompleted, ExecutionFailed, ExecutionCanceled, ExecutionBlocked, ExecutionInterrupted:
		return true
	default:
		return false
	}
}

func validVerification(value VerificationOutcome) bool {
	switch value {
	case VerificationPassed, VerificationFailed, VerificationUnknown, VerificationNotPerformed:
		return true
	default:
		return false
	}
}
