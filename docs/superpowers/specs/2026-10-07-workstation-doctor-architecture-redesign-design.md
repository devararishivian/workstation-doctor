# Workstation Doctor Architecture Redesign

## 1. Status and purpose

Status: Approved by the user.

This document replaces the earlier draft at this path. The user approved the written specification and authorized its
commit, an `AGENTS.md` revision, and sequential implementation plans. Implementation requires separate plan review and
an explicitly selected execution method.

Workstation Doctor helps developers inspect their workstation and apply selected maintenance actions through one TUI.
The application must work across different installations and configurations. It must not assume that every workstation
matches its original developer's environment.

macOS is the primary platform. Linux is also supported. Platform-specific behavior must remain close to the integrations
that need it.

The design serves developers and future open-source contributors. Its goals are safe inspection, understandable
findings, and low-cost checker extension. All documentation and application text remain English.

### Review status

The user endorsed the purpose, lightweight boundaries, explicit implementation registration, and dynamic workstation
discovery. The user also endorsed the distinction between an integration, an instance, and a check.

The user asked the author to choose the history policy and approved maintenance-attempt history only. The written-spec
approval also covers readable details, evidence-based installation timestamps, history limits, and explicit migration.

## 2. Scope

The redesign preserves the coverage of all 21 existing checks. It does not preserve assumptions that official
documentation contradicts.

The application must support these behaviors:

- Discover applicable installations and resource scopes at runtime.
- Inspect availability, versions, configuration, packages, integrations, plugins, and skills.
- Explain findings, update opportunities, and available actions in detail.
- Show integration and instance metadata with its source and observation time.
- Apply selected maintenance actions only after explicit confirmation.
- Retain bounded history that explains maintenance attempts and outcomes.

The following remain outside scope:

- Installation, packaging, deployment, and distribution of Workstation Doctor.
- A second CLI user interface or a numbered non-TTY menu.
- Runtime loading of third-party checker implementations.
- Continuous filesystem monitoring or a background discovery daemon.
- Automatic installation of absent tools.
- A universal package-management framework.
- TUI visual redesign or replacement of Go-TUI.
- Persistent workstation inventories, audit snapshots, event sourcing, and automatic rollback.

## 3. Repository evidence and invalidated assumptions

### Current architecture

The discovery baseline was branch `develop` at commit `0273123`. The repository has one Go module and one executable,
with root `main.go` as its entry point.

| Location                    | Observed responsibility                                                                                                  |
|-----------------------------|--------------------------------------------------------------------------------------------------------------------------|
| `main.go`                   | CLI setup, logging, command orchestration, history access, and CLI fix execution.                                        |
| `menu.go`                   | TUI, numbered non-TTY fallback, asynchronous state, history, and separate fix execution.                                 |
| `internal/doctor/doctor.go` | Checker contracts, fixed registration, concurrent execution, checks, and formatting. The inspected file has 1,233 lines. |
| `internal/store/store.go`   | SQLite schema and access for audit runs, results, and actions.                                                           |

`Checker` currently exposes `Name()`, `Category()`, and `Check(context.Context) Result`. `NewEngine()` registers 21
checks. The engine starts a goroutine per checker and preserves order through indexed results. Runtime execution does
not consume `Name()` or `Category()`.

`Result` combines findings with `Manual` and `Fix` command strings. The CLI and TUI execute fixes through `/bin/sh -c`
using separate flows. The TUI ignores action-recording errors.

The current store links actions to check runs. This dependency does not fit action-only history. The redesign removes
the need for a persisted audit before an action can exist.

During earlier discovery, `make vet`, `make test`, `make lint`, and `make vuln` passed. Those results describe the
existing implementation, not the proposed redesign.

### Corrections established by official sources

Executable presence does not identify the updater. Pi, Herdr, Starship, OpenCode, and Tokenjuice document installation
methods beyond the manager assumed by their current checks. Finding `opencode` in `PATH` does not prove that the active
installation belongs to npm.

Configuration locations belong to each integration. Pi supports an agent-directory override. Herdr supports its own
overrides and XDG locations. Ghostty supports XDG and macOS-specific locations, with version-dependent configuration
filenames.

Optional configuration is not a missing dependency. Ghostty can use defaults without a user configuration file. Herdr
source also treats missing configuration as defaults. The application must distinguish optional absence from an
unreadable or invalid existing file.

Pi uses `lastChangelogVersion` for changelog presentation. It is not a configuration-schema version. Herdr's
missing-section heuristic also does not establish that its configuration is obsolete.

A command that only displays or validates configuration is not a repair action. Existing findings that propose such
commands as fixes need deliberate correction.

The existing global npm check calls `npm outdated --json` without global scope. npm documents local scope as the
default. Pi packages can use npm, Git, or local sources, so one assumed npm directory cannot represent every resource.

Different Git commits do not prove that a checkout is safely updatable. Requested refs, pins, dirty files, divergence,
and detached checkouts affect the answer.

### Decisions retained or revised

