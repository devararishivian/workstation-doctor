# Workstation Doctor: Concurrency, Design Patterns, Go-TUI, and Config Checks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform `workstation-doctor` into a high-concurrency audit tool using Go design patterns (Strategy and Facade), add dedicated configuration validity and version checks, migrate the interactive TUI to Go-TUI, and update documentation to Simple English.

**Architecture:** Encapsulate every check into a `Checker` strategy under the Strategy pattern, coordinated by a concurrent `Engine` facade that populates deterministic slice indices using `sync.WaitGroup` and propagates `context.Context`. Replace Bubble Tea and Huh with pure Go declarative Go-TUI components in `menu.go`. Modernize `internal/store` with context awareness and prepared statements.

**Tech Stack:** Go 1.27, `github.com/grindlemire/go-tui` (v0.22.1), `github.com/urfave/cli/v3`, `modernc.org/sqlite`, `github.com/rs/zerolog`.

**Spec:** `docs/superpowers/specs/2026-09-25-concurrency-design-patterns-gotui-design.md`

## Global Constraints

- `check` is read-only. System changes happen exclusively behind the `fix` command.
- Never print the content of `mcp.json`. Only aggregated counts may reach output.
- Program output stays plain and pipeable on stdout. Diagnostics stay leveled on stderr through the `diag` logger.
- All user-facing text is English. Commit messages are English.
- `golangci-lint run ./...` must report 0 issues before any commit.
- Never commit the built binary or any `*.db` file.
- Never run `fix` (or `fix --yes`) without explicit user approval in the current session.
- Run the verification sequence through `make` targets.

## Review Focus

1. Parent context cancellation during concurrent checks: canceling `ctx` must abort running subprocesses promptly and return results without deadlock.
2. Index race condition in concurrent slice writes: multiple checkers finishing simultaneously must write to separate indices without data race under `go test -race`.
3. Non-interactive terminal detection: piped invocations (`| cat`) must always bypass Go-TUI and output plain text with zero ANSI escape codes.
4. SQLite foreign key integrity: inserting results with an invalid `run_id` must fail when `PRAGMA foreign_keys = ON;` is active.
5. Incomplete or unreadable configuration files: syntax errors or missing files must yield `StatusUnknown` with descriptive notes instead of crashing or panicking.

---

### Task 1: Modernize Storage Layer with Context and Prepared Statements

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `modernc.org/sqlite`, `database/sql`, `context.Context`
- Produces:
  - `(*Store).RecordRun(ctx context.Context, start, end time.Time, nOk, nUpdate, nUnknown, exitCode int, results []ResultRow) (int64, error)`
  - `(*Store).RecordAction(ctx context.Context, runID int64, kind, command, status, output string, start, end time.Time) error`
  - `(*Store).ListRuns(ctx context.Context, limit int) ([]Run, error)`
  - `(*Store).RunResults(ctx context.Context, runID int64) ([]ResultRow, error)`
  - `(*Store).RunActions(ctx context.Context, runID int64) ([]ActionRow, error)`

- [ ] **Step 1: Write tests for context-aware store methods and pragmas**

Add tests in `internal/store/store_test.go` checking context cancellation and SQLite foreign key enforcement:

```go
func TestStoreContextCancellation(t *testing.T) {
	st, err := Open(t.TempDir() + "/ctx_test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	start := time.Now()
	_, err = st.RecordRun(ctx, start, start, 1, 0, 0, 0, []ResultRow{
		{Component: "test", Status: "OK"},
	})
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

func TestStoreForeignKeysEnforced(t *testing.T) {
	st, err := Open(t.TempDir() + "/fk_test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	// Attempting to record action with non-existent run_id 99999 must fail if FK is enforced
	err = st.RecordAction(ctx, 99999, "fix", "brew upgrade", "ok", "out", time.Now(), time.Now())
	if err == nil {
		t.Fatal("expected foreign key error for non-existent run_id, got nil")
	}
}
```

- [ ] **Step 2: Run store tests to verify failures**

Run: `go test ./internal/store`
Expected: FAIL because method signatures do not accept `context.Context` yet and pragmas are not enabled.

