# History and Safe Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for native execution or superpowers:subagent-driven-development only after operator authorization. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record maintenance attempts independently of audits and execute only fresh, explicitly confirmed plans.

**Architecture:** Add an action-only store beside legacy storage, then an application service with narrow test seams. Use kernel-backed ownership and durable start records before commands. Migration is an explicit operation, not audit startup behavior.

**Tech Stack:** Existing `database/sql` and `modernc.org/sqlite`, standard contexts and subprocesses, existing `golang.org/x/sys` where required, temporary databases, and helper-process tests.

**Spec:** [Approved architecture](../specs/2026-10-07-workstation-doctor-architecture-redesign-design.md), sections 10 and 13-15.

## Global Constraints

- Requires Plans 1-3 and [roadmap contracts and limits](2026-10-07-workstation-doctor-redesign-roadmap.md).
- Persist maintenance attempts only; audit, refresh, preview, declined confirmation, and manual guidance create no records.
- No live updates, default database use, or real legacy migration in verification.
- Start recording failure blocks execution. Final recording failure is separate from execution and stops subsequent maintenance.
- No shell parsing, implicit privilege escalation, automatically installed managers, rollback promises, or long database transactions.
- History age/count: 90 days/10,000 terminal actions. Output: 16 KiB per step/64 KiB per action. Metadata: 8 KiB per action.
- Every task runs `make vet test lint vuln` before committing exact task files.

## Review Focus

- A crash can leave child work active after the parent releases its lock: Task 4's uncertain-owner blocking test.
- Canceling execution must not cancel outcome recording: Task 5's independent-finalization test.
- SQLite pragmas can be connection-local: Task 1's pooled-connection and foreign-key tests.
- A legacy database with WAL must not be preserved by an unsafe file-only copy: Task 3's snapshot/migration tests.
- Execution success is not verification success: Task 5's offline/wrong-version postcondition tests.

## File structure and contracts

Create under `internal/store`: `history_model.go`, `history.go`, `history_queries.go`, `retention.go`, `migration.go`, and matching tests.
Keep `store.go` and existing legacy tests until Plan 5 deletes their callers. New store code must never create `check_runs` or `check_results`.

Create under `internal/app`: `service.go`, `preview.go`, `executor.go`, `ownership.go`, `ownership_unix.go`, `paths.go`,
`migration.go`, and corresponding tests. Do not create a second broad application framework.

### Store types produced by Task 1

`ActionStart` holds `ID`, `BatchID`, `IntegrationID`, `CheckID`, `InstanceID`, `Label`, `Kind`, `Reason`, `AppVersion`,
`InstalledVersion`, `TargetVersion`, `Manager`, `Root`, `Scope`, and `OwnerToken` strings, `StartedAt time.Time`,
`Plan []PlannedStep`, `TargetIDs []string`, `MetadataVersion int`, and `Metadata map[string]string` (safe, allowlisted values).
Metadata version 1 allows only installed/intended/observed evidence source, precondition summaries, and optional installation-time source/meaning/precision.
Validate all metadata together within 8 KiB. Scope/target identity is a bounded fixed field, not optional metadata.
`PlannedStep` holds `Index int`, `Label string`, and `Description string`; it never holds runnable argv or credentials.

`StepResult` holds `Index int`, `Outcome StepOutcome`, `ExitCode *int`, `SafeError`, `SafeOutput` strings,
`StartedAt`, `FinishedAt` times, and `OutputTruncated bool`.
`ActionFinish` holds `FinishedAt time.Time`, `Execution ExecutionOutcome`, `Verification VerificationOutcome`,
`ObservedVersion`, and `SafeError` strings.
`ActionRecord` embeds `ActionStart` and adds `Finish *ActionFinish` and `Steps []StepResult`.
A nil Finish means unfinished, not successful.

Define execution values `Completed`, `Failed`, `Canceled`, `Blocked`, `Interrupted`, step values `Completed`, `Failed`, `Canceled`,
`NotStarted`, and verification values `Passed`, `Failed`, `Unknown`, `NotPerformed`. Use distinct prefixed constants and zero-as-unspecified.

### Store API

