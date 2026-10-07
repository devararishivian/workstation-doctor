# Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for native execution or superpowers:subagent-driven-development only after operator authorization. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a bounded read-only core without mixing file moves with corrected workstation behavior.

**Architecture:** Split the existing doctor package first. Add new typed contracts and an engine beside the legacy API. Disable legacy maintenance before new findings can reach a shell executor.

**Tech Stack:** Existing Go toolchain, standard testing, existing dependencies, and race-enabled Make targets.

**Spec:** [Approved architecture](../specs/2026-10-07-workstation-doctor-architecture-redesign-design.md).

## Global Constraints

- Read [roadmap contracts and limits](2026-10-07-workstation-doctor-redesign-roadmap.md) and `AGENTS.md` before this plan.
- macOS is primary and Linux is supported. Root `main.go` and Go-TUI remain.
- No real package updates, default database access, or real MCP fixtures.
- Keep structural and behavioral commits separate. No runtime plugins, DI container, or universal filesystem interface.
- Legacy reports stay unchanged during Task 2. Target behavior is not wired into the TUI yet.
- Verify every commit with `make vet test lint vuln`; lint must report `0 issues.`.

## Review Focus

- Empty or duplicate registration must fail at construction: Task 4's registry tests.
- Cancellation before discovery must not yield success: Task 5's canceled-audit test.
- A command with stdout and an unexpected non-zero exit must remain failed: Task 5's exit test.
- Symlink aliases and separate installations must retain distinct meanings: Task 4's identity test.
- A new finding must never reach either old maintenance path: Task 3's guard tests.

## File structure

Task 2 creates `legacy_model.go`, `legacy_engine.go`, `legacy_inspect.go`, `report.go`, `pi.go`, `herdr.go`, `ghostty.go`,
`starship.go`, `npm.go`, `serena.go`, `gortex.go`, `brew.go`, `pi_packages.go`, `superpowers.go`, `herdr_plugins.go`, and
`skills.go` under `internal/doctor`. Related configuration declarations stay in their integration file.
Keep `doctor.go` as the package overview. Preserve existing tests in `doctor_test.go` until their owning behavioral stage changes them.

Task 4 adds `model.go`, `identity.go`, `actions.go`, `registry.go`, `limits.go`, `host.go`, and matching model tests.
Task 5 extends `host.go` and adds `audit_engine.go` and corresponding tests. Root guard tests live in `main_test.go` and new `menu_test.go`.

### Task 1: Establish isolated characterization and focused verification

**Files:** Modify `Makefile`, `internal/doctor/doctor_test.go`, `main_test.go`; create `internal/doctor/test_support_test.go`.

**Interfaces:** Preserve `Engine.Run(context.Context) []Result`. Produce optional Make variables `TEST_PKG` (default `./...`)
and `TEST_RUN` (default `.`). Produce `testHost(t *testing.T) *Host` only after Host exists in Task 5.
Until then, tests use temporary roots and existing mock checkers. Do not invent a global environment fixture.

