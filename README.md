# workstation-doctor

workstation-doctor audits developer tools on a workstation and coordinates safe maintenance through a single interactive terminal dashboard (TUI). It inspects Pi, Herdr, Ghostty, Starship, opencode, tokenjuice, serena, gortex, Homebrew, global npm, configuration files, plugin registries, declared packages, and agent skills across 21 registered checks.

## Architecture & Design

The application is organized around explicit responsibility boundaries:

- **Root CLI & TUI (`main.go`, `menu*.go`)**: Configures launch options and runs a single interactive TUI via Go-TUI. Piped or non-TTY execution exits cleanly with `An interactive terminal is required.` No separate command-line subcommands exist.
- **Application Service (`internal/app`)**: Orchestrates read-only audits, target inspections, action preparation, kernel advisory locking (`Flock`), user confirmation binding, durable step execution in isolated process groups, registered verification, and release notes retrieval.
- **Health Engine & Registry (`internal/doctor`)**: Registers and schedules 21 modular health checks. Implements bounded host inspection (HTTP, file read, subprocess) without modifying system state, starting MCP servers, or evaluating untrusted configuration.
- **Action History Storage (`internal/store`)**: Action-only SQLite persistence storing confirmed maintenance attempts, ordered step checkpoints, execution and verification outcomes, and migration markers. Audit runs are never stored as historical snapshots.

## Build and Use

To build the executable:

```sh
make build
```

To launch the interactive dashboard:

```sh
./workstation-doctor
```

### Launch Flags

- `--db PATH`: Explicit absolute path for the action history database (defaults to platform application support / state directory).
- `--project PATH`: Scoped directory path for project-level tool and skill inspection.
- `--location INTEGRATION=PATH`: Explicit configuration or instance location override (repeatable).
- `--skill-root PATH`: Additional directory root for scanning Agent Skills (repeatable).
- `--history-days N`: Retention age limit in days for terminal actions (default: 90).
- `--history-limit N`: Maximum terminal actions retained (default: 10,000).
- `--history-step-bytes N`: Maximum safe output captured per step (default: 16 KiB).
- `--history-action-bytes N`: Maximum safe output captured across an action (default: 64 KiB).
- `--history-metadata-bytes N`: Maximum metadata payload per action (default: 8 KiB).

To run platform-specific tests:

```sh
make test-platform EXPECT_OS=darwin   # On macOS
make test-platform EXPECT_OS=linux    # On Linux
```

## Inspected Checks (21 Built-in Checks)

1. **pi**: CLI binary discovery, npm provenance, and version checks.
2. **herdr**: Binary discovery, GitHub release comparison, and provenance.
3. **ghostty**: Documented macOS/Linux binary discovery and version checks.
4. **starship**: Binary discovery, release comparison, and provenance.
5. **opencode**: Global npm package inspection and version checks.
6. **tokenjuice**: Global npm package inspection and version checks.
7. **serena**: uv tool receipt inspection, PyPI metadata, and launcher declarations.
8. **gortex**: Binary discovery, release comparison, and update proposals.
9. **ghostty-config-valid**: Bounded static syntax verification of configuration.
10. **ghostty-config-version**: Inspection for obsolete or deprecated configuration directives.
11. **herdr-config-valid**: Syntax and schema verification for Herdr configuration.
12. **herdr-config-version**: Schema version and section completeness verification.
13. **pi-config-valid**: Syntax inspection of `settings.json`, `mcp-adapter.json`, and cache without exposing credentials.
14. **pi-config-version**: Synchronization check between `lastChangelogVersion` and installed binary.
15. **brew-outdated**: Homebrew formula receipt inspection without repository refresh.
16. **npm-outdated-g**: Global npm package receipt inspection and pin evaluation.
17. **pi-packages**: Declared npm, Git, and local extensions in user and project scope.
18. **superpowers**: Declared superpowers harness repository state, branch, and ref advancement intent.
19. **herdr-integr**: Verification of agent hooks and integration state.
20. **herdr-plugins**: Plugin declarations in user registry and remote commit references.
21. **skills**: Agent Skills frontmatter (`SKILL.md`) validation, name matching, and length bounds.

## Safe Maintenance Lifecycle

Maintenance actions are never executed automatically without human consent:
1. **Prepare**: Targets are inspected freshly, proposal bounds verified, and an action fingerprint computed over frozen scope and steps.
2. **Preview**: Safe presentation hides runnable raw commands while showing reasons, targets, steps, side effects, and verification expectations.
3. **Confirm**: Explicit user affirmative interaction generates a single-use token bound to the action fingerprint. Any other interaction declines without records.
4. **Exclusive Ownership**: A kernel advisory lock (`Flock`) guarantees that only one maintenance process runs per user state directory. Leftover markers from unclean crashes trigger fail-closed protection.
5. **Durable Start**: The attempt is recorded in history before executing any subprocess.
6. **Execution**: Ordered steps run direct argv within process groups. Checkpoint failure immediately halts subsequent steps.
7. **Verification**: Post-action verification checks run through the registered check algorithm, separating execution outcome (`Completed`/`Failed`/`Canceled`) from verification outcome (`Passed`/`Failed`/`Unknown`).