| Previous decision                          | Revised decision                                                                                       |
|--------------------------------------------|--------------------------------------------------------------------------------------------------------|
| One executable and one TUI                 | Retain.                                                                                                |
| Keep root `main.go`                        | Retain. Moving it does not solve the identified problems.                                              |
| Split `doctor.go` into focused files first | Retain. Avoid one package per checker.                                                                 |
| Small checker contract                     | Retain, but support discovered instances and multiple findings.                                        |
| Explicit built-in registration             | Retain for supported implementations, not installed workstation contents.                              |
| Deterministic result order                 | Retain, with ordering rules for discovered instances.                                                  |
| Small application service                  | Retain for audit and action orchestration.                                                             |
| One confirmed action-execution path        | Retain.                                                                                                |
| Fixed paths and manager assumptions        | Replace with integration-specific discovery.                                                           |
| Missing tools reported as `UNKNOWN`        | Replace with explicit availability states.                                                             |
| Store audit runs, results, and actions     | Replace with maintenance-attempt history only.                                                         |
| Introduce structured actions later         | Include them in this redesign. History and updater selection depend on them.                           |
| Preserve existing behavior                 | Apply to structural-only stages. Discovery, status, and storage changes are explicit behavior changes. |

The previous non-goal of avoiding database changes no longer applies.

## 4. Approach and trade-offs

Three approaches were considered:

| Approach                                                           | Benefit                                     | Cost                                                    |
|--------------------------------------------------------------------|---------------------------------------------|---------------------------------------------------------|
| Keep independent checks with discovery inside each                 | Small initial change.                       | Repeated discovery and inconsistent instance selection. |
| Use explicit integrations with shared discovery and focused checks | Consistent evidence and reusable discovery. | Requires a small shared model.                          |
| Build a generic capability and plugin framework                    | Broad extensibility.                        | Adds machinery without a demonstrated requirement.      |

Use the second approach. Discovery must be dynamic. Implementation loading does not need to be dynamic.

The program explicitly registers what it knows how to inspect. At runtime, those implementations discover what exists. A
registry entry does not assert that a tool is installed.

## 5. Core model

Separate three concepts:

| Concept     | Meaning                                      | Example                                     |
|-------------|----------------------------------------------|---------------------------------------------|
| Integration | A supported product or resource family.      | Pi.                                         |
| Instance    | A discovered installation or resource scope. | Pi resolved from the active `PATH`.         |
| Check       | A question about that instance.              | Is an update available through its manager? |

An integration can have no discovered instances. An instance can produce several findings. Skills and plugin registries
can have directory or registry instances without standalone executables.

### Integration descriptors

An integration descriptor contains stable identity, display name, description, supported scope, and official references.
It identifies the discovery and inspection behavior supplied by the built-in implementation.

Keep static metadata in one authoritative location. Finding and instance data reference that identity rather than
redefining it. Category metadata is optional and remains only if the TUI uses it for grouping or filtering.

### Instances and evidence

An instance carries the information its checks and detail view need:

- Integration identity and instance identity.
- Discovery source, scope, and observation time.
- Executable path, resolved target, installation root, or resource location when known.
- Installed version or revision and its evidence source.
- Installation manager, package identity, and channel when established.
- Relevant configuration locations and supported capabilities.
- Installation-time evidence when available.
- Safe discovery diagnostics and unresolved facts.

Installation provenance means evidence of who manages an installation. Unknown provenance is valid data, not a reason to
guess a manager.

Evidence records the source and time of an observation. A fact can be known, unavailable, unsupported, or failed to
read. Optional values must not default to a plausible but unobserved value.

### Findings and identity

Identify findings by integration, check, and instance. A display label is not an identifier. Identity must distinguish
two installations of the same product within the selected scope.

Identity does not need to survive an installation moving. Paths require normalization where used for identity. Do not
use arbitrary subprocess output as an identifier.

A finding contains its outcome, explanation, relevant observations, and action proposals. A checker can return several
findings, such as one per plugin. Findings remain in memory and do not become database rows.

## 6. Discovery policy

### Sources and precedence

Each integration defines documented precedence among explicit locations, environment overrides, read-only product
commands, manager metadata, and documented defaults.

Defaults are allowed. Undocumented fixed assumptions are not. An invalid explicit override must produce a diagnostic
rather than a silent fallback that inspects another location.

Honor product-specific semantics. XDG paths require the rules defined by XDG. Product overrides can have different rules
and must follow the product's documentation.

Do not recursively search the whole home directory or disk. Do not source shell startup files to reconstruct another
environment.

### Scope and multiple installations

The default scope is the current user and active process environment. The executable resolved through `PATH` is the
primary command-line instance. Additional candidates can come from documented application locations or manager
inventory.

When several candidates exist, identify the active instance and disclose the others. Deduplicate aliases to the same
installation when evidence establishes equivalence. Do not merge distinct installations merely because their versions
match.

A discovered binary outside `PATH` can be listed as present but inactive. A location that cannot be inspected is
undetermined, not absent. Do not claim complete machine-wide discovery.

Project inspection requires an explicit selected project scope. Do not silently traverse unrelated repositories. Report
which scope supplied each relevant observation.

### Safe configuration discovery

Read configuration as data. Do not load extensions, run configuration expressions, source shell files, or start MCP
servers.