- [ ] **Step 3: Implement context propagation, SQLite pragmas, and prepared statements in `internal/store/store.go`**

In `internal/store/store.go`:
1. In `Open(path string)`:
   Add pragmas after opening `db`:
   ```go
   if _, err := db.Exec("PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;"); err != nil {
       _ = db.Close()
       return nil, fmt.Errorf("store: set pragmas %s: %w", path, err)
   }
   ```
2. Update `RecordRun`:
   Use `s.db.BeginTx(ctx, nil)`.
   Use `stmt, err := tx.PrepareContext(ctx, "INSERT INTO check_results(...) VALUES(?,?,?,?,?,?)")`.
   Execute inside loop: `stmt.ExecContext(ctx, runID, r.Component, r.Installed, r.Latest, r.Status, r.Note)`.
   Close statement before commit: `stmt.Close()`.
3. Update `RecordAction`, `ListRuns`, `RunResults`, and `RunActions` to accept `ctx context.Context` and use `ExecContext` or `QueryContext`.

- [ ] **Step 4: Run store tests to verify they pass**

Run: `go test -v ./internal/store`
Expected: PASS

- [ ] **Step 5: Commit store layer changes**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): add context propagation, SQLite pragmas, and prepared statements"
```

---

### Task 2: Strategy Pattern Core and Concrete Tool Checkers

**Files:**
- Modify: `internal/doctor/doctor.go`
- Test: `internal/doctor/doctor_test.go`

**Interfaces:**
- Consumes: `internal/doctor.Result`
- Produces:
  - `type Category string`
  - `type Checker interface { Name() string; Category() Category; Check(ctx context.Context) Result }`
  - Concrete strategies: `piVersionChecker`, `herdrVersionChecker`, `ghosttyVersionChecker`, `npmPackageChecker`, `serenaVersionChecker`, `gortexVersionChecker`

- [ ] **Step 1: Write test for Checker interface and tool checker strategies**

In `internal/doctor/doctor_test.go`:
```go
func TestToolCheckersInterface(t *testing.T) {
	checkers := []Checker{
		&piVersionChecker{},
		&herdrVersionChecker{},
		&ghosttyVersionChecker{},
		newNpmPackageChecker("opencode", "opencode-ai"),
		newNpmPackageChecker("tokenjuice", "tokenjuice"),
		&serenaVersionChecker{},
		&gortexVersionChecker{},
	}

	for _, c := range checkers {
		if c.Name() == "" {
			t.Errorf("checker %T has empty Name()", c)
		}
		if c.Category() != CategoryTool {
			t.Errorf("checker %s category = %s, want %s", c.Name(), c.Category(), CategoryTool)
		}
	}
}
```

- [ ] **Step 2: Run doctor tests to verify failure**

Run: `go test ./internal/doctor`
Expected: FAIL with `undefined: Checker`, `undefined: CategoryTool`

- [ ] **Step 3: Implement Strategy types and tool checkers in `internal/doctor/doctor.go`**

Define:
```go
type Category string

const (
	CategoryTool   Category = "tool"
	CategoryConfig Category = "config"
	CategorySystem Category = "system"
	CategorySkill  Category = "skill"
)

