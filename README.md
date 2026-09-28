# workstation-doctor

workstation-doctor audits developer tools on a workstation. The tool audits Pi, Herdr, Ghostty, Starship, MCP servers, and agent skills. It compares installed versions against latest versions. It also validates configuration files and configuration versions. It records every audit run and fix action in an embedded SQLite database.

The tool uses Go 1.27, `github.com/urfave/cli/v3`, `github.com/grindlemire/go-tui`, and `modernc.org/sqlite`. The SQLite driver is pure Go. You do not need a C compiler to build this project.

The tool writes output to stdout as plain lines. Scripts can pipe this output. The tool writes diagnostic events to stderr with log levels.

## Build and use

If you need a list of make targets, run `make help`.

To build and run the application:

```sh
go build -o workstation-doctor .
./workstation-doctor                     # interactive Go-TUI menu (classic menu when piped)
./workstation-doctor check               # read-only check and record run
./workstation-doctor manual              # ordered manual steps
./workstation-doctor fix                 # automatic fix with confirmation
./workstation-doctor fix --yes           # automatic fix without confirmation
./workstation-doctor history             # run history
./workstation-doctor history --run 3     # detail of one run and its actions
./workstation-doctor --db /tmp/x.db check  # custom database path
```

Exit code 0 means all components are current and valid. Exit code 1 means updates are pending or a fix action failed. Exit code 2 means a check returned UNKNOWN or the database failed to open.

## What it audits

The tool runs all checks concurrently for fast execution. It organizes checks through the Strategy pattern.

Tool version audits:
- Pi, Herdr, Ghostty, Starship, opencode, tokenjuice, serena, and gortex.
- Outdated packages from `brew outdated` and `npm outdated -g`.
- Extensions in `~/.pi/agent/npm` and the superpowers repository via `git ls-remote`.
- Herdr integrations and installed Herdr plugins.

Configuration audits:
- Ghostty configuration validity via `ghostty +show-config --changes-only`.
- Ghostty configuration version against deprecated options.
- Herdr configuration validity via `herdr config check`.
- Herdr configuration version against modern section requirements.
- Pi configuration validity for `settings.json`, `mcp.json`, and `mcp-cache.json`.
- Pi configuration version comparing `lastChangelogVersion` against the installed Pi binary.

Skill audits:
- Skill description length under the 1024 character limit.

## Network and secrets

The `check` command is read-only. It changes no files and installs no packages. It uses the network only to read version metadata from package registries and git repositories.

The tool never outputs the content of `mcp.json`. The file can contain secrets. Only server counts appear in output.

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
