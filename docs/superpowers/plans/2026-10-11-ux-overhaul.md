# Workstation Doctor UX Overhaul Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:
> executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the Superpowers and Herdr update mechanisms, eliminate the main menu, merge automated fix and manual steps
into a unified detail screen, add component selection for batch fixes, and rewrite all user-facing wording to be clear
and consistent.

**Architecture:** The TUI state machine shrinks from 11 modes to 9 (eliminating `menu` and `manual`, adding
`select_fixes`). Backend check modules change `pi install` to `pi update` and add a `herdr server reload-config` step.
All string literals visible to users are rewritten per the spec's vocabulary standard. No new packages or dependencies
are added.

**Tech Stack:** Go 1.22+, `github.com/grindlemire/go-tui`, SQLite (via existing `internal/store`)

**Spec:** `docs/superpowers/specs/2026-10-11-comprehensive-ux-review-design.md`

## Global Constraints

- All existing tests must pass (`make test`) after every task.
- No new external dependencies.
- No behavioral changes to the audit engine, discovery, or service layer beyond what each task explicitly describes.
- All user-facing text must use the vocabulary standard from spec Section D.1: "check" not "audit", "update" not
  "attention", "component" not "integration", "fix" not "maintenance", "manual command" not "manual steps".
- The `ActionMode` constants (`ActionAutomatic`, `ActionManual`) and `Outcome` constants are internal model values and
  must NOT be renamed — only their presentation text changes.
- The `preview_migration` and `migration_done` modes are out of scope and must not be altered.

## Review Focus

1. **GitHub API failure during Herdr plugin check returns `OutcomeUnknown` silently.** A user sees `OK` status when the
   check actually could not determine currentness. The spec requires a diagnostic fact on API failure. → Test added in
   Task 3.
2. **`pi update` for a git package that has never been installed (missing directory).** The `pi update` command may fail
   if the package was never installed. The proposal must fall back to `pi install` when the path does not exist. → Test
   added in Task 1.
3. **Batch fix with mixed success where `herdr server reload-config` fails after a successful plugin install.** The user
   must see the install succeeded but reload failed, with a manual fallback. → Test added in Task 3.
4. **`select_fixes` screen with zero fixable components.** The user presses `f` from results when all UPDATE items are
   manual-only. The app must show the result screen with "No automatic updates available", not an empty checkbox list. →
   Test added in Task 6.
5. **History records created by old code (`pi install` label) must still render correctly.** The wording changes must
   not break display of existing history entries. → Test added in Task 7.

---

### Task 1: Fix Pi Package Update Commands (git and npm)

**Files:**

- Modify: `internal/doctor/resource_git.go:249-270`
- Modify: `internal/doctor/resource_sources.go:289-310`
- Test: `internal/doctor/resource_sources_test.go`

**Interfaces:**

- Consumes: existing `findExecutable(h, "pi")`, `ActionProposal` struct
- Produces: `ActionProposal.Steps[0].Command.Args` now contains `["update", "git:<identity>"]` or
  `["update", "npm:<identity>"]` instead of `["install", ...]`; `ActionProposal.Label` uses `"Update <name> via Pi"`
  format; `ActionProposal.SideEffects` updated. Also: `ActionProposal.Steps[0].Command.Args` uses `["install", ...]`
  when the resource path does not exist on disk (new-install fallback).

- [ ] **Step 1: Write failing test for git package update command**

```go
// In resource_sources_test.go, inside TestCheckSuperpowersSourceWithGitTagAndRelease
// sub-test "outdated release proposes update":
// After existing assertion that f.Actions has 1 element, add:
func TestGitUpdateUsesUpdateNotInstall(t *testing.T) {
// ... (setup same as existing "outdated release proposes update" sub-test) ...
if f.Actions[0].Steps[0].Command.Args[0] != "update" {
t.Fatalf("expected 'update' command, got %q", f.Actions[0].Steps[0].Command.Args[0])
}
if !strings.Contains(f.Actions[0].Steps[0].Command.Args[1], "git:github.com/obra/superpowers") {
t.Fatalf("expected git identity in args, got %v", f.Actions[0].Steps[0].Command.Args)
}
}
```

