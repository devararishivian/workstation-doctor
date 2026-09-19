# workstation-doctor

workstation-doctor audits a developer workstation. It checks Pi, Herdr, Ghostty, MCP servers, and agent skills. It
compares installed versions against the latest versions. It stores each check and each fix action in the embedded SQLite
database.

The stack is Go 1.27 with `github.com/urfave/cli/v3` and `modernc.org/sqlite`. The SQLite driver is pure Go, so the
build needs no C compiler. There is no GUI and no new runtime dependency.

Logs use `github.com/rs/zerolog`. Program output (tables, steps, history) goes to stdout as plain lines without level
or timestamp, so scripts can pipe it. Diagnostics (warnings, prompts, failures) go to stderr as leveled events.

## Build and use

Use `make help` to list targets. Build the binary once, then run a command:

```sh
go build -o workstation-doctor .
./workstation-doctor                     # interactive menu
./workstation-doctor check               # read-only check and save the run
./workstation-doctor manual              # ordered manual steps
./workstation-doctor fix                 # automatic fix with confirmation
./workstation-doctor fix --yes           # automatic fix without confirmation
./workstation-doctor history             # run history
./workstation-doctor history --run 3     # detail of one run and its actions
./workstation-doctor --db /tmp/x.db check  # custom database
```

Exit code 0 means all components are current. Exit code 1 means an update is pending or a fix action failed. Exit code 2
means a check returned UNKNOWN or the database did not open.

## What it checks

workstation-doctor compares the installed version against the latest version for each tool: Pi, Herdr, Ghostty,
opencode-ai, tokenjuice, serena-agent, and gortex. It also reports collective status from `brew outdated` and
`npm outdated -g`. It checks the packages in `~/.pi/agent/npm` and the superpowers repo with read-only `git ls-remote`.
It reads `herdr integration status`. It validates the Ghostty configuration, the files `settings.json` and `mcp.json`
with the MCP cache, and the skill description limit of 1024 characters.

## Network and secrets

The `check` command does not change the system. It uses the network only to read the latest versions (npm registry, brew
info, PyPI, git ls-remote). It never prints the content of `mcp.json` because the file can contain secrets.

## History database

The default database is `~/.local/share/workstation-doctor/doctor.db`. You can override it with the `--db` flag. The
schema has three tables:

- Table `check_runs` stores one row per run with time, summary, and exit code.
- Table `check_results` stores one result row per component per run.
- Table `actions` stores one row per executed `fix` command with command, status, output, and time.

If the database does not open, the tool still runs without history. It writes a warning to stderr.

## Test and lint

Run these `make` targets before each commit:

```sh
make vet && make test   # static check and tests
make lint               # expect 0 issues (configuration: .golangci.yml)
make lint-fix           # fix automatically what the tool can fix
make vuln               # dependency vulnerability scan
```