Do not execute declared transient launchers such as `uvx` or `npx` during discovery. They can download or create
environments. A configured launch source can be identified without proving that its runtime is installed or runnable.

A default configuration path is a candidate, not proof of effective configuration. When runtime overrides cannot be
reconstructed, label the path as discovered or declared rather than active.

### Manager ownership

Provenance must connect the selected instance to its manager and scope. The presence of `brew`, `npm`, or `uv` does not
establish ownership.

When provenance remains unresolved, continue applicable inspection and explain the limitation. Disable automatic update
proposals that require guessed ownership. Upstream release information can still appear, clearly separate from manager
availability.

### Reuse and freshness

Reuse discovery and manager inventory within one audit. This is an in-memory observation context, not a persistent
inventory.

Every detail view shows when its data was observed. Refresh relevant discovery before executing an action. An
observation from another audit must not silently replace an in-progress action's captured context.

## 7. Availability and check outcomes

Availability and check outcome are separate:

| Availability   | Meaning                                                   |
|----------------|-----------------------------------------------------------|
| `Present`      | Discovery found the required instance.                    |
| `Absent`       | Applicable discovery completed and found no instance.     |
| `Unsupported`  | The platform or capability is outside supported behavior. |
| `Undetermined` | Discovery failed or evidence is insufficient.             |

The check outcomes are:

| Outcome         | Meaning                                          |
|-----------------|--------------------------------------------------|
| `OK`            | The check found no issue.                        |
| `Attention`     | The check found a problem or update opportunity. |
| `Unknown`       | The check could not establish its answer.        |
| `NotApplicable` | The prerequisite or capability does not apply.   |
| `Canceled`      | The operation was canceled.                      |

An absent optional integration does not make the workstation unhealthy. An offline lookup does not invalidate an
installed version already discovered. Missing optional configuration means defaults apply, while an unreadable existing
file produces a diagnostic.

Summaries must distinguish attention, uncertainty, absence, and cancellation. Canceled or undetermined work must not
contribute to an all-clear result.

## 8. Readable detail views

Detail views are part of the existing TUI, not a new interface. Their contracts use generic data, so adding a checker
does not require product-specific rendering.

The application retains current observations in memory. Reading details does not require audit snapshots in SQLite.
After restart, users refresh discovery to read current workstation details.

### Integration details

Show the integration's purpose, official references, supported checks, and discovered instances. Also show availability,
selected scope, and the limits of discovery.

An absent integration still has a useful detail view. Explain what was inspected and why checks are not applicable. Do
not turn that view into an automatic installation offer.

### Instance details

Show these fields when evidence exists:

| Field                                  | Required interpretation                                               |
|----------------------------------------|-----------------------------------------------------------------------|
| Executable location                    | Distinguish the resolved command from its symlink target.             |
| Installation or resource root          | Label observed, declared, or manager-reported locations.              |
| Installed version or revision          | Include the source and observation time.                              |
| Manager, package identity, and channel | Show unknown when ownership is not established.                       |
| Active status                          | Explain whether this instance is selected by the active environment.  |
| Configuration locations                | Distinguish candidate, declared, and established effective locations. |
| Installation time                      | Include source, meaning, and precision, or show unavailable.          |
| Related checks and actions             | Link to their findings, previews, and relevant history.               |

Installation time must not be inferred from file modification time, creation time, or Git commit dates. These timestamps
do not reliably establish installation.

A manager receipt can describe installation of the current package revision rather than the product's first
installation. Label it accordingly. Homebrew source provides receipt timestamp evidence, but availability and meaning
still require manager-specific evaluation.

History records when Workstation Doctor executed an update. That timestamp is not the original installation date. A
failed update record also does not prove installation of its target version.

If no reliable source exists, show `Installation time unavailable` with a short reason. Do not add a first-seen
inventory database to manufacture this value.

### Finding and update details

Show the check question, outcome, evidence, explanation, and limitations. For update findings, show installed and
candidate versions or revisions, selected source, channel, pins, and affected scope.

Distinguish an upstream release from a version available through the detected manager. Do not classify arbitrary unequal
version strings as an available update. Use source-appropriate ordering or the manager's update decision.

Provide an official release or changelog link when established. Release-note retrieval is optional, read-only, bounded,
and cancelable. Failure to retrieve notes must not remove known update evidence. Do not render untrusted terminal
controls or execute linked content.

Full package inventories or unbounded release notes are not required. Show relevant targets and summarize counts when a
collection is large.

### Available actions

Show automatic maintenance, manual guidance, and read-only inspection as distinct capabilities. Explain why an automatic
action is unavailable, such as unresolved provenance, a pin, or unsupported behavior.

An action preview shows its purpose, target instance, steps, affected scope, prerequisites, and side effects. It also
explains verification limits and whether manual interaction is required.

Viewing, copying, or opening guidance does not prove that the action ran. The TUI must never present a diagnostic
command as a completed repair.

### History details

A history detail view shows the context captured when the action ran, ordered steps, safe errors, execution outcome, and
verification outcome. It must remain readable when the target is no longer installed.

Historical context and current observations are separate. A record can link to a current instance only when identity
matches. Historical detail is not proof of the workstation's current state.