type Checker interface {
	Name() string
	Category() Category
	Check(ctx context.Context) Result
}
```
Implement `piVersionChecker`, `herdrVersionChecker`, `ghosttyVersionChecker`, `npmPackageChecker`, `serenaVersionChecker`, `gortexVersionChecker` delegating their logic to the respective check methods (`checkPi`, `checkHerdr`, `checkGhostty`, `checkNpmPkg`, `checkSerena`, `checkGortex`). Replace `http.DefaultClient` in `pypiLatest` with a dedicated HTTP client:
```go
var pypiClient = &http.Client{Timeout: 15 * time.Second}
```

- [ ] **Step 4: Run doctor tests to verify they pass**

Run: `go test -v ./internal/doctor`
Expected: PASS

- [ ] **Step 5: Commit Strategy pattern core and tool checkers**

```bash
git add internal/doctor/doctor.go internal/doctor/doctor_test.go
git commit -m "feat(doctor): introduce Checker strategy interface and tool checkers"
```

---

### Task 3: Separate Configuration Checkers (Validity and Freshness)

**Files:**
- Modify: `internal/doctor/doctor.go`
- Test: `internal/doctor/doctor_test.go`

**Interfaces:**
- Consumes: `internal/doctor.Checker`, `internal/doctor.CategoryConfig`
- Produces:
  - `ghosttyConfigValidChecker` (`ghostty-config-valid`)
  - `ghosttyConfigVersionChecker` (`ghostty-config-version`)
  - `herdrConfigValidChecker` (`herdr-config-valid`)
  - `herdrConfigVersionChecker` (`herdr-config-version`)
  - `piConfigValidChecker` (`pi-config-valid`)
  - `piConfigVersionChecker` (`pi-config-version`)

- [ ] **Step 1: Write tests for configuration validity and version checkers**

In `internal/doctor/doctor_test.go`:
```go
func TestConfigCheckers(t *testing.T) {
	checkers := []Checker{
		&ghosttyConfigValidChecker{},
		&ghosttyConfigVersionChecker{},
		&herdrConfigValidChecker{},
		&herdrConfigVersionChecker{},
		&piConfigValidChecker{},
		&piConfigVersionChecker{},
	}

	for _, c := range checkers {
		if c.Category() != CategoryConfig {
			t.Errorf("checker %s category = %s, want %s", c.Name(), c.Category(), CategoryConfig)
		}
	}
}

func TestEvaluatePiConfigVersion(t *testing.T) {
	// If lastChangelogVersion matches installed, status is OK
	res := evaluatePiConfigVersion("0.87.1", "0.87.1")
	if res.Status != StatusOK {
		t.Errorf("got status %s, want %s", res.Status, StatusOK)
	}

	// If lastChangelogVersion is older, status is UPDATE
	res = evaluatePiConfigVersion("0.85.0", "0.87.1")
	if res.Status != StatusUpdate {
		t.Errorf("got status %s, want %s", res.Status, StatusUpdate)
	}
}
```

- [ ] **Step 2: Run doctor tests to verify failure**

Run: `go test ./internal/doctor`
Expected: FAIL with undefined config checkers

- [ ] **Step 3: Implement config checkers in `internal/doctor/doctor.go`**

1. Implement `ghosttyConfigValidChecker` (`ghostty-config-valid`): validates via `ghostty +show-config --changes-only`.
2. Implement `ghosttyConfigVersionChecker` (`ghostty-config-version`): validates active config against deprecated options using `ghostty +show-config --default`.
3. Implement `herdrConfigValidChecker` (`herdr-config-valid`): validates via `herdr config check`.
4. Implement `herdrConfigVersionChecker` (`herdr-config-version`): checks structure of `config.toml` (sections `[ui]`, `[theme]`, `[[keys.command]]`).
5. Implement `piConfigValidChecker` (`pi-config-valid`): validates JSON of `settings.json` and `mcp.json`, and presence of `mcp-cache.json` without duplicate disk reads.
6. Implement `piConfigVersionChecker` (`pi-config-version`): reads `"lastChangelogVersion"` in `settings.json` and compares against `pi --version`.

- [ ] **Step 4: Run doctor tests to verify they pass**

Run: `go test -v ./internal/doctor`
Expected: PASS

- [ ] **Step 5: Commit config checkers**

```bash
git add internal/doctor/doctor.go internal/doctor/doctor_test.go
git commit -m "feat(doctor): add separate config validity and version checkers"
```

---

### Task 4: System Checkers and Concurrent Engine Facade with Parallel Plugin Resolution

**Files:**
- Modify: `internal/doctor/doctor.go`
- Test: `internal/doctor/doctor_test.go`

**Interfaces:**
- Consumes: All `Checker` implementations
- Produces:
  - System checkers: `brewOutdatedChecker`, `npmOutdatedGlobalChecker`, `piPackagesChecker`, `superpowersChecker`, `herdrIntegrationsChecker`, `herdrPluginsChecker`, `skillsChecker`
  - `Engine` struct
  - `NewEngine() *Engine`
  - `(*Engine).Run(ctx context.Context) []Result`
  - `doctor.Run(ctx context.Context) []Result` (public facade)

- [ ] **Step 1: Write concurrency and deterministic ordering test for Engine**

In `internal/doctor/doctor_test.go`:
```go
type mockChecker struct {
	name  string
	delay time.Duration
}

