# TUI Cutover and Details Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for native execution or superpowers:subagent-driven-development only after operator authorization. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the approved redesign through one TUI with generic current details and readable maintenance history.

**Architecture:** Root main constructs the completed engine and service. Split TUI state, rendering, and detail mapping in the root package. Remove legacy CLI workflows and persistence after their callers are replaced, without a visual-system rewrite.

**Tech Stack:** Existing Go-TUI v0.22.1, urfave/cli/v3 for launch flags only, zerolog diagnostics, isatty, application service, and action-only SQLite history.

**Spec:** [Approved architecture](../specs/2026-10-07-workstation-doctor-architecture-redesign-design.md), especially sections 8, 10, 14-15, and 18.

## Global Constraints

- Requires completed Plans 1-4 and [roadmap contracts](2026-10-07-workstation-doctor-redesign-roadmap.md).
- One TUI, root `main.go`, no numbered fallback or check/manual/fix/history subcommands.
- TUI collects input and renders generic data. It never executes SQL, subprocesses, or product-specific inspection.
- Audits and details work without SQLite. Maintenance becomes available only through Plan 4 service confirmation and ownership.
- Installation dates are sourced or display `Installation time unavailable`.
- History records past action context, not current inventory or an audit snapshot.
- Every task runs `make vet test lint vuln` before its local commit; platform claims require native runtime evidence.

## Review Focus

- A late async audit result must not overwrite a newer selection or stopped app: Task 1's generation/cancellation test.
- Missing install-time evidence must not display a zero date: Task 2's unavailable-field test.
- Release notes can contain control characters, redirects, and oversized bodies: Task 2's note retrieval test.
- Declining a preview or opening history must not execute a command: Task 3's consent test.
- A missing historical target must not break history detail: Task 4's orphan-record view test.

## File structure

Modify `main.go`, `main_test.go`, `menu.go`, and `menu_test.go`.
Create root `menu_state.go`, `menu_views.go`, `menu_details.go`, and matching test files. Keep root TUI code rather than adding a package for its own sake.
Add `internal/app/release_notes.go` and matching tests only for bounded optional note retrieval.
Update `README.md`, `AGENTS.md`, and `Makefile` when actual cutover occurs. Remove obsolete legacy files only in Task 5.

### Task 1: Connect TUI lifecycle to the application service

**Files:** Modify `menu.go`, `menu_test.go`; create `menu_state.go`, `menu_state_test.go`, `menu_views.go`;
modify root composition in `main.go` without enabling another maintenance path.

**Interfaces:** Change `newDoctorApp(ctx context.Context, service *app.Service) *doctorApp` and produce
`runTUI(ctx context.Context, service *app.Service) error`.
Produce `func (a *doctorApp) startAudit()` and `func (a *doctorApp) stop()` with `currentGeneration uint64` owned by the UI event loop.
The component retains Go-TUI state and QueueUpdate usage. It holds current typed reports and selection keys, not store rows/run IDs.

- [ ] Verify the pinned Go-TUI documentation before using new APIs. Existing `State`, `QueueUpdate`, and timer behavior are visible in current `menu.go`; do not assume a newer framework signature.
- [ ] Add `TestMenuAuditUsesService`, `TestMenuGenerationGuardsLateResults`, and `TestMenuQuitCancelsWork`.
Use a service with synthetic definitions, fake history opener, and channel-controlled completion, never installed tools.

```go
controller.startAudit()
// Complete a newer audit first, then the older request.
if controller.currentGeneration != expectedGeneration || historyOpenCalls != 0 {
    t.Fatal("stale audit or read-only history regression")
}
```

- [ ] Run `make test TEST_PKG=. TEST_RUN='TestMenuAudit|TestMenuGeneration|TestMenuQuit'`; expect FAIL.
- [ ] Replace direct `runAndRecord`, `lastRunSummary`, and store calls with service operations. Generation IDs prevent late callbacks from changing current state.
Create per-operation cancellation contexts and cancel on quit. During maintenance, wait for bounded cancellation/finalization before destroying app resources.
- [ ] Split existing rendering and state by responsibility, keeping its Go-TUI appearance. Keep maintenance unavailable until Task 3 connects the confirmed service.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `refactor: route TUI lifecycle through application service`.

### Task 2: Add generic integration, instance, and finding details

**Files:** Create `menu_details.go`, `menu_details_test.go`; extend `menu_views.go` and tests;
create `internal/app/release_notes.go`, `release_notes_test.go`.