```go
func InspectHistory(ctx context.Context, path string) (DBKind, error)
func OpenHistory(ctx context.Context, path string, policy Policy) (*HistoryStore, error)
func DefaultPolicy() Policy
func (s *HistoryStore) StartAction(ctx context.Context, action ActionStart) error
func (s *HistoryStore) SaveStep(ctx context.Context, actionID string, step StepResult) error
func (s *HistoryStore) FinishAction(ctx context.Context, actionID string, finish ActionFinish) error
func (s *HistoryStore) ListActions(ctx context.Context, query ActionQuery) (ActionPage, error)
func (s *HistoryStore) Action(ctx context.Context, id string) (ActionRecord, error)
func (s *HistoryStore) Prune(ctx context.Context, now time.Time) (int64, error)
func (s *HistoryStore) Close() error
```

`DBKind` distinguishes missing, action-only, legacy, and unsupported schemas. Inspection does not create the file.
`Policy` contains `MaxAge time.Duration`, `MaxTerminal int`, `MaxStepOutput`, `MaxActionOutput`, `MaxMetadata int`.
`ActionQuery` contains integration/instance/execution/verification/batch filters, optional from/to times, opaque cursor, and limit.
`ActionPage` contains `Items []ActionRecord` with step bodies omitted from lists and `NextCursor string`.

### Task 1: Add standalone action storage and durability tests

**Files:** Create `history_model.go`, `history.go`, `history_test.go`; use driver registration explicitly in root and tests.

**Interfaces:** Produce the types above (including Policy, DBKind, ActionQuery, ActionPage), `DefaultPolicy`,
`InspectHistory`, `OpenHistory`, `StartAction`, `SaveStep`, `FinishAction`, `Close`.
StartAction saves the action and safe planned steps atomically. SaveStep checkpoints one result without duplicating its index.

- [ ] Add `TestHistoryIndependentOfAudit`, `TestHistoryStateTransitions`, `TestHistoryBounds`, and `TestHistoryConnectionPragmas`.
Test no run FK, duplicate IDs/indexes, foreign keys on every connection, canceled writes, private permissions, and oversized critical fields.

```go
if err := history.StartAction(ctx, startWithoutRunID); err != nil { t.Fatal(err) }
// Query sqlite_master and assert check_runs/check_results do not exist in this new database.
// Reopen and assert the unfinished action and planned steps remain durable.
```

- [ ] Run `make test TEST_PKG=./internal/store TEST_RUN='TestHistory'`; expect FAIL.
- [ ] Define and human-review the versioned actions/steps schema against these queries before writing migration SQL.
Use parameterized statements, context-aware operations, UTC timestamps, a bounded pool, connection-safe pragmas, and short transactions.
Existing legacy or unknown schema returns a typed migration/unsupported error; no implicit overwrite or audit-table creation.
- [ ] Implement state validation, private directories/files, and roadmap fixed-field limits. Reject altered IDs/critical scope instead of truncating them.
Output retention uses only approved safe summaries/excerpts and preserves explicit truncation flags.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: store maintenance attempts independently of audits`.

### Task 2: Add filtered history, retention, and physical-size evidence

**Files:** Create `history_queries.go`, `retention.go` and matching tests; extend `history_model.go`.

**Interfaces:** Consume DefaultPolicy, ActionQuery, and ActionPage from Task 1. Produce `ListActions`, `Action`, and `Prune`.
Order by `(StartedAt, ID)` descending and encode both in a validated opaque cursor. Reject malformed cursors/filters.

- [ ] Add `TestHistoryPagination`, `TestHistoryFilters`, `TestHistoryRetention`, and `TestHistoryOutputBudget`.
Cover tied timestamps, additions between pages, 50 default/200 maximum rows, canceled reads, age boundary, newest 10,000 terminal rows, running rows, and step deletion.

```go
policy := DefaultPolicy()
if policy.MaxAge != 90*24*time.Hour || policy.MaxTerminal != 10000 { t.Fatal("policy drift") }
if policy.MaxStepOutput != 16<<10 || policy.MaxActionOutput != 64<<10 || policy.MaxMetadata != 8<<10 {
    t.Fatal("history byte caps drifted")
}
```

- [ ] Run `make test TEST_PKG=./internal/store TEST_RUN='TestHistoryPagination|TestHistoryFilters|TestHistoryRetention|TestHistoryOutputBudget'`; expect FAIL.
- [ ] Implement indexed bounded queries and action details. Apply age first and count second to terminal rows only, atomically deleting associated steps.
Do not prune on reads or audits, and do not run VACUUM implicitly. Validate documented positive retention overrides.
- [ ] Test synthetic batches at maximum step count and output caps, asserting retained payload bounds and measuring actual file size without promising a hard file cap.
Safe error/metadata fields use fixed limits; oversized executable plans remain rejected, not shortened.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: query and retain bounded action history`.