- [ ] **Step 2: Write failing test for npm package update command**

```go
func TestNpmUpdateUsesUpdateNotInstall(t *testing.T) {
// ... (setup same as existing TestPiPackageAutomaticUpdateProposal) ...
if f.Actions[0].Steps[0].Command.Args[0] != "update" {
t.Fatalf("expected 'update' command, got %q", f.Actions[0].Steps[0].Command.Args[0])
}
}
```

- [ ] **Step 3: Write failing test for new-install fallback (path does not exist)**

```go
func TestGitUpdateFallsBackToInstallWhenPathMissing(t *testing.T) {
// Setup: same as outdated-release test but set r.Path to a directory that
// does NOT exist on disk.
// Assert: f.Actions[0].Steps[0].Command.Args[0] == "install"
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test -race -run 'TestGitUpdateUses|TestNpmUpdateUses|TestGitUpdateFalls' ./internal/doctor/`
Expected: FAIL — args still contain `"install"`

- [ ] **Step 5: Change git update args in `resource_git.go:261-264`**

In `inspectGitResource`, change the `ActionProposal.Steps` construction: check if `r.Path` exists with `os.Stat`; if it
exists use `"update"`, otherwise use `"install"`. Update `Label` to
`fmt.Sprintf("Update %s via Pi", filepath.Base(r.Identity))`. Update `SideEffects` to
`"Pulls the latest version from the remote repository and updates the local checkout."`.

- [ ] **Step 6: Change npm update args in `resource_sources.go:301-304`**

Same pattern: check if `r.Path` exists; use `"update"` if present, `"install"` if absent. Update `Label` and
`SideEffects` text analogously.

- [ ] **Step 7: Run tests to verify they pass**

Run:
`go test -race -run 'TestGitUpdateUses|TestNpmUpdateUses|TestGitUpdateFalls|TestPiPackage|TestCheckSuperpowers' ./internal/doctor/`
Expected: PASS

- [ ] **Step 8: Run full suite**

Run: `make test`
Expected: PASS (0 failures)

- [ ] **Step 9: Commit**

```bash
git add internal/doctor/resource_git.go internal/doctor/resource_sources.go internal/doctor/resource_sources_test.go
git commit -m "fix: use pi update instead of pi install for existing packages"
```

---

### Task 2: Fix Superpowers Update Wording and Side Effects

**Files:**

- Modify: `internal/doctor/resource_git.go:253-269`
- Modify: `internal/doctor/resource_sources.go:287-308`
- Test: `internal/doctor/resource_sources_test.go`

**Interfaces:**

- Consumes: changes from Task 1 (the `"update"` args are already in place)
- Produces: `ActionProposal.Reason` uses `fmt.Sprintf("Package %s is outdated (%s → %s)", ...)` for npm;
  `fmt.Sprintf("Git package %s is outdated (%s → %s)", ...)` for git. `ActionProposal.SideEffects` uses spec wording.

- [ ] **Step 1: Write failing test for updated Reason wording**

```go
// In TestCheckSuperpowersSourceWithGitTagAndRelease, "outdated release proposes update":
// Assert the Reason field contains the version transition arrow format.
if !strings.Contains(f.Actions[0].Reason, "→") && !strings.Contains(f.Actions[0].Reason, "->") {
t.Fatalf("expected version transition in Reason, got %q", f.Actions[0].Reason)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run 'TestCheckSuperpowersSourceWithGitTagAndRelease' ./internal/doctor/`
Expected: FAIL

- [ ] **Step 3: Update Reason and SideEffects text in `resource_git.go` and `resource_sources.go`**