## 9. Preservation of existing check coverage

| Existing check IDs                               | Revised discovery and evaluation                                                                                                                                             |
|--------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `pi`                                             | Discover the active executable, agent directory, installation source, and supported update method. Do not always select npm.                                                 |
| `herdr`                                          | Discover the executable and provenance. Use update information appropriate to its actual channel.                                                                            |
| `ghostty`                                        | Discover `PATH` and documented macOS application candidates. Use applicable Linux discovery. Separate executable discovery from updater ownership.                           |
| `starship`                                       | Discover the executable and installation source. Homebrew is one possible source.                                                                                            |
| `opencode`                                       | Preserve version and update coverage beyond npm-managed installations.                                                                                                       |
| `tokenjuice`                                     | Support evidenced npm and Homebrew installations. Handle other sources explicitly without guessing an updater.                                                               |
| `serena`                                         | Discover persistent installations and explicitly configured launch sources. Do not infer absence solely from a missing binary or launch transient environments during audit. |
| `gortex`                                         | Discover the executable and version-supported inspection and update capabilities. Treat plan-mode safety as version-dependent.                                               |
| `ghostty-config-valid`, `ghostty-config-version` | Use locations and diagnostics appropriate to the installed version. Optional missing files are valid. Deprecation does not imply an automatic repair exists.                 |
| `herdr-config-valid`, `herdr-config-version`     | Follow Herdr's configuration precedence. Use supported diagnostics and compatibility evidence rather than mandatory-section guesses.                                         |
| `pi-config-valid`, `pi-config-version`           | Inspect applicable Pi and adapter formats. Optional MCP files and caches are not mandatory. Changelog state is not schema compatibility.                                     |
| `brew-outdated`                                  | Discover Homebrew and inventory. Inspect outdated packages without refreshing Homebrew during audit. Disclose freshness limits.                                              |
| `npm-outdated-g`                                 | Discover the active npm prefix and global inventory. Inspect global scope explicitly.                                                                                        |
| `pi-packages`                                    | Discover declared npm, Git, and local sources in the selected scope. Respect pins and package ownership.                                                                     |
| `superpowers`                                    | Discover supported harness declarations, resource roots, or explicit locations. Do not assume a Pi-managed directory or branch `main`.                                       |
| `herdr-integr`                                   | Inspect supported targets relevant to discovered agents. Absent agents do not require hooks.                                                                                 |
| `herdr-plugins`                                  | Resolve Herdr's registry. Evaluate plugins individually and respect source types, requested refs, and supported update behavior.                                             |
| `skills`                                         | Discover roots through supported consumers and explicit locations. Parse YAML frontmatter, including multiline descriptions. Apply Agent Skills limits.                      |

These rows cover the 21 registered checks. Preserve their questions and resource coverage, not false-positive
heuristics.

Version-sensitive compatibility checks must explain unsupported behavior. They must not report `OK` without evidence.
Unknown or unhandled package sources must not be silently reported as current.

Follow skill-directory links only within declared roots and with cycle detection and traversal limits. Permission
failures and truncated scans must appear as incomplete coverage rather than disappear.

## 10. Architecture and responsibilities

Keep the single executable entry point in root `main.go`. It constructs dependencies, configures lifecycle and logging,
and starts the TUI.

```text
main.go
  constructs dependencies and starts the TUI

TUI: menu.go initially, or internal/tui when justified
  calls internal/app use cases

internal/app
  coordinates audits, approved actions, and history
  uses internal/doctor and internal/store

internal/doctor
  discovers instances and evaluates checks
  returns observations, findings, and action proposals

internal/store
  owns action-history persistence and queries
```

The diagram describes dependency direction, not a requirement to create every directory immediately.

### Doctor

Split the current file by integration and shared responsibility in the same package. Doctor must not render terminal
output, access SQLite, prompt, or execute maintenance actions.

Read-only filesystem, subprocess, and network inspection belong here. Discovery must not modify inspected resources. Do
not introduce a separate discovery package without a concrete dependency or test boundary.

### Application

The application coordinates audit lifecycle, detail retrieval, action preparation, execution, post-action inspection,
and history. It applies the shared cancellation and failure policy.

Action execution can start in focused files within `internal/app`. A separate executor package is not required. Define
small consumer-owned interfaces only where substitution or independently varying behavior requires them.

### TUI and process behavior

The TUI renders generic data and collects input. It does not contain integration-name switches, SQL, registry wiring, or
shell execution.

Keep Go-TUI. Audit, detail, preview, progress, and history are views within one application. Do not add a parallel CLI
workflow.

Require an interactive terminal. Without the required TTY, print a plain error to stderr and return a non-zero exit
without terminal control sequences. Launch flags for configuration or database selection do not create a second workflow
interface.

## 11. Registration and extensibility

Keep one explicit built-in registration location. It describes supported checks and their integration ownership, not
workstation installations.

Adding a checker requires its implementation, one registry entry, and tests. A new integration's implementation also
supplies discovery. Existing checks reuse discovery for their integration.

The engine, TUI, and store must not switch over integration names. Registration validates empty and duplicate
identities. Keep metadata authoritative rather than duplicating it in methods and results.

