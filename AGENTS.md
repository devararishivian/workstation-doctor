# Agent Guidelines: workstation-doctor

Your role is Workstation Reliability Engineer. Keep inspection fast, correct, and safe on live developer workstations.
User guides are in `README.md`. Verification commands are in `Makefile`. All documentation and application text must be
English.

## 1. Authority and work boundaries

The approved target design is the
[architecture spec](docs/superpowers/specs/2026-10-07-workstation-doctor-architecture-redesign-design.md).
The implementation sequence is in the
[redesign roadmap](docs/superpowers/plans/2026-10-07-workstation-doctor-redesign-roadmap.md).

Spec approval permits planning. It does not permit automatic implementation. Before product changes, obtain plan
approval and the user's execution choice. Do not delegate to subagents unless the user authorizes delegation.

Read the relevant spec, plan, and source files completely before changing them. Keep structural moves separate from
behavior changes and commit them separately. Stop for review if a stage changes its approved scope or shared contracts.

## 2. Current implementation versus approved target

The redesign is in progress. Legacy behavior remains active while characterization and the doctor file split have landed.
Do not describe unimplemented packages or target behavior as already present.

Current code map:

- `main.go` owns `urfave/cli/v3` commands, process logging, audit recording, and the CLI fix flow.
- `menu.go` owns Go-TUI rendering, the numbered non-TTY menu, state, and a separate fix flow.
- `internal/doctor/doctor.go` is the package overview. Integration checks live in focused files in the same package.
- `internal/doctor/legacy_engine.go`, `legacy_model.go`, and `legacy_inspect.go` retain the old contracts and execution.
- `internal/doctor/report.go` retains legacy report formatting.
- `internal/store/store.go` owns SQLite access for audit runs, per-check results, and actions.

Current audit entry points also record runs. Current maintenance executes shell strings through two paths. These are
migration targets, not patterns to extend. Do not use live workstation commands to exercise them during development.

Approved target boundaries:

- Root `main.go` constructs dependencies and starts one TUI. Non-TTY operation fails plainly without terminal controls.
- The TUI renders generic data, collects input, and calls application use cases. It does not execute shell commands or
  SQL.
- `internal/app` coordinates audits, action previews, confirmed execution, post-action inspection, and history.
- `internal/doctor` discovers instances and returns observations, findings, and proposals. It never prints, persists,
  prompts, or applies maintenance.
- `internal/store` is the only package that accesses SQLite. It stores maintenance attempts and steps, not audit
  snapshots.

Keep checks in focused files within the same package first. Do not create one package per checker. Update this code map
as stages actually land. Keep `README.md` accurate for the current stage rather than claiming future behavior works.

## 3. Safety invariants

1. Audits, discovery, and current detail reads remain read-only. Do not refresh manager repositories, fetch into inspected
   Git checkouts, install packages, create resource caches, rewrite configuration, or start MCP servers.
2. Never run real `fix`, `fix --yes`, or any replacement maintenance action without explicit permission in the active
   session. A spec, plan, or test approval is not permission to update workstation packages.
3. Never print or persist MCP configuration contents. For `mcp-adapter.json`, `mcp.json`, and related MCP configuration,
   expose aggregate counts only. Do not expose server names, commands, URLs, headers, credentials, or environment
   values.
4. Do not assume a command is read-only because of its name. Establish behavior for supported versions. If safety is
   unresolved, report the limitation rather than executing it.
5. Do not load extensions, source shell profiles, execute configuration expressions, or invoke transient `npx` or `uvx`
   launchers during discovery. Read declarations as data.
6. Automatic maintenance requires explicit confirmation, exclusive ownership, durable start recording, and fresh
   preconditions. If start recording fails, execution must not start. Never rerun an action to repair its history.
7. Execute structured paths and arguments, not displayed command strings. Do not add shell parsing as an extension API.
8. Bound files, traversal, network responses, subprocess output, dynamic concurrency, plans, and persisted detail.
   Preserve completed work and report incomplete coverage or cancellation without an all-clear result.
9. Keep diagnostics on stderr with log levels. Legacy stdout reports stay plain until removal. Non-TTY errors never
   emit TUI control sequences. Sanitize external text before terminal display.
10. Never commit secrets, compiled binaries, database files, SQLite sidecars, or real workstation configuration
    fixtures.

## 4. Discovery, extension, and history rules

Register supported implementations explicitly. Discover installed instances dynamically. Do not add reflection,
`init()` registration, a runtime plugin loader, a DI container, or an operating-system framework.

A new check using existing capabilities needs its implementation, one registry entry, and tests. The engine, store,
formatter, and TUI must not require an integration-name switch. New execution capabilities need separate review.

Use documented defaults and integration-specific override precedence. A binary location does not identify its updater.
An absent optional integration is not an application failure. Unknown provenance must disable guessed automatic updates.
macOS is primary and Linux is supported. Keep real OS differences local. Do not substitute this machine's paths for
portable discovery or replace a product's directory convention with Workstation Doctor's storage convention.

Show installation time only with reliable evidence and its meaning. File timestamps, commit dates, and first observation
are not installation dates. Show unavailable when evidence is absent.

Current details come from in-memory observations. Persist maintenance attempts only, with relevant context and separate
execution and verification outcomes. Do not create check-run tables or a first-seen inventory to support details.
Preserve legacy databases through explicit migration. Never silently delete, overwrite, migrate, compact, or prune a real
database during an audit.

## 5. Go skill routing

Load `golang-how-to` first for every Go task. Load supporting skills for the task:

- `golang-testing` for new behavior and test planning.
- `golang-error-handling` for failures and cancellation paths.
- `golang-security` for filesystem, subprocess, network, and secret boundaries.
- `golang-refactoring` for structural changes and staged migrations.
- `golang-pkg-go-dev` for dependency research before web search.
- `simple-english` for documentation and user-facing text.

Use the Go design-pattern knowledge base when evaluating patterns, not as a mandate to add them. If skills overlap,
state the ownership boundary in one sentence. If an optional skill or navigation tool is unavailable, state the
limitation and continue with verified repository evidence. Do not install tooling or alter project tracking implicitly.

## 6. Verification and commit procedure

Use existing `make` targets instead of raw verification commands:

```sh
make vet test lint vuln
```

Tests include race detection. `make lint` must report `0 issues.` before every commit. Do not claim a command passed
without fresh execution evidence. For product changes, verify behavior by execution, not only by reading diffs.

Use fake commands and HTTP responses, synthetic fixtures, and temporary databases for action tests. A temporary database
alone does not make real maintenance safe. Never use the default history database or a real updater in test
verification.

If targeted or platform tests need a missing target, add a `make` target in the implementing stage. Native Linux tests
are required for Linux runtime claims. Cross-compilation alone is not enough. Honor the toolchain declared in `go.mod`.

Inspect `git status` and the staged diff before committing. Stage exact task files, use English commit messages, and
create local commits only. Do not push or create pull requests without explicit authorization. Do not run broad cleanup
commands that can remove databases or unrelated user work.
