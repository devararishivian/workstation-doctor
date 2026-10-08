package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestHistorySchemaUpgradeTimestampOrder(t *testing.T) {
	path := privateHistoryPath(t, "schema-v1.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacySQL := actionSchema
	for _, statement := range []string{
		"CREATE INDEX idx_actions_batch_started ON actions(batch_id, started_at DESC, id DESC);\n",
		"CREATE INDEX idx_actions_execution_started ON actions(execution, started_at DESC, id DESC);\n",
		"CREATE INDEX idx_actions_verification_started ON actions(verification, started_at DESC, id DESC);\n",
		"CREATE INDEX idx_actions_finished_started ON actions(finished_at, started_at DESC, id DESC);\n",
	} {
		legacySQL = strings.ReplaceAll(legacySQL, statement, "")
	}
	if _, err := db.Exec(legacySQL); err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA application_id=%d; PRAGMA user_version=1`, historyApplicationID)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO actions(id,batch_id,integration_id,check_id,instance_id,label,kind,reason,app_version,installed_version,target_version,manager,root,scope,owner_token,started_at,target_ids,metadata_version,metadata_json)
		VALUES('v1-action','','pi','check','instance','label','update','reason','','','','','','user','owner','2026-10-08T12:00:00Z','["target"]',0,'{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO steps(action_id,step_index,label,description,outcome,started_at,finished_at)
		VALUES('v1-action',0,'step','desc','Completed','2026-10-08T12:00:00.1Z','2026-10-08T12:00:00.2Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	h, err := OpenHistory(context.Background(), path, DefaultPolicy())
	if err != nil {
		t.Fatalf("upgrade v1 schema: %v", err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	var started, stepStarted, stepFinished string
	if err := h.db.QueryRow(`SELECT started_at FROM actions WHERE id='v1-action'`).Scan(&started); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRow(`SELECT started_at,finished_at FROM steps WHERE action_id='v1-action'`).Scan(&stepStarted, &stepFinished); err != nil {
		t.Fatal(err)
	}
	wantTime := "2026-10-08T12:00:00.000000000Z"
	if started != wantTime || stepStarted != "2026-10-08T12:00:00.100000000Z" || stepFinished != "2026-10-08T12:00:00.200000000Z" {
		t.Fatalf("normalized timestamps action=%q step=%q..%q", started, stepStarted, stepFinished)
	}
	var version int
	if err := h.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != historySchemaVersion {
		t.Fatalf("schema version = %d, %v", version, err)
	}
	var indexes int
	if err := h.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name IN ('idx_actions_batch_started','idx_actions_execution_started','idx_actions_verification_started','idx_actions_finished_started')`).Scan(&indexes); err != nil || indexes != 4 {
		t.Fatalf("filter/retention indexes = %d, %v", indexes, err)
	}
}

func TestHistoryCursorTimestampsAreLexicallyOrdered(t *testing.T) {
	ctx := context.Background()
	h, err := OpenHistory(ctx, privateHistoryPath(t, "cursor.db"), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close() //nolint:errcheck // close errors need no action in a test
	for id, timestamp := range map[string]string{
		"late":  "2026-10-08T12:00:00.900000000Z",
		"early": "2026-10-08T12:00:00.100000000Z",
	} {
		if _, err := h.db.Exec(`INSERT INTO actions(id,batch_id,integration_id,check_id,instance_id,label,kind,reason,app_version,installed_version,target_version,manager,root,scope,owner_token,started_at,target_ids,metadata_version,metadata_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, "", "pi", "check", "instance", "label", "kind", "reason", "", "", "", "", "", "scope", "owner", timestamp, "[\"target\"]", 0, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	page, err := h.ListActions(ctx, ActionQuery{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "late" {
		t.Fatalf("first subsecond page = %+v", page)
	}
	page, err = h.ListActions(ctx, ActionQuery{Limit: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "early" {
		t.Fatalf("second subsecond page = %+v", page)
	}
}