Checks that use existing finding and action capabilities require no engine, formatter, TUI, or store changes. A
genuinely new execution capability can require executor work. This exception must remain explicit.

The specification defines behavioral contracts, not final Go signatures. Keep them small and support multiple findings
without a universal plugin lifecycle.

## 12. Read-only audit execution

Audit and detail refresh perform discovery and inspection only. Neither creates audit-history rows or initializes
storage.

They must not install packages, refresh manager repositories, fetch into inspected Git checkouts, rewrite configuration,
or initialize resource caches. They must not start MCP servers or load third-party extensions.

Inspection-command safety requires version-aware evidence. A command can refresh metadata or initialize state even when
its name sounds read-only. Use restrictive environment options, direct metadata inspection, or verified behavior. If
safety is unresolved, report the limitation instead of executing the command.

Network metadata retrieval is allowed without changing inspected workstation resources. Bound requests and disclose
network failures or stale metadata. Do not modify the machine to improve an audit answer.

### Ordering, resources, and cancellation

Order findings by built-in check order and stable integration-defined instance order. Dynamic packages and plugins
require bounded concurrency. File sizes, traversal depth, response sizes, and subprocess output also require limits.

Propagate context cancellation through subprocesses, HTTP requests, and file walks. Stop scheduling new work after
cancellation. Keep completed observations and mark incomplete work explicitly.

One failed plugin lookup must not discard successful findings for other plugins. Resource-limit exhaustion must produce
a partial-coverage diagnostic. Test shared observation state with the race detector.

## 13. Structured actions and confirmation

An action proposal contains target identity, reason, intended effect, scope, provenance, preconditions, ordered steps,
and expected postcondition. Manual-only guidance remains valid when safe automatic execution is unavailable.

Executable data and display text are separate. Use resolved executable paths, argument arrays, and explicit working
directories. Do not reparse displayed commands or interpolate discovered values into shell strings.

Prefer a verified native updater over reproducing installer pipelines. When an updater requires behavior that cannot be
safely represented, provide manual guidance. Do not add arbitrary shell execution as an extension mechanism.

### Lifecycle

```text
Finding
  -> Prepare action
  -> Preview target, scope, and side effects
  -> Explicit confirmation
  -> Acquire exclusive maintenance ownership
  -> Record attempt start
  -> Recheck preconditions
  -> Execute ordered steps
  -> Inspect postcondition
  -> Record execution and verification outcomes
  -> Release maintenance ownership
```

Do not execute if the start record cannot be saved. If preconditions fail, finish the attempt as blocked without
executing steps. A changed installation, version, target, or scope invalidates the proposal and requires fresh
confirmation.

Serialize maintenance actions initially. Prevent conflicting actions across application processes. The ownership
mechanism must not be a fragile PID-only marker or an open transaction held through subprocess execution.

Before each action, recheck the evidence needed for its plan. External programs can still alter the installation
concurrently. This design cannot guarantee machine-wide exclusivity and must handle resulting failures.

Do not silently request elevated privileges or install an absent manager. Explain requirements for interactive
authentication and restart where known. Actions unsuitable for safe TUI execution remain manual-only.

### Scope and verification

Avoid offering a broad manager update and a tool-specific update as separate operations against the same target. Prefer
selected targets. Disclose unavoidable additional changes before confirmation.

Pins are intentional unless evidence establishes otherwise. Treat dirty, divergent, or detached Git states as explicit
constraints, not permission to pull.

Execution and verification have separate outcomes. A successful command with unavailable verification is not a verified
repair. Failed or canceled execution can leave partial changes and must not imply rollback.

Cancellation must stop supported subprocess work and prevent later steps. Use a separate bounded finalization context to
record the outcome after execution cancellation. Do not leave every canceled attempt unfinished merely because its
execution context ended.

## 14. Action-history-only storage

### Selected history policy

Persist maintenance attempts only. User-started audits, automatic scans, detail refreshes, previews, copied guidance,
and read-only inspections do not create history records.

This keeps audits read-only and avoids recreating `check_runs` under another name. Current details come from discovery.
Relevant before-and-after facts belong to the action that needed them.

The trade-off is deliberate: users cannot browse previous audits or reconstruct a complete installation timeline.
Installation dates come from reliable external evidence, or remain unavailable.

### Record content and structure

Keep SQLite and replace the current dependence on audit runs and results. An action record and ordered step records are
sufficient. Do not create a generic event store.

Capture enough bounded context to explain the attempt:

- Action identity, optional batch identity, and initiating application version.
- Integration, check, and instance identity, plus a safe historical label.
- Action type, target scope, and execution ownership.
- Start and finish times, independently of installation-time evidence.
- Relevant installed and intended versions or revisions and their sources.
- Known manager ownership and safe locations needed to explain the target.
- Reason, preconditions, and ordered planned steps.
- Per-step execution results and safe errors.
- Observed post-action version or state, when obtained.
- Separate execution and verification outcomes.

Do not attach the entire audit or full instance inventory. Capture only action-relevant evidence. Installation-time
evidence can be included when relevant, but remains optional.

