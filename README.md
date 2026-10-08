# workstation-doctor

workstation-doctor audits developer tools on a workstation. The tool audits Pi, Herdr, Ghostty, Starship, MCP servers, and agent skills. It compares installed versions against latest versions. It also validates configuration files and configuration versions. It records every audit run and fix action in an embedded SQLite database.

## Redesign status

The CLI and TUI still use the legacy audit checks and audit-history database. Automatic maintenance is blocked during the migration. Both `fix` commands and TUI maintenance requests stop before audit, database access, or subprocess execution.

The new `internal/doctor` registry implements all 21 tool, manager, configuration, package, plugin, and skill checks beside the legacy application. It uses bounded local metadata and HTTP reads. Skill checks parse YAML frontmatter and scan only the user, selected-project, or explicit skill roots. The registry does not execute manager inventory commands, native previews, transient launchers, or maintenance. It is not connected to the CLI or TUI yet.

Version detail depends on supported metadata. Homebrew formula receipts, linked global npm packages, uv receipt entrypoints, and Ghostty XML application metadata provide evidence. Other native layouts and Homebrew casks remain explicit limitations. Upstream releases are separate from manager update decisions. Installation times describe the current receipt revision, not first installation.

The tool uses Go 1.27, `github.com/urfave/cli/v3`, `github.com/grindlemire/go-tui`, and `modernc.org/sqlite`. The SQLite driver is pure Go. You do not need a C compiler to build this project.

The tool writes output to stdout as plain lines. Scripts can pipe this output. The tool writes diagnostic events to stderr with log levels.

## Build and use

If you need a list of make targets, run `make help`.

To build and run the application:

```sh
go build -o workstation-doctor .
./workstation-doctor                     # interactive Go-TUI menu (classic menu when piped)
./workstation-doctor check               # legacy audit and record run
./workstation-doctor manual              # ordered manual steps
./workstation-doctor fix                 # blocked during architecture migration
./workstation-doctor fix --yes           # blocked during architecture migration
./workstation-doctor history             # run history
./workstation-doctor history --run 3     # detail of one run and its actions
./workstation-doctor --db /tmp/x.db check  # custom database path
```

Exit code 0 means all components are current and valid. Exit code 1 means updates are pending or a fix action failed. Exit code 2 means a check returned UNKNOWN or the database failed to open.

## What it audits

The following list describes legacy audit coverage. Some legacy assumptions are migration targets, not the new provider contracts. The new registry uses explicit definitions and bounded scheduling.

Tool version audits:
- Pi, Herdr, Ghostty, Starship, opencode, tokenjuice, serena, and gortex.
- Legacy Homebrew and npm outdated reports. The legacy npm command does not establish explicit global scope.
- Extensions in `~/.pi/agent/npm` and the superpowers repository via `git ls-remote`.
- Herdr integrations and installed Herdr plugins.

Configuration audits:
- Ghostty configuration validity via `ghostty +show-config --changes-only`.
- Ghostty configuration version against deprecated options.
- Herdr configuration validity via `herdr config check`.
- Herdr configuration version against modern section requirements.
- Pi configuration validity for `settings.json`, `mcp-adapter.json` (or `mcp.json`), and `mcp-cache.json`.
- Pi configuration version comparing `lastChangelogVersion` against the installed Pi binary.

Skill audits:
- Required `SKILL.md` frontmatter fields, description length, and the skill-name-to-directory match.

## Network and secrets

The legacy `check` command still records audit history and invokes legacy inspection commands. It does not use the new provider safety contracts. Do not use a live audit to verify migration safety.

The new audit engine does not access SQLite or execute maintenance. Its providers read bounded metadata without initializing manager caches or fetching into inspected Git checkouts. Tests use synthetic files and fake HTTP responses.

The tool never outputs the content of `mcp-adapter.json` or `mcp.json`. The file can contain secrets. Only server counts appear in output.

## History database

The default database location is `~/.local/share/workstation-doctor/doctor.db`. You can override this path with the `--db` flag.

The schema contains three tables:
- `check_runs`: records each audit run, timestamps, summary counts, and exit code.
- `check_results`: records individual component results for each run.
- `actions`: records executed fix commands, timestamps, status, and output.

If the database fails to open, the tool prints a warning to stderr and continues without history.

## Test and verify

Run these commands before you commit changes:

```sh
make vet && make test   # run static checks and tests
make lint               # run linters (must report 0 issues)
make lint-fix           # fix lint issues automatically where supported
make vuln               # scan dependencies for known vulnerabilities
```
