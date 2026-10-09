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

## 4. Release Workflow

When publishing a new release, execute the following steps in sequence:

### Step 1: Run Quality Gates
Before creating a release, run all validation checks on the local `develop` branch:

```sh
make vet test lint vuln
make test-platform EXPECT_OS=darwin   # On macOS
make test-platform EXPECT_OS=linux    # On Linux
```

Make sure that `golangci-lint` reports 0 issues and `govulncheck` reports 0 vulnerabilities.

### Step 2: Prepare Version and Demo Assets
1. Update `var version` in `main.go` to the target Semantic Version (for example, `1.0.2`).
2. If user interface or visual behavior changed, re-record the full interactive demo to `assets/demo.gif`:
   - Use `asciinema` with window size `140x38`.
   - Render to animated GIF using `agg --theme dracula`.
   - Capture all screens: Main Menu, Results Table, Component Detail, Manual Steps, Action Preview, History List, History Detail, and Quit.
3. Commit version and asset updates to `develop`:

```sh
git add main.go assets/demo.gif
git commit -m "chore: bump version to X.Y.Z and update demo assets"
```

### Step 3: Synchronize Branches
Push changes from `develop` and fast-forward merge them into `main`:

```sh
# Push develop branch
git push origin develop

# Switch to main and fast-forward merge
git checkout main
git merge develop --ff-only
git push origin main
```

### Step 4: Tag the Release
Create an annotated Git tag on `main` and push it to GitHub:

```sh
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
```

### Step 5: Publish Release with GoReleaser
Run GoReleaser using your authenticated GitHub CLI token:

```sh
GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
```

GoReleaser compiles multi-platform binaries (`darwin/arm64`, `darwin/amd64`, `linux/arm64`, `linux/amd64`), generates `checksums.txt`, and publishes the release assets directly to GitHub Releases.

Remove the temporary `dist/` directory after release completion:

```sh
python3 -c "import shutil; shutil.rmtree('dist', ignore_errors=True)"
```

### Step 6: Return to Development Branch
Switch back to `develop` and rebuild the local binary:

```sh
git checkout develop
make build
```

Verify that `git status` reports a clean working tree.