Terminal execution outcomes distinguish completed, failed, canceled, blocked, and interrupted attempts. Step outcomes
distinguish completed, failed, canceled, and not started. Verification distinguishes passed, failed, unknown, and not
performed.

Keep frequently filtered fields directly queryable. Optional metadata is bounded, versioned, and allowlisted. Store safe
step descriptions rather than secrets embedded in executable arguments. Persisted records are never executable plans.

### Queries and readable history

Support recent actions, integration or instance filters, outcome filters, time ranges, batches, and paginated detail
queries. Do not load the entire history to render a list.

History details retain relevant context even after the target disappears. They distinguish intended targets from
observed outcomes. A legacy record with limited evidence must not acquire invented details during migration.

### Failure and interruption policy

Open or create storage only for history or maintenance use cases. Audits and current detail views remain available if
the database is unavailable.

Save an attempt before maintenance execution and avoid long database transactions around subprocesses. If final
recording fails, show the actual execution outcome and the history failure separately. Stop subsequent maintenance
actions until resolved. Never rerun a completed action to repair its history.

An unfinished record indicates incomplete observation. Reconcile interruption only when the owning execution is no
longer active. Do not classify another live application's action as interrupted when opening history.

After a crash, an interrupted attempt does not prove that the system stayed unchanged. Do not auto-resume or retry it.
Require fresh inspection and user confirmation for any later maintenance.

A declined confirmation creates no record. A confirmed attempt that fails a precondition or is canceled before execution
can explain that no step started. If start recording itself fails, show the error without claiming a durable record
exists.

### Privacy, limits, and retention

Never persist configuration contents, complete environment variables, or unbounded output. For MCP configuration, allow
aggregate counts only. Server names, commands, URLs, headers, and credentials must not enter detail views, diagnostics,
or history.

Use allowlisted summaries by default. Optional output excerpts need integration-specific secret handling. Do not assume
generic redaction can make arbitrary output safe. Remove terminal control sequences before display.

Keep history files private to the user. Store only needed paths and metadata. These remain local workstation
information, not telemetry.

Use these initial retention defaults for review:

| Limit               | Initial policy                                              |
|---------------------|-------------------------------------------------------------|
| Age                 | Retain terminal actions for 90 days.                        |
| Record count        | Retain at most 10,000 terminal actions, keeping the newest. |
| Safe output excerpt | At most 16 KiB per step and 64 KiB across one action.       |
| Optional metadata   | At most 8 KiB per action.                                   |

These are design defaults, not measured workload requirements. Include explicit truncation indicators. Planned step
counts and targets must also be bounded without truncating executable scope silently.

The output and metadata caps do not constitute a hard database-file size limit. Step counts, fixed fields, indexes, and
SQLite overhead also affect size. Set their limits in the implementation plan and test retention against realistic
action batches.

Apply retention outside audits and never delete running attempts. Deleting old actions includes their associated steps.
Expose policy values and support documented overrides without a broad configuration framework.

SQLite row deletion does not guarantee immediate physical shrinkage. File compaction is separate maintenance, not an
audit side effect. History is local maintenance history, not a permanent compliance archive.

### Legacy migration

Do not silently delete or overwrite the existing database. Preserve the original database and migrate existing action
rows into the new model using available safe context.

Mark missing or inferred fields explicitly. Legacy status is not proof of post-action verification. Sanitize legacy
command and output text under the same privacy rules.

Do not copy old audit runs or snapshots into the new model. Extract only context relevant to an actual legacy action.
The preserved original remains outside the new history queries.

Migration requires an explicit application maintenance flow, failure recovery, and user-approved handling of the old
database. It must not run during an audit. Validate it against temporary databases before any real data migration.

## 15. Platform policy

Use shared Go code where behavior is shared. Put genuine differences, such as macOS application discovery and process
controls, in focused helpers or OS-specific files.

Do not add a universal operating-system interface. Homebrew is not synonymous with macOS. Linux support also does not
require support for every Linux package manager in this redesign.

A tool can be present and inspectable while its update method remains unsupported. Explain this at the instance and
action levels. Do not hide the installation because automatic maintenance is unavailable.

For Workstation Doctor's own database, use macOS Application Support and Linux XDG state storage, with an explicit path
override. Honor valid absolute XDG paths on Linux. The old database location requires explicit migration handling.

This application policy must not replace the conventions of inspected products. Configuration, data, and state are
distinct locations.

## 16. Pattern decisions and organization references

### Small checker boundary

The recurring problem is independently maintained inspection algorithms behind one engine. Retain the existing
Strategy-like boundary because it already supports that variation. Make registration and implementation files easier to
understand rather than adding another strategy layer.

The local Strategy note describes independent algorithms and the cost of additional objects. The checks are not
runtime-selected replacements for one business algorithm, so this is a limited structural analogy.

### Represented actions

The recurring problem is coupling TUI triggers to concrete operations and recording. The local Command note describes
representing requests separately from their sender.

Structured action data and one executor address that problem. They do not require a command interface or class per
operation. This design does not claim that serialization, undo, or crash recovery comes automatically from the pattern.

### Patterns not selected