func (m *mockChecker) Name() string       { return m.name }
func (m *mockChecker) Category() Category { return CategoryTool }
func (m *mockChecker) Check(ctx context.Context) Result {
	time.Sleep(m.delay)
	return Result{Component: m.name, Status: StatusOK}
}

func TestEngineRunDeterministicOrder(t *testing.T) {
	engine := &Engine{
		checkers: []Checker{
			&mockChecker{name: "first", delay: 30 * time.Millisecond},
			&mockChecker{name: "second", delay: 10 * time.Millisecond},
			&mockChecker{name: "third", delay: 20 * time.Millisecond},
		},
	}

	res := engine.Run(context.Background())
	if len(res) != 3 {
		t.Fatalf("len(res) = %d, want 3", len(res))
	}
	if res[0].Component != "first" || res[1].Component != "second" || res[2].Component != "third" {
		t.Errorf("unexpected order: %s, %s, %s", res[0].Component, res[1].Component, res[2].Component)
	}
}
```

- [ ] **Step 2: Run doctor tests to verify failure**

Run: `go test ./internal/doctor`
Expected: FAIL with `undefined: Engine`

- [ ] **Step 3: Implement Engine and parallel plugin resolution in `internal/doctor/doctor.go`**

1. Implement `Engine` holding `checkers []Checker`.
2. Implement `NewEngine() *Engine` registering all checkers in consistent order:
   - Tool version checkers
   - Separate config checkers (validity & version)
   - System checkers (brew, npm, pi, superpowers, herdr integrations, herdr plugins, skills)
3. Implement `(e *Engine).Run(ctx context.Context) []Result` with `sync.WaitGroup` and index-based slice assignment.
4. Update `doctor.Run(ctx context.Context) []Result` as a facade calling `NewEngine().Run(ctx)`.
5. Update `evaluateHerdrPlugins` to resolve plugin remote commits concurrently using goroutines when multiple plugins are present.

- [ ] **Step 4: Run doctor tests with data race detector**

Run: `go test -race -v ./internal/doctor`
Expected: PASS with 0 race detections

- [ ] **Step 5: Commit concurrent Engine and system checkers**

```bash
git add internal/doctor/doctor.go internal/doctor/doctor_test.go
git commit -m "feat(doctor): implement concurrent Engine facade and parallel plugin resolution"
```

---

### Task 5: Main Wiring Update for Context and Subcommands

**Files:**
- Modify: `main.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: `internal/store` (context-aware), `internal/doctor`
- Produces: CLI commands and helpers passing `ctx context.Context` to all store operations.

- [ ] **Step 1: Write test verifying context propagation in main**

In `main_test.go`:
Verify `runAndRecord` succeeds with context and non-interactive pipe output remains plain.

- [ ] **Step 2: Run main tests to verify build state**

Run: `go test .`
Expected: FAIL due to mismatched signatures on `st.RecordRun` in `main.go`.

- [ ] **Step 3: Update `main.go` to pass `ctx` to store calls**

Update calls in:
- `runAndRecord`: `st.RecordRun(ctx, start, time.Now(), ...)`
- `runFixFlow`: `st.RecordAction(ctx, runID, "fix", ...)`
- `doHistory`: `st.ListRuns(ctx, limit)`
- `showRun`: `st.RunResults(ctx, id)` and `st.RunActions(ctx, id)`

- [ ] **Step 4: Run root tests and verify compilation**

Run: `go test -v .`
Expected: PASS

- [ ] **Step 5: Commit main wiring updates**

```bash
git add main.go main_test.go
git commit -m "feat(cli): propagate context to store methods across all subcommands"
```