**Interfaces:** Produce `Detail` (Title string, Sections []DetailSection), `DetailSection` (Heading string, Rows []DetailRow),
and `DetailRow` (Label, Value, Source, ObservedAt string).
Produce `integrationDetail(report doctor.AuditReport, id string) (Detail, error)`,
`instanceDetail(report doctor.AuditReport, id string) (Detail, error)`,
`findingDetail(report doctor.AuditReport, key doctor.FindingKey) (Detail, error)`, and `detailText(detail Detail) string`.
Produce service `ReleaseNotes(ctx context.Context, key doctor.FindingKey) (ReleaseNotes, error)`.
`ReleaseNotes` holds `URL`, `Text` strings, `ObservedAt time.Time`, and `Truncated bool`.

- [ ] Add `TestIntegrationDetailAbsent`, `TestInstanceDetailEvidence`, `TestFindingUpdateDetail`, `TestGenericNewCheckDetail`, and `TestReleaseNotesBounds`.
Cover inactive installs, unresolved provenance, manager versus upstream candidates, stale facts, unavailable dates, and safe note failure.

```go
text := detailText(instanceDetailFixtureWithoutTimestamp)
if !strings.Contains(text, "Installation time unavailable") || strings.Contains(text, "0001-") {
    t.Fatal("missing installation evidence became a fabricated date")
}
```

- [ ] Run `make test TEST_RUN='TestIntegrationDetail|TestInstanceDetail|TestFindingUpdateDetail|TestGenericNewCheckDetail|TestReleaseNotesBounds'`; expect FAIL.
- [ ] Map generic descriptors/facts to rows with source and observed time. Show executable versus resolved path, installation root, ownership, scope, active status, configuration evidence, and revision-specific install meaning.
Do not map action-start time, filesystem time, or first observation to original installation time.
- [ ] Retrieve optional notes through bounded host HTTP from established official URLs. Validate scheme and redirects against the provider's official source,
use the finding's PublicReference of kind `release-notes` without an integration-name switch,
apply existing body/deadline caps, sanitize terminal controls, and keep known update findings when notes fail. No browser/process launch is required.
- [ ] Connect Enter/detail and Back navigation to selected identity, with scroll clamping and empty-state handling. No integration-name switch or independent UI tab per product.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: display sourced integration and update details`.

### Task 3: Connect action preview, explicit consent, and progress

**Files:** Modify `menu_state.go`, `menu_views.go`, `menu.go`; extend their tests and `menu_details.go`.

**Interfaces:** Consume service `Prepare`, `Confirm`, `Apply`, and PreparedAction.Preview. Produce
`actionDetail(preview app.Preview) Detail` and typed pending/active action state in the TUI.
Produce `func (a *doctorApp) declinePreparedAction()`. No executable argv is edited or reconstructed from view text.

- [ ] Add `TestMenuActionPreview`, `TestMenuDeclineNoHistory`, `TestMenuStaleApproval`, and `TestMenuActionOutcomeLabels`.
Cover `Automatic`, `Manual`, and `Inspection` capabilities, disabled ownership, changed target after preview, partial steps, and history errors.

```go
controller.declinePreparedAction()
if historyStartCalls != 0 || commandCalls != 0 { t.Fatal("decline had maintenance effects") }
// Completed execution with Unknown verification must not render "verified repair".
```

- [ ] Run `make test TEST_PKG=. TEST_RUN='TestMenuAction|TestMenuDecline|TestMenuStaleApproval'`; expect FAIL.
- [ ] Display reason, intended version, selected instance, scope, steps, side effects, preconditions, and verification expectations.
Affirmative confirmation alone calls Confirm then Apply. All other keys/escape decline. Manual and inspection guidance stay separate and do not create maintenance records.
- [ ] Route progress and cancellation through service results. Show command outcome, verification, and history failure separately.
Never report rollback or silently retry. Display the migration guard until the completed service is bound, then remove its reachable TUI callers.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: run confirmed maintenance from TUI previews`.

### Task 4: Add paginated action history and explicit migration interaction

**Files:** Modify `menu_state.go`, `menu_views.go`, `menu_details.go`; extend tests;
consume migration wrappers from Plan 4 and extend interaction tests; do not introduce SQL into TUI/app tests.

**Interfaces:** Consume `History`, `HistoryDetail`, `PreviewMigration`, and `MigrateLegacy` from Plan 4.
Produce `historyDetail(record store.ActionRecord) Detail` and `func (a *doctorApp) showHistoryDetail(record store.ActionRecord) error`.
The migration UI's explicit yes passes confirmed=true; all other interactions pass false or do not invoke migration.

- [ ] Add `TestMenuHistoryPaging`, `TestHistoryDetailMissingInstance`, `TestMenuMigrationPreviewConsent`, and `TestHistoryAndCurrentSeparated`.
Use synthetic legacy and action databases under temporary roots. No automatic migration occurs on app startup or audit.

```go
_ = controller.showHistoryDetail(recordForRemovedInstance)
if currentAuditCalls != 0 || commandCalls != 0 { t.Fatal("historical detail triggered inspection or execution") }
```