- [ ] Add `TestLegacyRegistryOrder` that compares all 21 registered names in existing order without calling their checks.
- [ ] Add `TestLegacyFormattingSnapshot` using synthetic results. Assert the current labels, plain output, and status mapping.
- [ ] Run `make test`; expect the characterization tests and current suite to pass. Record the baseline, including tests that encode rejected heuristics.
- [ ] Parameterize `make test` as `go test -race -run '$(TEST_RUN)' $(TEST_PKG)` while preserving its default package set.
- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN=TestLegacyRegistryOrder`; expect PASS. Restore a deliberately perturbed local expectation and prove the assertion fails, then restore it.
- [ ] Run the global gate and commit exact files: `test: characterize doctor registry and reports`.

### Task 2: Split doctor by responsibility without changing behavior

**Files:** Modify `internal/doctor/doctor.go`; create the Task 2 files listed above. Keep `doctor_test.go` unchanged.

**Interfaces:** Consume and preserve all current types, functions, and checker methods. Produce the same package API and registration order from smaller files.

- [ ] Map each existing declaration to its destination. Keep npm helpers in `npm.go`, shared old command helpers in `legacy_inspect.go`, and Git helpers with their resource consumer.
- [ ] Move declarations without renaming, algorithm corrections, status changes, or modified command strings. Keep `Result` and old `Engine` in explicitly named legacy files.
- [ ] Run `make test`; expect every pre-move assertion to pass, including known legacy heuristics.
- [ ] Inspect the diff for moves only. Do not combine dependency, output, or storage changes with this task.
- [ ] Run the global gate and commit all moved doctor files: `refactor: split doctor checks into focused files`.

### Task 3: Block old automatic maintenance during migration

**Files:** Modify `main.go`, `menu.go`, `main_test.go`; create `menu_test.go` and `maintenance_guard.go`.

**Interfaces:** Produce `legacyMaintenanceError() error`. Preserve the old entry signatures until Plan 5 removes them.
`doFix`, `runFixFlow`, `doctorApp.runAction("fix")`, and `doctorApp.executeFix` must all stop before audit, store, or process execution.

- [ ] Add `TestLegacyMaintenanceBlocked` and `TestMenuMaintenanceBlocked`. Give the pending fixture only a temporary helper-program command, never an installed updater.

```go
err := legacyMaintenanceError()
if err == nil || err.Error() != "Automatic maintenance is unavailable during the architecture migration." {
    t.Fatalf("unexpected guard: %v", err)
}
```

- [ ] Run `make test TEST_RUN='TestLegacyMaintenanceBlocked|TestMenuMaintenanceBlocked'`; expect FAIL before the guard exists.
- [ ] Implement the guard in all entry paths, including direct confirmation callbacks and `--yes`. Assert the temporary marker and test database are never created.
- [ ] Run the focused tests; expect PASS. A TUI status explains the disabled capability rather than claiming an update succeeded.
- [ ] Run the global gate and commit: `fix: block legacy maintenance during redesign migration`.

### Task 4: Define typed evidence, proposals, identity, and registry

**Files:** Create `internal/doctor/model.go`, `identity.go`, `actions.go`, `registry.go`, `limits.go`, `host.go`, and matching model `_test.go` files.

**Interfaces:** Produce roadmap model structs plus `CanonicalInstanceID(integrationID, scope, path string) (string, error)`,
`ValidateDefinitions(defs []Definition) error`, and `ValidateProposal(proposal ActionProposal, limits Limits) error`.
Enum constants use type-specific prefixes, such as `AvailabilityPresent`, `OutcomeOK`, `EvidenceKnown`, and `ActionAutomatic`.
Declare Definition and CheckDefinition in `registry.go` and Host's function-field struct in `host.go`, so registry types compile independently.
Declare Limits and `DefaultLimits()` in `limits.go` here for proposal tests; Task 5 supplies concrete Host I/O behavior.

- [ ] Add table tests `TestDefinitionValidation`, `TestCanonicalInstanceID`, and `TestProposalLimits` for blank IDs, duplicate check IDs,
missing discovery/evaluation functions, missing/duplicate positive Order values, aliases, separate prefixes, and oversized steps/arguments.

```go
limits := DefaultLimits()
proposal := ActionProposal{Steps: make([]CommandStep, 33)}
if err := ValidateProposal(proposal, limits); err == nil {
    t.Fatal("expected rejection above 32 steps")
}
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestDefinitionValidation|TestCanonicalInstanceID|TestProposalLimits'`; expect FAIL.
- [ ] Define roadmap types. Use explicit source/state on facts and nil installation evidence when unavailable. Resolve existing-path aliases without collapsing nonexistent explicit overrides into defaults.
- [ ] Validate uniqueness within each integration and unique integration IDs across definitions. Do not create production no-op check registrations.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: define discovered instance and finding contracts`.

### Task 5: Add bounded inspection and the generic audit engine

**Files:** Extend `internal/doctor/host.go`, `limits.go`; create `audit_engine.go` and corresponding tests; finish `test_support_test.go`.

**Interfaces:** Produce `NewHost(scope Scope, limits Limits) (*Host, error)`, `ReadBounded(ctx context.Context, path string, limit int64) ([]byte, error)`,
and roadmap `NewAuditEngine`, `Audit`, and `Inspect`. `Host` holds only inspection dependencies and an audit-local cache.

- [ ] Add `TestAuditDiscoveryOnce`, `TestAuditDeterministicOrder`, `TestAuditCanceledBeforeStart`, `TestInspectionExitSemantics`,
`TestInspectionLimits`, and `TestAuditNoStorage`. Fake definitions return deliberately reversed completion order.

```go
ctx, cancel := context.WithCancel(context.Background())
cancel()
report := engine.Audit(ctx, host, Scope{})
if !report.Canceled || discoveryCalls != 0 {
    t.Fatal("canceled audit scheduled discovery")
}
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestAudit|TestInspection'`; expect FAIL before implementation.
- [ ] Implement index-based stable results, per-integration shared discovery, bounded scheduling, fresh `Inspect`, and roadmap limits.
Retain completed results after cancellation. Generate `NotApplicable` for absent/unsupported prerequisites and `Unknown` for undetermined discovery.
Sort by registration check order, then integration-defined stable instance order. Derive report Checks from registration.
Create a fresh cache for every Audit/Inspect without copying mutexes or retaining inventory across audits.
- [ ] Implement direct-argv read commands with bounded combined capture and explicit provider exit acceptance. Never accept every non-zero exit merely because stdout exists.
Use HTTP request contexts and bounded bodies. Read errors and resource limits produce safe diagnostics without raw configuration.
- [ ] Run focused tests with zero real commands/network, then `make vet test lint vuln`; expect PASS. Commit: `feat: add bounded read-only audit engine`.

## Stage exit

The old executable still builds and its maintenance paths are blocked. New core tests require no store or installed product.
There is no TUI cutover or database migration in this plan. Review identity, enum, and limit contracts before Plan 2.