### Task 3: Add platform paths and explicit non-destructive migration

**Files:** Create `internal/app/paths.go`, `paths_test.go`;
create `internal/store/migration.go`, `migration_test.go`; use synthetic old schemas in temporary databases.

**Interfaces:** Produce `ResolveHistoryPaths(osName, home string, env map[string]string, override string) (HistoryPaths, error)`.
`HistoryPaths` holds `Database`, `StateDir`, and `LegacyDatabase` strings. Defaults use `workstation-doctor/doctor.db` under
macOS `~/Library/Application Support` or Linux absolute XDG_STATE_HOME/fallback `~/.local/state`.
The legacy candidate is `~/.local/share/workstation-doctor/doctor.db`; --db does not move maintenance ownership to a different user lock.
Produce store `PreviewLegacyMigration(ctx context.Context, source, destination string) (MigrationPreview, error)` and
`ImportLegacyActions(ctx context.Context, preview MigrationPreview) (MigrationResult, error)`.
Preview contains paths, schema, safe row counts, and source fingerprint. Result contains imported count and preserved source path.

- [ ] Add `TestHistoryPaths`, `TestMigrationRequiresExplicitApproval`, `TestLegacyImportPreservesSource`, and `TestLegacyImportFailureRecovery`.
Test invalid relative XDG paths, explicit DB override, WAL-active source, destination collision, modified source after preview, and repeat invocation.

```go
// Save a hash and logical rows from a quiescent synthetic legacy database.
result, err := ImportLegacyActions(ctx, approvedPreview)
if err != nil { t.Fatal(err) }
if result.Imported != 2 { t.Fatalf("imported %d, want two actual actions", result.Imported) }
// Assert source remains readable and unchanged, destination has no audit snapshots, verification is Unknown.
```

- [ ] Run `make test TEST_PKG='./internal/store ./internal/app' TEST_RUN='TestHistoryPaths|TestMigration|TestLegacyImport'`; expect FAIL.
- [ ] Verify modernc/SQLite's current read-only snapshot and backup capabilities through dependency documentation.
Require legacy writers to be quiescent or use a verified consistent read-only snapshot. Never preserve a live WAL database by copying only its main file.
- [ ] Implement read-only preview and an explicitly confirmed import into a separate new-schema destination.
Preserve the old database; reject changed fingerprints and existing incompatible destinations. Commit import plus its source marker atomically for restart/idempotence.
Import only real action rows. Preserve known safe fields and mark unavailable identity/context; discard untrusted legacy output/commands unless an allowlisted safe summary can be established.
- [ ] Assert failed imports do not modify source or publish partial destination data. Audits never call migration APIs.
Run the global gate. Commit: `feat: migrate legacy actions without modifying source history`.

### Task 4: Add exclusive ownership and immutable action preparation

**Files:** Create `internal/app/ownership.go`, `ownership_unix.go`, `ownership_test.go`, `preview.go`, `preview_test.go`, `service.go`, `service_test.go`.

**Interfaces:** Produce roadmap `NewService`, `Audit`, `Current`, `Prepare`, and `Confirm`.
Produce `Ownership` with `Token() string` and `Release() error`, and `AcquireOwnership(ctx context.Context, stateDir string) (Ownership, error)`.
Use a private lock location under stable user application state, independent of database overrides.
Define HistoryWriter with StartAction/SaveStep/FinishAction, HistoryReader with ListActions/Action, and HistoryMaintenance with Prune/Close,
using the exact store signatures above. History embeds these three consumer-owned interfaces.
Options supplies `OpenHistory func(context.Context, string, store.Policy) (History, error)`,
`RunStep func(context.Context, doctor.CommandStep) (store.StepResult, error)`, and
`Acquire func(context.Context, string) (Ownership, error)` seams plus the roadmap fields.
The production opener wraps store.OpenHistory's concrete return as History; no covariance assumption or service locator.

- [ ] Add `TestServiceAuditNoHistory`, `TestPrepareFrozenScope`, `TestConfirmationBinding`, `TestMaintenanceOwnership`, and `TestUncertainOwnerBlocksMaintenance`, and `TestCurrentSnapshotIsolation`.
Use same-user helper processes with different database paths. Test active owner, reused PID uncertainty, orphan helper, refused privilege escalation, stale fingerprint, reused approval, and overlapping targets.

