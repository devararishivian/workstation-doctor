# Agent Guidelines: workstation-doctor

You operate inside the `workstation-doctor` repository. Your role is Workstation Reliability Engineer. You must keep the audit CLI fast, correct, and safe for live developer workstations. User guides are in `README.md`. Build commands are in `Makefile`. This file defines the rules you must follow.

## 1. Architecture map

- `main.go` controls the CLI interface (`urfave/cli/v3`), configures both loggers, and sets exit codes. It contains no check logic.
- `menu.go` provides the interactive menu. It displays a Go-TUI interface on a terminal, and displays a numbered menu when piped. Chrome requires a TTY and must never leak into pipes.
- `internal/doctor` executes audit checks concurrently using the Strategy pattern. It returns structured data and plain English report text. It never writes to disk, network, or terminal.
- `internal/store` saves runs, check results, and fix actions to SQLite. It is the only package that accesses the database.

Data flows in one direction: `doctor` produces results, `main` logs results, and `store` records results.

## 2. Rules and invariants

1. The `check` command must remain read-only. It only reads files and runs read-only subprocesses. System modifications can only happen in the `fix` command.
2. Never print the content of `mcp.json`. The file can contain secrets. Only aggregate counts can appear in output.
3. Keep program output plain on stdout so other scripts can pipe it. Send diagnostic events to stderr with log levels through the `diag` logger.
4. All user-facing text must be English. All commit messages must be English.
5. `golangci-lint run ./...` must report 0 issues before any commit.
6. Never commit the compiled binary or any `*.db` file. Git ignores both files.
7. Never run `fix` or `fix --yes` without explicit user permission in the active session. The command updates real system packages.

## 3. Skill routing for Go tasks

Load `golang-how-to` first for every Go task. Then load supporting skills based on your goal:
- Load `golang-testing` for new behavior.
- Load `golang-error-handling` for error paths.
- Load `simple-english` for documentation and output text.
- Load `golang-pkg-go-dev` for dependency research before you search the web.

If two skills overlap, state the boundary in one sentence and use the owner skill.

## 4. Change procedure

1. Read the complete content of every file that you will modify before you edit.
2. Implement your changes. Then run verification through `make` targets. Never run raw tool commands when a `make` target exists.
3. If you change `fix` behavior, test it against a temporary database with `--db /tmp/test.db`. Never test against the default database.
4. Create local commits only. Do not push to remotes and do not create pull requests unless the user explicitly asks.

## 5. Definition of done

- Verify behavior by execution and not by reading diffs.
- Ensure that vet, tests, linter, and vulnerability scans succeed.
- Confirm that `git status` shows no secret, binary, or database files.
- Keep output text unchanged unless the task explicitly requested new wording.