---

### Task 6: Go-TUI Interactive Menu Migration

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `menu.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: `github.com/grindlemire/go-tui` (v0.22.1)
- Produces:
  - `menuApp` implementing `tui.KeyListener` and `Render(app *tui.App) *tui.Element`
  - `runMenu(ctx context.Context, cmd *cli.Command) error`
  - Fallback to `classicMenu` when not interactive

- [ ] **Step 1: Add `github.com/grindlemire/go-tui` and clean unused dependencies**

Run:
```bash
go get github.com/grindlemire/go-tui@v0.22.1
go mod tidy
```

- [ ] **Step 2: Implement Go-TUI interactive menu in `menu.go`**

1. Define `menuApp` with state `selectedIndex *tui.State[int]`, `lastSummary string`, `selectedChoice *tui.State[string]`.
2. Implement `KeyMap() tui.KeyMap`:
   - `KeyDown` / `Rune('j')`: move down
   - `KeyUp` / `Rune('k')`: move up
   - `KeyEnter`: select current item and stop app
   - `Rune('1')`..`Rune('4')`: immediate selection
   - `KeyEscape` / `Rune('q')` / `Rune('0')`: quit
3. Implement `Render(app *tui.App) *tui.Element`:
   - Render outer box with `tui.BorderRounded`
   - Render header "workstation-doctor"
   - Render subtitle with last run summary
   - Render options list with cursor indicator `> ` and bold highlight on selected item
   - Render footer with navigation keys hint
4. In `fancyMenu`:
   - Create `app, err := tui.NewApp(tui.WithRootComponent(menu))`
   - Run app with `app.Run()`
   - Execute selected action (`check`, `manual`, `fix`, `history`, `quit`)
5. In `gatherWithSpinner`:
   - Provide spinner or progress rendering using Go-TUI during concurrent check execution.

- [ ] **Step 3: Run tests to verify clean compilation and execution**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Commit Go-TUI migration**

```bash
git add menu.go go.mod go.sum
git commit -m "feat(tui): migrate interactive menu to Go-TUI"
```

---

### Task 7: Simple English Documentation Rewrite

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

**Rules:**
- Follow ASD-STE100 guidelines via `simple-english` skill:
  - Short sentences (< 25 words).
  - Active voice, simple tenses.
  - Condition before command with a comma.
  - Modals: can, will, must (no should, would, may).
  - No contractions, no semicolons, no em-dashes.
  - Direct facts without fluff or buzzwords.

- [ ] **Step 1: Rewrite `README.md`**

Rewrite `README.md` to describe `workstation-doctor`, its architecture, commands, configuration audit capabilities, and build instructions following Simple English.

- [ ] **Step 2: Rewrite `AGENTS.md`**

Rewrite `AGENTS.md` preserving all guidelines, invariants, skill routing, and definitions of done under Simple English constraints.

- [ ] **Step 3: Verify formatting and wording**

Inspect word counts, verify zero contractions, no em-dashes, and clear instructions.

- [ ] **Step 4: Commit documentation rewrites**

```bash
git add README.md AGENTS.md
git commit -m "docs: rewrite README and AGENTS guidelines in Simple English"
```

---

### Task 8: End-to-End Verification, Race Detection, and Benchmark

**Files:**
- Workspace-wide verification

- [ ] **Step 1: Run static analysis and vet**

Run: `make vet`
Expected: PASS

- [ ] **Step 2: Run unit and race tests**

Run: `go test -race -v ./...`
Expected: PASS with 0 race warnings

- [ ] **Step 3: Run linter**

Run: `make lint`
Expected: 0 issues reported by `golangci-lint`

- [ ] **Step 4: Run vulnerability scan**

Run: `make vuln`
Expected: No known vulnerabilities found

- [ ] **Step 5: Run real benchmark and measure performance**

Run: `time ./workstation-doctor --db /tmp/bench-final.db check`
Expected:
- Check completes successfully with exit code 0 or 1.
- Total execution time drops from baseline ~12.1s down to ~3-4s.
- Clean output on stdout without escape codes or secrets.