```go
_ = service.Audit(ctx)
_ = service.Current()
if openHistoryCalls != 0 { t.Fatal("read-only service opened storage") }
```

- [ ] Run `make test TEST_PKG=./internal/app TEST_RUN='TestServiceAudit|TestPrepare|TestConfirmation|TestMaintenanceOwnership|TestUncertainOwner'`; expect FAIL.
- [ ] Implement a kernel advisory lock supported on macOS/Linux, not a PID-only lock file. Capture execution ownership evidence privately.
Persist only private action-ownership evidence under StateDir before executing, not workstation inventory or PID-only liveness claims.
Acquiring an available lock does not prove orphan updater work stopped. If previous ownership cannot be established inactive, block mutations while retaining read-only views.
This rule also applies when another application instance uses a different database override.
- [ ] Prepare by fresh `Inspect`, validate proposal limits, deduplicate affected target IDs, and freeze a fingerprint including scope, executable, version, manager, pins, and plan.
Copy report slices/maps when publishing Current and freeze proposal slices/maps when preparing; caller mutation cannot change execution data.
Hide runnable data from the UI. Confirm issues a one-use fingerprint-bound approval; decline creates none. Token creation is not itself evidence of human consent.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: prepare confirmed actions with exclusive ownership`.

### Task 5: Implement durable execution, verification, and history use cases

**Files:** Create `internal/app/executor.go`, `executor_test.go`, `migration.go`, `migration_test.go`; complete `service.go`, `service_test.go` and history adapter.

**Interfaces:** Produce roadmap `Apply`, `History`, and `HistoryDetail`, plus `PreparedAction.Preview() Preview`.
`ActionReport` contains `RecordID string`, execution/verification enum values, `SafeError string`, and `HistoryError error`.
Produce `RunCommandStep(ctx context.Context, step doctor.CommandStep) (store.StepResult, error)` using direct argv and controlled process groups.
Unknown updater interaction or uncontrollable lifecycle keeps the proposal manual-only.
Produce service `PreviewMigration(ctx context.Context, source, destination string) (store.MigrationPreview, error)` and
`MigrateLegacy(ctx context.Context, preview store.MigrationPreview, confirmed bool) (store.MigrationResult, error)`.
A false confirmation returns without writes; true still requires unchanged fingerprint/destination and exclusive migration ownership.

- [ ] Add `TestApplyStartFailure`, `TestApplyStalePreconditions`, `TestApplyStepCheckpointFailure`, `TestApplyVerificationOutcomes`,
`TestApplyFinalizationAfterCancel`, `TestApplyNoShellOrReplay`, and `TestMigrationWrapperConsent`. Synthetic commands only create/read files inside temporary roots.
Cover cancellation during execution, verification, and final recording. Tests of legacy SQL fixtures belong to internal/store; app tests do not issue SQL.

```go
report, err := service.Apply(ctx, prepared, approval)
if !errors.Is(err, injectedStartFailure) || commandCalls != 0 {
    t.Fatal("command ran before durable start")
}
// Separate case: a successful command and failed final recording retain Completed execution plus HistoryError.
```

- [ ] Run `make test TEST_PKG=./internal/app TEST_RUN='TestApply'`; expect FAIL.
- [ ] Implement the spec lifecycle: confirmation, ownership, start, fresh preconditions, ordered steps, postcondition, finish, release.
Write a blocked attempt on changed preconditions. Stop later commands on any checkpoint/history failure.
Use 10-minute per-step execution and a separate 5-second finalization context after cancellation. Attempt final safe status recording even if one checkpoint fails.
- [ ] Kill/wait supported process groups on cancellation, never report rollback, and mark remaining steps NotStarted.
Reject shell interpreters and oversized plans. Verify by registered fresh inspection rather than a product switch.
A command can be Completed with verification Unknown or Failed; never replace that with a success banner.
- [ ] Implement bounded history queries and explicit migration service wrappers. Reconcile unfinished records only with evidence of inactive ownership.
Release ownership on every exit; surface release errors. Failed final recording disables later maintenance until a deliberate recovery check, without replaying the command.
- [ ] Run helper-process tests and the global gate; expect PASS. Commit: `feat: execute and record confirmed maintenance safely`.

## Stage exit

Service and store contracts are tested independently of TUI and real packages. Legacy paths remain blocked.
No real migration or maintenance ran. Review crash/ownership, history failure, and stale-approval evidence before enabling Plan 5.
