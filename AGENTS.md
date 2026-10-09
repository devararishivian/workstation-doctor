# Agent Guidelines: workstation-doctor

This guide defines operating standards and safety boundaries for AI coding agents working on `workstation-doctor`.

## 1. Project Mission

`workstation-doctor` audits developer workstation tools and coordinates safe maintenance through a single interactive
terminal dashboard (TUI). All user-facing documentation and application text must be in English.

## 2. Architecture Map

- **Root CLI & TUI (`main.go`, `menu*.go`)**:
    - `main.go`: Entrypoint using `urfave/cli/v3` for launch flags. Requires an interactive terminal
      (`An interactive terminal is required.` on stderr when non-TTY).
    - `menu.go`: TUI lifecycle, keyboard listener, and event loops.
    - `menu_state.go`: Reactive state, generation guarding against late async updates, and navigation modes.
    - `menu_views.go`: Catppuccin-themed layout rendering (summary dashboard, component details, manual steps, action
      preview, history browser).
    - `menu_details.go`: Structured models (`Detail`, `DetailSection`, `DetailRow`) formatting component facts and
      history.
- **Application Service (`internal/app`)**:
    - `service.go`: Coordinates read-only audits, target inspections, action preparation, confirmation binding,
      execution, and history queries.
    - `ownership.go` / `ownership_unix.go`: Kernel advisory locking (`Flock`) under the user state directory with orphan
      marker crash detection.
    - `preview.go`: Immutable frozen action proposals and single-use fingerprint-bound approvals.
    - `executor.go` / `executor_unix.go`: Direct-argv step execution inside isolated process groups with process
      termination on cancel.
    - `migration.go`: Legacy history migration with explicit consent.
    - `release_notes.go`: Bounded HTTPS retrieval and ANSI escape sanitization.
    - `paths.go`: Platform-specific database and state directory resolution (`Application Support` on macOS, XDG on
      Linux).
- **Health Engine & Registry (`internal/doctor`)**:
    - `builtin.go`: Explicit registration of all 18 built-in checks.
    - `audit_engine.go`: Bounded concurrent scheduling with cancellation and fresh inspection.
    - `model.go`, `registry.go`, `identity.go`, `actions.go`, `limits.go`: Domain models, proposal validation, and
      safety limits.
    - `host.go`: Bounded read-only command execution, HTTPS metadata fetching, and file reads.
    - `discovery.go`, `inventory*.go`, `tool_instances.go`, `tool_native.go`, `manager_instances.go`: Local tool and
      package discovery.
    - `skills.go`: Agent Skills frontmatter parsing, validation, and directory matching.
- **Action History Storage (`internal/store`)**:
    - `history.go`: SQLite connection pooling, pragmas, atomic start and step checkpointing.
    - `history_model.go`: Domain models for action attempts, steps, and query pagination.
    - `history_queries.go`: Filtered, indexed, paginated action history queries.
    - `history_schema.go`: Schema versions (v1, v2, v3) and transactional migrations.
    - `retention.go`: Retention enforcement (age, count) for terminal actions and cascading steps.

## 3. Strict Safety Invariants

1. **Audits are strictly read-only**: Never modify system state, refresh package repositories, fetch into Git checkouts,
   or execute untrusted configuration during health checks.
2. **Explicit Human Confirmation**: Never run maintenance actions automatically. Every action requires human approval
   (`y`/`Y`) bound to an action fingerprint.
3. **No Secret Leakage**: Never print, log, or persist MCP configuration secrets, tokens, or private credentials. Only
   aggregate counts are allowed.
4. **Durable Action Lifecycle**: Every action attempt must be recorded in history before executing any subprocess.
   Checkpoint failure immediately aborts subsequent steps.
5. **Quality Gate Requirement**: All tests must run with the race detector enabled (`-race`). Before making any commit,
   run `make vet test lint vuln`. Lint must report `0 issues.` and `govulncheck` must report 0 vulnerabilities.
6. **No Machine-Specific Hardcoding**: Never commit personal usernames, private home paths, or live API credentials. Use
   test helpers (`t.TempDir()`, synthetic hosts) for tests.
