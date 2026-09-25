# Workstation Doctor Architecture Spec: Concurrency, Design Patterns, Go-TUI, and Config Checks

## 1. Overview and Problem Statement

The `workstation-doctor` CLI tool currently performs 16 health and version checks sequentially. Running these checks sequentially creates a latency bottleneck of over 12 seconds per run because of network round-trips (npm registry, PyPI, Homebrew info, and git ls-remote) and external subprocess invocations.

In addition:
- Configuration validation and configuration freshness checks are currently coupled or incomplete across Ghostty, Pi, and Herdr.
- Check routines are defined as ad-hoc closures without a formalized design pattern structure.
- The interactive terminal interface relies on Bubble Tea and Huh, whereas the project requires migration to Go-TUI (`github.com/grindlemire/go-tui`).
- Database methods in `internal/store` lack context propagation, prepared statements, and SQLite integrity pragmas.
- Project documentation (`README.md` and `AGENTS.md`) needs simplification under Simple English standards.

## 2. Goals and Non-Goals

### Goals
- Accelerate audit execution from ~12s down to ~3-4s using deterministic concurrent goroutines with `sync.WaitGroup`.
- Implement the Strategy and Facade patterns from the Go Design Patterns knowledge base (`golang-design-patterns-kb`).
- Introduce separate, dedicated checks for configuration validity and configuration version freshness.
- Replace Bubble Tea and Huh with Go-TUI (`github.com/grindlemire/go-tui`) using pure Go declarative components without external build dependencies.
- Update `internal/store` to be fully context-aware with prepared statements and SQLite foreign key enforcement.
- Rewrite `README.md` and `AGENTS.md` following the `simple-english` style guide.
- Maintain zero issues under `golangci-lint run ./...` and preserve all repo invariants (clean stdout for piping, diagnostics to stderr, read-only checks).

### Non-Goals
- Altering the read-only invariant of `check`. System modifications remain exclusively behind the `fix` command.
- Printing any secrets from `mcp.json`. Only aggregate counts will be displayed.
- Introducing a C compiler dependency or changing pure Go SQLite drivers.

## 3. Architecture and Design Patterns

### 3.1 Strategy Pattern (`internal/doctor`)
Following the canonical Strategy pattern (`Strategy.md`):
- **Intent**: Define a family of audit algorithms, encapsulate each into a concrete type, and make them interchangeable.
- **Contract**:
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
- **Concrete Strategies**:
  - **Tool Version Checks**: `piVersionChecker`, `herdrVersionChecker`, `ghosttyVersionChecker`, `npmPackageChecker` (opencode, tokenjuice), `serenaVersionChecker`, `gortexVersionChecker`.
  - **Configuration Validity Checks**:
    - `ghosttyConfigValidChecker`: executes `ghostty +show-config --changes-only`.
    - `herdrConfigValidChecker`: executes `herdr config check`.
    - `piConfigValidChecker`: validates JSON syntax of `settings.json` and `mcp.json`, and verifies `mcp-cache.json` existence.
  - **Configuration Version / Freshness Checks**:
    - `ghosttyConfigVersionChecker`: inspects active configuration against `ghostty +show-config --default` for deprecated options.
    - `herdrConfigVersionChecker`: validates schema sections (`[ui]`, `[theme]`, `[[keys.command]]`).
    - `piConfigVersionChecker`: compares `"lastChangelogVersion"` in `settings.json` with the installed `pi --version`.
  - **System and Skills Checks**: `brewOutdatedChecker`, `npmOutdatedGlobalChecker`, `piPackagesChecker`, `superpowersChecker`, `herdrIntegrationsChecker`, `herdrPluginsChecker`, `skillsChecker`.

### 3.2 Facade Pattern (`internal/doctor.Engine`)
Following the canonical Facade pattern (`Facade.md`):
- `Engine` coordinates checker registration and deterministic parallel execution.
- `doctor.Run(ctx)` remains the public facade entry point to preserve backward compatibility with `main.go`.

## 4. Concurrent Execution Engine

### 4.1 Deterministic Concurrent Slices
To ensure 100% deterministic output order without locking overhead:
```go
func (e *Engine) Run(ctx context.Context) []Result {
    results := make([]Result, len(e.checkers))
    var wg sync.WaitGroup
    for i, chk := range e.checkers {
        wg.Add(1)
        go func(idx int, c Checker) {
            defer wg.Done()
            results[idx] = c.Check(ctx)
        }(i, chk)
    }
    wg.Wait()
    return results
}
```
- Each goroutine writes exclusively to its dedicated index `results[idx]`.
- No shared memory mutations occur; data-race free under `go test -race`.
- `ctx` is propagated to every checker for cancellation upon SIGINT.

### 4.2 Herdr Plugins Concurrency
`herdrPluginsChecker` resolves multiple plugin remote commits in parallel using goroutines instead of sequentially invoking `git ls-remote`.

## 5. Storage Layer Modernization (`internal/store`)

- Add `context.Context` to all methods:
  - `RecordRun(ctx context.Context, start, end time.Time, nOk, nUpdate, nUnknown, exitCode int, results []ResultRow) (int64, error)`
  - `RecordAction(ctx context.Context, runID int64, kind, command, status, output string, start, end time.Time) error`
  - `ListRuns(ctx context.Context, limit int) ([]Run, error)`
  - `RunResults(ctx context.Context, runID int64) ([]ResultRow, error)`
  - `RunActions(ctx context.Context, runID int64) ([]ActionRow, error)`
- Enforce foreign keys and busy timeout:
  - Execute `PRAGMA foreign_keys = ON;` and `PRAGMA busy_timeout = 5000;` on connection.
- Use `tx.PrepareContext(ctx, ...)` for inserting into `check_results` inside `RecordRun`.

## 6. Go-TUI Migration (`menu.go`)

### 6.1 Pure Go Implementation
- Use `github.com/grindlemire/go-tui` without requiring `.gsx` build tools.
- Implement `menuApp` satisfying `tui.KeyListener`:
  - `KeyMap()` handles `KeyDown`, `KeyUp`, `Rune('j')`, `Rune('k')`, `KeyEnter`, `Rune('1')`..`Rune('4')`, `KeyEscape`, `Rune('q')`.
  - `Render(app *tui.App) *tui.Element` renders a responsive, rounded box menu with item selection highlighting.
- Maintain terminal detection:
  - When `isInteractive()` is true, use Go-TUI.
  - When non-interactive (piped, automation, tests), fallback to `classicMenu` (plain text prompt).

## 7. Simple English Documentation

Rewrite `README.md` and `AGENTS.md` following ASD-STE100 guidelines:
- Short, active-voice sentences.
- Condition before command with a comma.
- One meaning per word.
- Modals limited to can, will, and must.
- Removal of promotional or fluffy language.

## 8. Verification Plan

1. `make test` and `go test -race ./...` to verify concurrent safety.
2. `make vet` for static analysis.
3. `make lint` confirming 0 issues from `golangci-lint`.
4. `make vuln` for dependency scan.
5. Real execution benchmark: `./workstation-doctor --db /tmp/bench.db check` comparing sequential vs concurrent execution time.
