# Agent Guidelines: workstation-doctor

Your role is Workstation Reliability Engineer. Keep inspection fast, correct, and safe on live developer workstations.
User guides are in `README.md`. Verification commands are in `Makefile`. All documentation and application text must be
English.

## 1. Authority and work boundaries

The approved architecture design is documented in
[architecture spec](docs/superpowers/specs/2026-10-07-workstation-doctor-architecture-redesign-design.md).
The implementation sequence is defined in
[redesign roadmap](docs/superpowers/plans/2026-10-07-workstation-doctor-redesign-roadmap.md).

Work directly on the local git branch. Pushes, subagent delegation, and remote pull requests require operator authorization.
Read relevant spec and implementation files before making changes.

## 2. Architecture and code map

The workstation-doctor redesign is complete and fully integrated:

- `main.go`: Application entrypoint configuring launch flags via `urfave/cli/v3`. Enforces interactive TTY requirement (`An interactive terminal is required.` on stderr for non-TTY).
- `menu*.go`: Go-TUI dashboard interface:
  - `menu.go`: Key listener, watchers, and TUI runner.
  - `menu_state.go`: Application UI state, generation guarding against late async callbacks, and action lifecycle.
  - `menu_views.go`: Generic view rendering for menus, audit results, details, action previews, and migration.
  - `menu_details.go`: Structured detail models (`Detail`, `DetailSection`, `DetailRow`) for integrations, instances, findings, action previews, and action history records.
- `internal/app`: Application orchestration service:
  - `service.go`: Coordinates read-only audits, target inspections, action preparation, confirmation binding, execution, and history queries.
  - `ownership.go` / `ownership_unix.go`: Kernel advisory locking (`Flock`) under user `StateDir` with crash/orphan detection.
  - `preview.go`: Immutable frozen action proposals and single-use fingerprint-bound approvals.
  - `executor.go` / `executor_unix.go`: Direct-argv step execution inside isolated process groups with process termination on cancel.
  - `migration.go`: Legacy history migration wrappers with explicit consent and ownership guards.
  - `release_notes.go`: Bounded HTTPS retrieval and ANSI escape sanitization for public release notes.
  - `paths.go`: Platform-specific database and state directory resolution (macOS Application Support / Linux XDG state).
- `internal/doctor`: Modular health inspection engine:
  - `builtin.go`: Explicit registration of all 21 health checks.
  - `audit_engine.go`: Bounded concurrent scheduling (MaxConcurrency: 8) with cancellation and fresh inspection.
  - `model.go`, `registry.go`, `identity.go`, `actions.go`, `limits.go`: Domain model, proposal validation, and limit definitions.
  - `host.go`: Bounded read-only command execution, HTTPS metadata fetching, and file reads.
  - `discovery.go`, `inventory*.go`, `tool_instances.go`, `tool_native.go`, `manager_instances.go`: Local receipt and instance discovery.
  - `skills.go`: Agent Skills frontmatter parsing, validation, and directory matching.
- `internal/store`: Action-only SQLite persistence:
  - `history.go`: Connection pooling, pragmas, atomic start and step checkpointing, and finish recording.
  - `history_model.go`: Domain models for action attempts, steps, and query pagination.
  - `history_queries.go`: Filtered, indexed, paginated action history queries.
  - `history_schema.go`: Schema versions (v1, v2, v3) and transactional migrations.
  - `retention.go`: Retention enforcement (age, count) deleting terminal actions and cascading steps.
  - `migration.go`: Non-destructive online SQLite backup snapshot and safe action migration from legacy databases.

## 3. Safety invariants

1. Audits, discovery, and current detail reads remain strictly read-only. Never modify system state, refresh package repositories, fetch into Git checkouts, or execute configuration.
2. Never run maintenance actions without explicit affirmative confirmation in the active session.
3. Never print or persist MCP configuration contents. Only aggregate server counts may be exposed.
4. Execution must never proceed if durable start recording fails. Checkpoint failure immediately aborts later steps.
5. All tests must be executed with the race detector enabled (`-race`). Before any commit, run `make vet test lint vuln`; lint must report `0 issues.`