In `resource_git.go`, the `ActionProposal` Reason field: change to use the spec's format. In `resource_sources.go`, same
treatment for npm proposals. Update all `SideEffects` slices to use spec wording from Section D.2.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/doctor/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/doctor/resource_git.go internal/doctor/resource_sources.go internal/doctor/resource_sources_test.go
git commit -m "fix: rewrite update proposal wording per spec vocabulary"
```

---

### Task 3: Add Herdr Reload Step and API Failure Diagnostic

**Files:**

- Modify: `internal/doctor/herdr_resources.go:354-390`
- Test: `internal/doctor/herdr_resources_test.go`

**Interfaces:**

- Consumes: existing `herdrPluginUpdateProposal` signature, `findExecutable(h, "herdr")`
- Produces: `ActionProposal.Steps` now has 2 entries: index 0 is `herdr plugin install ...`, index 1 is
  `herdr server reload-config`. `ActionProposal.SideEffects` appended with reload description. When `h.Fetch` returns
  error for a branch/compare API call, the finding's `Evidence` slice gains a diagnostic `Fact` with label
  `"GitHub API status"`.

- [ ] **Step 1: Write failing test for reload-config step**

```go
func TestHerdrPluginUpdateIncludesReload(t *testing.T) {
// Setup: same as an existing test that produces an ActionAutomatic proposal
// (e.g., adapt from TestHerdrPluginDirtyCheckoutBlocksUpdate's setup but with
// a clean checkout and an ahead upstream).
// Assert: len(f.Actions[0].Steps) == 2
//         f.Actions[0].Steps[1].Command.Args == []string{"server", "reload-config"}
}
```

- [ ] **Step 2: Write failing test for API failure diagnostic**

```go
func TestHerdrPluginCheckAddsAPIFailureDiagnostic(t *testing.T) {
// Setup: h.Fetch returns error for the branch URL.
// Assert: finding.Evidence contains a Fact with Label "GitHub API status"
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -race -run 'TestHerdrPluginUpdateIncludesReload|TestHerdrPluginCheckAddsAPI' ./internal/doctor/`
Expected: FAIL

- [ ] **Step 4: Add reload-config step in `herdrPluginUpdateProposal` at line 388**

After the existing `p.Steps = []CommandStep{{...}}` line, append a second step:

```go
p.Steps = append(p.Steps, CommandStep{
Label:   "Reload the Herdr server configuration",
Command: Command{Executable: executable, Args: []string{"server", "reload-config"}},
})
```

Append to `p.SideEffects`: `"Reloads the running Herdr server to apply plugin changes."`

- [ ] **Step 5: Add API failure diagnostic in `checkHerdrPluginSources`**

In the `h.Fetch` error paths (around lines 303-310 and 320-330), before returning `[]Finding{f}`, append a diagnostic
fact:

```go
f.Evidence = append(f.Evidence, Fact{
State: EvidenceKnown, Label: "GitHub API status",
Value: "Request failed or rate limited", Source: "HTTP response",
ObservedAt: hostNow(h),
})
```

- [ ] **Step 6: Run tests**

Run: `go test -race ./internal/doctor/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/doctor/herdr_resources.go internal/doctor/herdr_resources_test.go
git commit -m "fix: add reload-config step to herdr plugin update and API failure diagnostic"
```

---

### Task 4: Eliminate Main Menu and Launch into Audit

**Files:**

- Modify: `menu_state.go:71,82-89,309-355,419-455`
- Modify: `menu.go:40-55,309-355`
- Modify: `menu_views.go:49-60,62-148`
- Test: `menu_state_test.go`

**Interfaces:**

- Consumes: existing `newDoctorApp`, `startAudit()`, `runAction(key string)`
- Produces: `newDoctorApp` sets initial `mode` to `"checking"` (was `"menu"`). `startAudit()` is called from `BindApp`.
  `runAction` no longer handles `"manual"` key. The `menuItem` struct, `items` field, `selectNext()`, `selectPrev()`,
  `activateSelected()` methods are removed. The `renderMenu()` function is removed. The `"menu"` case in `KeyMap()` is
  removed. All `mode.Set("menu")` calls are changed to `mode.Set("results")` (except where going back to results is
  already the behavior). The `"manual"` case in `Render()` and `KeyMap()` is removed. `renderManual()` is removed.

- [ ] **Step 1: Write failing test for auto-start audit on launch**

```go
func TestAppStartsAuditOnBind(t *testing.T) {
// Create doctorApp with a mock service, bind it.
// Assert: mode.Get() == "checking" (not "menu")
// After the audit goroutine completes: mode.Get() == "results"
}
```

- [ ] **Step 2: Write failing test that results `Esc`/`b` goes to quit, not menu**

```go
func TestResultsEscQuitsNotMenu(t *testing.T) {
// Simulate Esc key on results mode.
// Assert: app.stop() was called or mode is not "menu"
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -race -run 'TestAppStartsAudit|TestResultsEscQuits' .`
Expected: FAIL

- [ ] **Step 4: Modify `newDoctorApp` to set initial mode to `"checking"` and remove `items`**

In `menu_state.go`: change `tui.NewState("menu")` to `tui.NewState("checking")`. Remove the `items: []menuItem{...}`
block. Remove the `menuItem` struct (lines 17-21). Remove `selectNext()`, `selectPrev()`, `activateSelected()` methods.
Remove the `items` field from `doctorApp`.

- [ ] **Step 5: Start audit from `BindApp`**

In `menu_state.go`, at the end of `BindApp()`, add `go d.startAudit()` (use a goroutine so binding is non-blocking).

- [ ] **Step 6: Remove `"menu"` case from `KeyMap()` in `menu.go`**

Delete the entire `case "menu":` block (lines 40-55). In the `"results"` case, change `mode.Set("menu")` to `d.stop()`
for Esc/b (the app quits from results). Remove `'2'` key binding from results (manual screen no longer exists). Change
`'3'` to `'f'` for `runAction("fix")`. Add `'r'` for `d.startAudit()` and `'h'` for `d.runAction("history")`.

- [ ] **Step 7: Remove `"manual"` case from `KeyMap()` and `Render()`**

Delete the `case "manual", "fix_done":` block from `KeyMap()`. Delete the `case "manual":` from `Render()`. Delete
`renderManual()` from `menu_views.go`.

- [ ] **Step 8: Update all `mode.Set("menu")` calls**

In `menu.go` and `menu_state.go`, change every remaining `mode.Set("menu")` to either `mode.Set("results")` or
`d.stop()`, depending on context:

- History/history_detail `Esc`/`b` → `mode.Set("results")` (back to results, not exit)
- `no_fixes` `Esc`/`b` → `mode.Set("results")`
- migration_done → `mode.Set("results")`

- [ ] **Step 9: Remove `renderMenu()` from `menu_views.go`**

Delete the `renderMenu()` function (lines 62-148). Update the `default` case in `Render()` to render `renderResults()`
or `renderRunning()`.

- [ ] **Step 10: Remove `"manual"` from `runAction()` switch**

In `menu_state.go:309-331`, remove the entire `case "manual":` block.

- [ ] **Step 11: Update existing tests**

Update tests that assert `mode.Get() == "menu"` to assert `"results"` or `"checking"`. Update `TestMenuAuditUsesService`
and others that start from menu mode. Remove `TestMenuManualStepsScrollAndWindowing` or rewrite it for the new
detail-inline behavior (Task 5 will handle the detail rewrite).

- [ ] **Step 12: Run full test suite**

Run: `make test`
Expected: PASS

- [ ] **Step 13: Commit**

```bash
git add menu_state.go menu.go menu_views.go menu_state_test.go
git commit -m "feat: eliminate main menu, launch directly into audit"
```

---

### Task 5: Merge Manual Command into Component Detail Screen

**Files:**

- Modify: `menu_details.go:190-310`
- Modify: `menu_views.go` (inside `renderDetail`)
- Test: `menu_details_test.go`

**Interfaces:**

- Consumes: `componentDetail(report, checkID) Detail` signature (unchanged), `doctor.Finding`, `doctor.ActionProposal`
- Produces: `componentDetail` returns `Detail` whose `Sections` include a new `"Update Available"` section (replacing
  both `"Items Requiring Attention"` and `"Proposed Actions"`) containing `DetailRow` entries with labels:
  `"Manual command"` (the full shell command string), `"Documentation"` (URL). The `renderDetail` function renders the
  `"Update Available"` section with the manual command as a copyable green-styled line, and adds `'f'` key hint in the
  footer when automatic fix is available.

- [ ] **Step 1: Write failing test for merged "Update Available" section**

```go
func TestComponentDetailMergesActionsIntoUpdateSection(t *testing.T) {
// Build a report with one finding that has OutcomeAttention and one ActionAutomatic.
// Call componentDetail(report, checkID).
// Assert: no section with Heading "Items Requiring Attention" exists.
// Assert: no section with Heading "Proposed Actions" exists.
// Assert: a section with Heading "Update Available" exists.
// Assert: section contains rows with Label containing "Manual command".
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run 'TestComponentDetailMergesActions' .`
Expected: FAIL

- [ ] **Step 3: Rewrite `componentDetail` to produce "Update Available" section**

In `menu_details.go:270-300`, replace the two separate section-building loops (for attention rows and action rows) with
a single loop that creates a `"Update Available"` section. For each finding with `OutcomeAttention`:

- Add a row with the instance name and version transition.
- For each action, add a row with label `"Fix: <label> [<mode>]"` and value showing the reason.
- Add a row with label `"Manual command"` and value containing the full command string reconstructed from
  `action.Steps[0].Command`.
- Add a row with label `"Documentation"` and the first reference URL.

- [ ] **Step 4: Update `renderDetail` to render "Update Available" with command styling**

In `menu_views.go`, inside the detail section rendering switch, add a `case "Update Available":` handler that renders
the command row with `catppuccinGreen` style and a `$` prefix.

- [ ] **Step 5: Update detail footer to show `f` key**

In `renderDetail`'s footer construction, replace the `hasAutoFix` check's text with:
`"[Esc/b: Back · f: Fix this component · ↑/↓: Scroll · q: Quit]"`.

- [ ] **Step 6: Change detail `'3'`/`'p'` bindings to `'f'` in `menu.go`**

In the `case "detail":` keymap, replace `'3'` and `'p'` with `'f'` calling `d.prepareFirstAction()`. Remove the `'2'`
binding (manual screen no longer exists).

- [ ] **Step 7: Run tests**

Run: `go test -race ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add menu_details.go menu_details_test.go menu_views.go menu.go
git commit -m "feat: merge manual command into component detail screen"
```

---

### Task 6: Add Select Fixes Screen

**Files:**

- Modify: `menu_state.go` (add `selectFixItems` state, `toggleFixItem()`, `selectAllFixes()`, `deselectAllFixes()`,
  `confirmSelectedFixes()` methods)
- Modify: `menu.go` (add `case "select_fixes":` keymap)
- Modify: `menu_views.go` (add `renderSelectFixes()`, update `Render` switch)
- Test: `menu_state_test.go`

**Interfaces:**

- Consumes: `prepareAutomaticFixes(filterComponent string)` from Task 4 (still exists)
- Produces: new type `fixCandidate struct { Component, Label, ActionID string; Key doctor.FindingKey; Selected bool }`.
  New field `selectFixItems *tui.State[[]fixCandidate]` on `doctorApp`. New method `enterSelectFixes()` that populates
  `selectFixItems` from current report findings. New method `toggleFixItem(index int)`. New method
  `confirmSelectedFixes()` that calls `prepareAutomaticFixes` with the selected components and then transitions to
  `"preview_action"`.

- [ ] **Step 1: Write failing test for select_fixes population**

```go
func TestSelectFixesPopulatesFromReport(t *testing.T) {
// Setup: doctorApp with a report containing 2 components with automatic actions.
// Call enterSelectFixes().
// Assert: selectFixItems has 2 entries, both with Selected == true.
// Assert: mode is "select_fixes".
}
```

- [ ] **Step 2: Write failing test for toggle behavior**

```go
func TestSelectFixesToggle(t *testing.T) {
// Setup: enter select_fixes with 2 items both selected.
// Call toggleFixItem(0).
// Assert: item 0 is deselected, item 1 remains selected.
}
```

- [ ] **Step 3: Write failing test for zero-eligible redirect**

```go
func TestSelectFixesWithNoAutoActionsShowsResult(t *testing.T) {
// Setup: report has UPDATE findings but only ActionManual.
// Call enterSelectFixes().
// Assert: mode is "no_fixes" (or the result equivalent), not "select_fixes".
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test -race -run 'TestSelectFixes' .`
Expected: FAIL

- [ ] **Step 5: Add `fixCandidate` type and `selectFixItems` state field to `menu_state.go`**

Add `type fixCandidate struct { ... }` after the existing `menuItem` deletion site. Add
`selectFixItems *tui.State[[]fixCandidate]` to `doctorApp`. Initialize it in `newDoctorApp`. Bind it in `BindApp`.

- [ ] **Step 6: Implement `enterSelectFixes()`, `toggleFixItem()`, `confirmSelectedFixes()`**

`enterSelectFixes()`: iterate report findings, collect each automatic action as a `fixCandidate` with `Selected: true`.
If none found, set mode to `"no_fixes"` and return. Otherwise set `selectFixItems` and mode to `"select_fixes"`.

`toggleFixItem(idx)`: flip `Selected` on the item at index.

`confirmSelectedFixes()`: collect selected component IDs, call `prepareAutomaticFixes` with them joined or individually,
then transition to `"preview_action"`. This requires modifying `prepareAutomaticFixes` to accept `[]string` instead of a
single `string` — change the parameter from `filterComponent string` to `filterComponents []string` and adjust the
filter logic.

- [ ] **Step 7: Add `case "select_fixes":` keymap in `menu.go`**

Bindings: `Space` → `toggleFixItem(selected)`, `Enter` → `confirmSelectedFixes()`, `'a'` → select all, `'n'` → deselect
all, `Esc`/`b` → `mode.Set("results")`, `q` → `d.stop()`, `Up`/`Down`/`j`/`k` → navigation.

- [ ] **Step 8: Add `renderSelectFixes()` in `menu_views.go` and update `Render` switch**

Render a checkbox list with title `"Select Updates to Apply"`. Each row: `[x]` or `[ ]` prefix, component name, action
label. Footer: `"[Space: Toggle · Enter: Apply selected · a: Select all · n: Deselect all · Esc: Cancel · q: Quit]"`.

- [ ] **Step 9: Wire results `'f'` key to `enterSelectFixes()`**

In the `"results"` keymap in `menu.go`, change `'f'` (currently `runAction("fix")`) to call `d.enterSelectFixes()`.

- [ ] **Step 10: Run full test suite**

Run: `make test`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add menu_state.go menu.go menu_views.go menu_state_test.go
git commit -m "feat: add component selection screen for batch fixes"
```

---

### Task 7: Rewrite All User-Facing Wording

**Files:**

- Modify: `menu_views.go` (all string literals)
- Modify: `menu_details.go` (section headings, status labels)
- Test: `menu_state_test.go`, `menu_details_test.go`

**Interfaces:**

- Consumes: all rendering functions from Tasks 4-6
- Produces: every user-visible string matches the spec's Section D.2 tables. No functional change — only text and
  labels.

- [ ] **Step 1: Write test for results screen wording**

```go
func TestResultsScreenWording(t *testing.T) {
// Render results view, extract text content.
// Assert: title contains "Workstation Check Results" (not "Workstation Audit Results")
// Assert: column header is "SUMMARY" (not "NOTE")
// Assert: summary pill uses "Update:" (not "NEED UPDATE:")
}
```

- [ ] **Step 2: Write test for detail screen wording**

```go
func TestDetailScreenWordingUpdate(t *testing.T) {
// Build detail for an UPDATE component.
// Assert: section heading is "Update Available" (not "Items Requiring Attention")
// Assert: status label is "Update available" (not "Attention / Update Required")
}
```

- [ ] **Step 3: Write test for history rendering backward-compatibility**

```go
func TestHistoryRenderingOldLabels(t *testing.T) {
// Create a store.ActionRecord with Label "Run pi install git:github.com/obra/superpowers"
// Build historyDetail(record).
// Assert: the detail renders without error and the old label appears in the output.
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test -race -run 'TestResultsScreenWording|TestDetailScreenWording|TestHistoryRendering' .`
Expected: FAIL (old wording still present)

- [ ] **Step 5: Update `renderResults()` wording in `menu_views.go`**

Apply all changes from spec Section D.2 "Results Screen" table:

- Line 362: `"Workstation Audit Results"` → `"Workstation Check Results"`
- Line 366: `"components inspected"` → `"components checked"`
- Line 388: `"NOTE"` → `"SUMMARY"`
- Lines 489-516: Summary pills — `"NEED UPDATE"` → `"Update"`, `"UNKNOWN"` → `"Unknown"`, remove brackets
- Footer: rewrite per spec

- [ ] **Step 6: Update `renderRunning()` wording**

Line 186: `"Running workstation health checks concurrently..."` → `"Checking component status..."`

- [ ] **Step 7: Update `componentDetail()` status labels in `menu_details.go`**

Line 211: `"OK"` stays. Line 213: `"Attention / Update Required"` → `"Update available"`. Line 215:
`"Not Applicable (N/A)"` → `"Not applicable"`. Line 217: `"Unknown"` stays.

- [ ] **Step 8: Update `renderPreviewAction()` wording in `menu_views.go`**

Title: `"Confirmed Action Preview"` → `"Confirm Update"`. Prompt: spec Section D.2 Confirm Screen table.

- [ ] **Step 9: Update `renderActionDone()` wording**

Title cases: `"Action Result & Verification"` → `"Update Completed"`, `"Action Execution Failed"` → `"Update Failed"`,
`"Action Execution Incomplete"` → use ratio format.

- [ ] **Step 10: Update `renderNoFixes()` wording**

Title: `"Automatic Maintenance Notice"` → `"All Components Are Current"`. Body: spec text. Remove reference to menu item
`[2]`.

- [ ] **Step 11: Update `renderHistory()` wording**

Title: `"Maintenance Action History"` → `"Update History"`. Column: `"LABEL"` → `"DESCRIPTION"`. Column: `"ACTION ID"` →
`"ID"` (shorter). Empty message: `"No maintenance actions recorded yet."` → `"No updates recorded yet."`.

- [ ] **Step 12: Update `historyDetail()` and `actionDetail()` section headings in `menu_details.go`**

`"Execution & Outcome"` → `"Result"`. `"Expected Side Effects"` → `"Side effects"`. `"Action Overview"` →
`"Update details"`.

- [ ] **Step 13: Run full test suite**

Run: `make test`
Expected: PASS

- [ ] **Step 14: Commit**

```bash
git add menu_views.go menu_details.go menu_state_test.go menu_details_test.go
git commit -m "feat: rewrite all user-facing wording per spec vocabulary"
```

---

### Task 8: Populate InstalledVersion in History and Add GitHub Rate-Limit Diagnostic

**Files:**

- Modify: `internal/app/service.go:143-155` (ActionStart construction)
- Modify: `internal/doctor/resource_git.go` (add API failure evidence)
- Modify: `internal/doctor/resource_sources.go` (add API failure evidence)
- Test: `internal/app/service_test.go`
- Test: `internal/doctor/resource_sources_test.go`

**Interfaces:**

- Consumes: `PreparedAction.proposal.Preconditions` (slice of `doctor.Fact`)
- Produces: `ActionStart.InstalledVersion` populated from the first precondition whose `Label` contains `"Installed"` or
  `"revision"`. When `h.Fetch` returns error in `inspectGitResource` or `evaluateResource` (npm), a `Fact` with label
  `"GitHub API status"` or `"npm API status"` is appended to `f.Evidence`.

- [ ] **Step 1: Write failing test for InstalledVersion population**

```go
func TestApplyPopulatesInstalledVersion(t *testing.T) {
// Setup: PreparedAction with Preconditions containing
// {Label: "Installed version", Value: "1.2.3"}.
// After Apply(), check the ActionStart.InstalledVersion recorded.
// Assert: == "1.2.3"
}
```

- [ ] **Step 2: Write failing test for git API failure diagnostic**

```go
func TestGitResourceAddsAPIFailureFact(t *testing.T) {
// Setup: h.Fetch returns error.
// Assert: finding.Evidence contains Fact with Label "GitHub API status"
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -race -run 'TestApplyPopulatesInstalled|TestGitResourceAddsAPI' ./...`
Expected: FAIL

- [ ] **Step 4: Populate `InstalledVersion` in `service.go` Apply method**

After constructing the `start` variable (around line 150), search `prepared.proposal.Preconditions` for a fact whose
`Label` contains `"Installed"` or `"revision"`, and set `start.InstalledVersion = fact.Value`.

- [ ] **Step 5: Add API failure diagnostic in `resource_git.go` and `resource_sources.go`**

In `inspectGitResource`, after the `h.Fetch` error check (around line 297), append an evidence fact before returning.
Same pattern in `evaluateResource` for the npm registry fetch (around line 272).

- [ ] **Step 6: Run full test suite**

Run: `make test`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/app/service.go internal/app/service_test.go internal/doctor/resource_git.go internal/doctor/resource_sources.go internal/doctor/resource_sources_test.go
git commit -m "fix: populate installed version in history and add API failure diagnostics"
```

---

## Self-Review Results

### 1. Spec Coverage

| Spec Section                       | Task            |
|------------------------------------|-----------------|
| B.1 Results First / eliminate menu | Task 4          |
| B.4 Merge manual steps             | Task 5          |
| B.5 Select fixes screen            | Task 6          |
| D.2 All wording                    | Task 7          |
| E.1 pi install → pi update         | Task 1          |
| E.2 Superpowers wording            | Task 2          |
| E.3 Herdr reload-config            | Task 3          |
| E.4 History InstalledVersion       | Task 8          |
| E.2 GitHub rate-limit diagnostic   | Task 3 + Task 8 |

No gaps found.

### 2. Step Scan

Every step names the exact file and location or the exact test signature. No "TBD" or "add appropriate handling" lines.
Steps 5-6 in Task 6 describe `confirmSelectedFixes()` at the design level — the signature change to
`prepareAutomaticFixes` (from `string` to `[]string`) is specified. The implementer can write the filter logic from the
existing code.

### 3. Type Consistency

- `fixCandidate` (Task 6) is defined once and used in `selectFixItems`, `toggleFixItem`, `confirmSelectedFixes`, and
  `renderSelectFixes`.
- `prepareAutomaticFixes` signature change from `string` to `[]string` (Task 6) is used in `confirmSelectedFixes` (Task
  6) and the existing `triggerAutomaticFix` call is updated to pass `nil` or `[]string{}` for "all".
- `ActionProposal.Steps[0].Command.Args[0]` value `"update"` (Task 1) is consistent across git and npm tests and
  implementations.

### 4. Review Focus Tests

All five Review Focus items have tests pinned to specific tasks:

1. GitHub API failure diagnostic → Task 3, Step 2 + Task 8, Step 2
2. `pi update` fallback to install → Task 1, Step 3
3. Batch with reload failure → Task 3, Step 1 (reload step existence) + existing `TestApplyStepCheckpointFailure` covers
   mid-batch failure
4. Zero-eligible select_fixes → Task 6, Step 3
5. Old history label rendering → Task 7, Step 3

### 5. Proportion

The spec design document is ~800 lines. This plan is ~450 lines. The plan does not contain function bodies — only
signatures, file locations, and test assertions. Proportion is acceptable.