Do not add Observer subscriptions, Factory Method machinery, a DI container, reflection registration, or runtime checker
plugins. No current requirement justifies them.

The application service is concrete orchestration, not a Facade framework. Keep constructor wiring explicit and extract
interfaces only where consumers need them.

### Reference projects

[Mole](https://github.com/tw93/Mole) separates scan, model, output, and view responsibilities across files. Its multiple
commands and broader scanning scope do not justify copying its command layout or caches.

[Dev Cockpit](https://github.com/caioricciuti/dev-cockpit) uses TUI module contracts but still registers its modules
explicitly. Its modules are UI tabs, not inspection checks. Do not copy that lifecycle into doctor.

The
reviewed [Gortex upgrade implementation](https://github.com/zzet/gortex/blob/e12965558e80c153805521d174efcbda2b0654d5/cmd/gortex/upgrade.go)
provides another Go reference. It separates update planning from execution and distinguishes installation methods. Use
that evidence to inform previews and provenance, not to copy shell-command parsing.

These references support focused responsibilities and explicit composition. They do not define discovery behavior for
unrelated products.

## 17. Migration outline and documentation follow-up

This is a sequencing outline, not an implementation plan. Every stage needs explicit scope, execution tests, and
approval through the later implementation plan.

1. Characterize current behavior and distinguish verified behavior from heuristics that this spec rejects.
2. Split `doctor.go` into focused files within its current package without mixing structural moves with behavior
   changes.
3. Introduce integration descriptors, discovered instances, shared observations, and explicit registration.
4. Correct discovery, availability, version, and compatibility rules across all existing check coverage.
5. Centralize TUI use cases and structured confirmed execution. Remove the parallel CLI and non-TTY workflows.
6. Introduce action-only history, safe migration, and separate execution and verification outcomes.
7. Add generic integration, instance, finding, action, and history details.
8. Verify platform behavior, cancellation, privacy, and contributor extension paths.

New maintenance execution must not be enabled before start recording, ownership, and confirmation rules work together.
Do not leave an intermediate release with an unrecorded execution path.

### `AGENTS.md` follow-up

`AGENTS.md` is repository-local working guidance for coding agents. It explains the code map, safety rules, verification
commands, and contribution procedure. It is not runtime configuration or a substitute for this design.

After the user approves the written spec, rewrite it as a separate documentation task. Distinguish current
implementation from approved target architecture during migration. Do not describe unimplemented packages as already
present.

The future revision must preserve these rules:

- Audits and detail reads remain read-only.
- Real maintenance requires permission in the active session.
- MCP information remains aggregate-only and secret-safe.
- Documentation, application text, and commit messages remain English.
- Verification uses existing `make` targets and temporary databases.
- No binaries, database files, secrets, remote pushes, or pull requests without authorization.
- Go skill routing remains explicit, while design-pattern use stays problem-driven.

Replace obsolete CLI and persisted-audit instructions only when their migration state is clear. Link to approved design
decisions instead of duplicating the full specification. This document does not change the current `AGENTS.md`.

## 18. Verification and acceptance criteria

Tests use controlled subprocess and network dependencies, temporary files, and temporary databases. They must not
perform real updates or modify the default history database.

Required execution cases include:

- Absent tools, empty collections, and unavailable dependent checks.
- Optional missing configuration and invalid existing configuration.
- Environment overrides, custom prefixes, and unreadable explicit locations.
- Multiple executable candidates and uncertain ownership.
- Supported and unsupported installed-version formats.
- Offline, stale, and partially available metadata.
- Pins, dirty Git states, divergence, and detached revisions.
- Partial plugin results and bounded traversal.
- Reliable, unavailable, and revision-specific installation timestamps.
- Generic details for integrations, instances, findings, previews, and historical records.
- Cancellation during discovery, execution, verification, and final recording.
- Changed preconditions after confirmation and duplicate target scope.
- History failures before and after execution, interrupted records, and concurrent ownership.
- Retention, pagination, truncation, and legacy migration.
- Secret handling and terminal-control sanitization.
- Deterministic result ordering and race-free shared observations.

Linux requires Linux execution tests. Cross-compilation alone does not establish runtime correctness. Confirm
standard-library features against the declared Go toolchain before selecting implementation APIs.

The implementation must satisfy these acceptance criteria:

- One TUI starts from root `main.go` and rejects non-TTY operation plainly.
- All 21 existing check areas retain coverage with corrected discovery and evaluation.
- A checker using existing capabilities needs implementation, one registry entry, and tests only.
- Doctor never prints, persists, prompts, or applies maintenance.
- TUI never executes shell strings or SQL and does not branch on integration names.
- Details explain evidence, freshness, uncertainty, update sources, and available actions.
- Installation times are sourced and labeled or explicitly unavailable.
- Audits and details work without storage and never create audit snapshots.
- Confirmed maintenance uses one execution path with start recording and exclusive ownership.
- History explains action-relevant context, steps, errors, execution, and verification without becoming a persistent
  inventory.
- Existing data is not silently deleted during migration.
- MCP data remains aggregate-only across all outputs and persistence.
- `make vet`, `make test`, `make lint`, and `make vuln` succeed for implementation changes.

## 19. Official sources and research limits

Sources were reviewed for product-specific facts, not as universal rules. Some website requests failed, and repository
source supplied several gaps. Installed versions still need version-aware compatibility evidence and fixtures.

| Product or specification | Official source and design relevance                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
|--------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Pi                       | [Repository](https://github.com/earendil-works/pi). [Pinned configuration](https://github.com/earendil-works/pi/blob/eb326d265ae0b88489a6d10319307780df827cdf/packages/coding-agent/docs/configuration.md), [packages](https://github.com/earendil-works/pi/blob/eb326d265ae0b88489a6d10319307780df827cdf/packages/coding-agent/docs/packages.md), and [changelog handling](https://github.com/earendil-works/pi/blob/eb326d265ae0b88489a6d10319307780df827cdf/packages/coding-agent/src/modes/interactive/interactive-mode.ts). |
| Herdr                    | [Repository](https://github.com/herdrdev/herdr). [Pinned configuration](https://github.com/herdrdev/herdr/blob/a124eed73c1f911ddf89a6ac5b2f7ab70d76f5c2/src/config/io.rs) and [plugin registry](https://github.com/herdrdev/herdr/blob/a124eed73c1f911ddf89a6ac5b2f7ab70d76f5c2/src/persist/plugin_registry.rs).                                                                                                                                                                                                                 |
| Ghostty                  | [Configuration documentation](https://ghostty.org/docs/config) and [repository](https://github.com/ghostty-org/ghostty). Locations, defaults, and version-dependent filenames.                                                                                                                                                                                                                                                                                                                                                   |
| Starship                 | [Repository](https://github.com/starship/starship) and [pinned configuration](https://github.com/starship/starship/blob/d4d0459c5c24ba8f64663af8714f7858d7fb357b/docs/config/README.md). Installation and override behavior.                                                                                                                                                                                                                                                                                                     |
| OpenCode                 | [Repository](https://github.com/anomalyco/opencode). Installation methods beyond npm.                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Tokenjuice               | [Repository](https://github.com/vincentkoc/tokenjuice). npm and Homebrew installation sources.                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| Serena                   | [Repository](https://github.com/oraios/serena). Installation and launch requirements.                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Gortex                   | [Repository](https://github.com/zzet/gortex) and [pinned upgrade implementation](https://github.com/zzet/gortex/blob/e12965558e80c153805521d174efcbda2b0654d5/cmd/gortex/upgrade.go). Planning, methods, and execution side effects.                                                                                                                                                                                                                                                                                             |
| uv                       | [Storage documentation](https://docs.astral.sh/uv/reference/storage/). Tool directories and overrides.                                                                                                                                                                                                                                                                                                                                                                                                                           |
| npm                      | [Outdated](https://docs.npmjs.com/cli/v11/commands/npm-outdated), [prefix](https://docs.npmjs.com/cli/v11/commands/npm-prefix), and [root](https://docs.npmjs.com/cli/v11/commands/npm-root). Scope and discovery.                                                                                                                                                                                                                                                                                                               |
| Homebrew                 | [Manpage](https://docs.brew.sh/Manpage), [repository](https://github.com/Homebrew/brew), and [receipt source](https://github.com/Homebrew/brew/blob/main/Library/Homebrew/tab.rb). Inventory, inspection behavior, and nullable receipt timestamp evidence.                                                                                                                                                                                                                                                                      |
| Superpowers              | [Repository](https://github.com/obra/superpowers). Harness-specific installation, including Pi packages.                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Agent Skills             | [Specification](https://agentskills.io/specification) and [repository](https://github.com/agentskills/agentskills). YAML frontmatter and description constraints.                                                                                                                                                                                                                                                                                                                                                                |
| Pi MCP Adapter           | [Repository](https://github.com/nicobailon/pi-mcp-adapter). Configuration discovery and version-dependent formats.                                                                                                                                                                                                                                                                                                                                                                                                               |
| XDG                      | [Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/latest/). Absolute paths and configuration, data, and state distinctions.                                                                                                                                                                                                                                                                                                                                                                     |

Pattern reasoning uses the local Go Design Patterns Knowledge Base notes for Strategy and Command, attributed to
Refactoring.Guru. Those notes do not establish platform behavior, security guarantees, or performance.

## 20. Approved decisions

The approved specification selects the following defaults:

- Keep root `main.go` and one TUI, with no non-TTY menu.
- Register supported implementations explicitly and discover workstation instances dynamically.
- Use the current user and active environment by default, with explicit project scope.
- Separate availability, findings, executable plans, and recorded outcomes.
- Provide generic readable details backed by current observations.
- Show installation-time evidence with its meaning, or show unavailable.
- Include structured actions and action-only history in this redesign.
- Persist maintenance attempts only, not audits or detail reads.
- Block maintenance when its start cannot be recorded.
- Use initial retention limits of 90 days and 10,000 terminal actions.
- Preserve the legacy database through an explicit migration flow.
- Rewrite `AGENTS.md` only after spec approval, with migration-aware instructions.

Approval of this spec permits planning, not automatic code execution. Review the implementation plans before selecting
an execution method. Installation, packaging, deployment, and distribution remain excluded.