- [ ] Run `make test TEST_RUN='TestMenuHistory|TestHistoryDetailMissingInstance|TestMenuMigration|TestHistoryAndCurrentSeparated'`; expect FAIL.
- [ ] Replace run-history columns with action time, target, execution, verification, and safe summary. Page/filter through store queries, not in-memory whole-history loading.
Historical details show captured context and ordered steps, even when the instance disappeared. Current-state links require a matching identity.
- [ ] On legacy detection, offer the explicit source/destination/count preview and approval interaction. Preserve old data and display import limitations.
History migration failures leave current audits/details usable and mutation disabled where history is unresolved.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: browse action history and explicit migration in TUI`.

### Task 5: Remove legacy workflows and prove the completed target

**Files:** Modify `main.go`, `main_test.go`, `menu.go`, `Makefile`, `README.md`, `AGENTS.md`, `go.mod`, `go.sum`;
remove obsolete `maintenance_guard.go`, `internal/doctor/legacy_model.go`, `legacy_engine.go`, `legacy_inspect.go`, `report.go`,
and `internal/store/store.go` only after no active caller remains. Update/remove legacy tests in `internal/doctor/doctor_test.go`,
`internal/store/store_test.go`, and `menu_test.go`, preserving new contract coverage.
Modify the retired declaration sections in doctor `pi.go`, `herdr.go`, `ghostty.go`, `starship.go`, `npm.go`, `serena.go`, `gortex.go`,
`brew.go`, `pi_packages.go`, `superpowers.go`, `herdr_plugins.go`, and `skills.go`.
Replace obsolete tests rather than preserving rejected heuristic assertions.

**Interfaces:** Produce LaunchOptions with `DBPath`, `ProjectDir` strings, `Locations map[string][]string`, and `SkillRoots []string`.
Launch flags are `--db PATH`, `--project PATH`, repeated `--location INTEGRATION=PATH`, and repeated `--skill-root PATH`.
Reject unknown integration IDs, malformed overrides, and relative application storage overrides; product overrides follow their own documented rules.
Produce `newRootCommand(start func(context.Context, LaunchOptions) error, interactive func() bool) *cli.Command`.
Retain urfave only for launch configuration, help, and version. Its `Commands` list has no check/manual/fix/history workflows.
Expose retention overrides as `--history-days`, `--history-limit`, `--history-step-bytes`, `--history-action-bytes`, and `--history-metadata-bytes`,
with approved defaults 90, 10000, 16384, 65536, and 8192 respectively. Add `HistoryPolicy store.Policy` to LaunchOptions.
Reject non-positive values and step caps greater than the action cap. No new configuration-file framework.
Add `make test-platform EXPECT_OS=darwin|linux`, which refuses host mismatch before running native race-enabled tests.

- [ ] Add `TestEntryNonTTY`, `TestEntryNoWorkflowSubcommands`, `TestStartupNoStorage`, and `TestTargetAcceptanceMatrix`.
Inject launch callbacks and synthetic service dependencies. Assert exact non-TTY error `An interactive terminal is required.` on stderr with no escape codes or numbered menu.
Help/version remain plain metadata operations and do not launch inspection or storage.
- [ ] Run `make test TEST_RUN='TestEntry|TestStartupNoStorage|TestTargetAcceptanceMatrix'`; expect FAIL.
- [ ] Compose completed builtins, read-only Host, lazy service, and one TUI at the root. Remove CLI workflows, duplicated execution,
run recording, classic menu, run-ID state, and obsolete formatting dependencies. Do not delete the preserved legacy database.
- [ ] Remove old checker implementations and tests once the new tests cover their intended questions. Keep a registry contract test proving all 21 IDs.
Update README and AGENTS code maps to actual implemented behavior, safe migration, launch flags, limits, and read-only detail policy.
- [ ] Add native helper-process tests for advisory locking, cancellation, private files, and non-TTY startup. Run `make test-platform EXPECT_OS=darwin`
on macOS and `make test-platform EXPECT_OS=linux` on Linux. If either environment is unavailable, record the missing gate and do not claim redesign completion.
- [ ] Run `make vet test lint vuln`; expect all packages PASS, `0 issues.`, and no vulnerabilities. Inspect the final diff and status for databases, sidecars, binaries, secrets,
legacy integration-name UI switches, and shell-string execution. Commit: `feat: complete TUI-only workstation doctor redesign`.

## Completion evidence

Record test names and native-platform outcomes against every spec acceptance criterion in the roadmap or a small test-evidence section.
Confirm audits/details never call storage; actions require consent, ownership, and durable start; history is bounded and action-only;
all 21 check areas work with synthetic heterogeneous installations; and unavailable installation times remain explicit.

Do not run a real update or migrate a user's database as a completion demonstration. Final implementation review precedes any integration/release decision.
