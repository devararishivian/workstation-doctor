# Workstation Doctor Redesign Roadmap

Status: Implementation plans proposed for review. No implementation started.

Spec: [Approved architecture](../specs/2026-10-07-workstation-doctor-architecture-redesign-design.md).
Baseline: `develop`, product commit `0273123`. Spec approval commit: `c7ad518`.

## Ordered plans

Execute these plans in order. Review each stage's evidence before beginning the next stage.

| Order | Plan | Independently testable deliverable |
| --- | --- | --- |
| 1 | [Foundation](2026-10-07-workstation-doctor-01-foundation.md) | Focused doctor files, migration safety guard, typed observations, registry, bounded read-only engine. |
| 2 | [Tool discovery](2026-10-07-workstation-doctor-02-tool-discovery.md) | Eight tool checks and two manager checks evaluated through discovered instances. |
| 3 | [Resource inspection](2026-10-07-workstation-doctor-03-resource-inspection.md) | Six configuration checks and five resource checks, completing all 21 check IDs. |
| 4 | [History and safe actions](2026-10-07-workstation-doctor-04-history-actions.md) | Action-only store, explicit migration, ownership, confirmed executor, tested without live updates. |
| 5 | [TUI cutover and details](2026-10-07-workstation-doctor-05-tui-cutover.md) | One TUI using the service, readable details, removal of legacy workflows, native platform verification. |

Plans 1 through 4 build usable, tested components beside the legacy application. They do not claim that the target TUI already exists.
Do not publish intermediate stages as the completed redesign. The legacy maintenance guard stays active until the Plan 5 cutover.

Local commits only. No remote pushes or PRs. Do not create worktrees or delegate until the user chooses execution and isolation.
For future implementation, use one writer per worktree. These plans share contracts and registration, so serial execution is the default.

## Refactoring inventory

| Change | Files and consumers | Risk | Kind | Owner |
| --- | --- | --- | --- | --- |
| Characterize and split doctor | `doctor.go`, existing tests | Medium | Structural | Plan 1, tasks 1-2 |
| Disable unsafe legacy maintenance | `main.go`, `menu.go` | High | Behavioral | Plan 1, task 3 |
| Add new observations and engine beside old API | doctor core files | Medium | Behavioral | Plan 1, tasks 4-5 |
| Replace ownership and version heuristics | tool and manager files | High | Behavioral | Plan 2 |
| Replace configuration/resource heuristics | resource files and registry | High | Behavioral | Plan 3 |
| Add action-only history and explicit import | store and application files | High | Behavioral | Plan 4, tasks 1-3 |
| Add exclusive confirmed execution | application files | High | Behavioral | Plan 4, tasks 4-5 |
| Cut over TUI and delete old APIs | root files, doctor/store legacy files | High | Behavioral | Plan 5 |

Refactoring guidance owns the safe sequencing. Struct/interface guidance owns the new contracts. Named patterns remain explanatory, not implementation requirements.
Graph localization failed because the repository is not tracked. File and caller descriptions here come from direct inspection, not a graph-backed impact claim.

## Shared contract decisions

These names are planned APIs, not existing symbols. Each producing task owns its definitions. Consumers must use these contracts rather than invent alternatives.
Any change to a shared contract requires updating all affected plans before implementation continues.

### Doctor models: Plan 1, task 4

Use concrete structs and string-backed enums. Empty enum values mean unspecified, never success.
Define these values in `internal/doctor/model.go`:

- `Availability`: `Present`, `Absent`, `Unsupported`, `Undetermined`.
- `Outcome`: `OK`, `Attention`, `Unknown`, `NotApplicable`, `Canceled`.
- `EvidenceState`: `Known`, `Unavailable`, `UnsupportedEvidence`, `ReadFailed`.
- `ActionMode`: `Automatic`, `Manual`, `Inspection`.

```go
type Scope struct {
    ProjectDir string
    Locations map[string][]string // explicit roots keyed by integration ID
    SkillRoots []string
}
type Fact struct {
    State EvidenceState
    Label, Value, Source, Note string
    ObservedAt time.Time
}
type InstallationEvidence struct {
    At time.Time
    Source, Meaning, Precision string
    ObservedAt time.Time
}
type Provenance struct {
    State EvidenceState
    Manager, Package, Root, Channel string
}
type Integration struct {
    ID, Name, Description string
    References []string // official public references only
    SupportedScopes []string
}
type PublicReference struct { Kind, Label, URL string }
type CheckDescriptor struct { IntegrationID, ID, Name, Question string; Order int }
type Instance struct {
    ID, IntegrationID string
    Scope, DiscoverySource string
    ObservedAt time.Time
    Capabilities []ActionMode
    Availability Availability
    Active bool
    Executable, ResolvedPath, Root, Version Fact
    Configuration []Fact
    Provenance Provenance
    InstalledAt *InstallationEvidence
    Diagnostics []string // safe summaries, never raw configuration
}
type FindingKey struct { IntegrationID, CheckID, InstanceID string }
type Finding struct {
    Key FindingKey
    Outcome Outcome
    Question, Explanation string
    Evidence []Fact
    References []PublicReference // official links, including kind "release-notes"
    Actions []ActionProposal
}
type Command struct { Executable, Dir string; Args []string; Env map[string]string }
type CommandResult struct {
    Stdout, Stderr []byte // internal only; not directly rendered or persisted
    ExitCode int
    Truncated bool
}
type CommandStep struct { Label string; Command Command }
type ActionProposal struct {
    ID string
    Key FindingKey
    Mode ActionMode
    Label, Reason, DisabledReason string
    TargetIDs []string
    TargetVersion Fact
    Steps []CommandStep
    Preconditions []Fact
    VerificationCheckID string
    SideEffects []string
}
type Discovery struct {
    Availability Availability
    Instances []Instance
    Diagnostics []string
}
type AuditReport struct {
    StartedAt, FinishedAt time.Time
    Scope Scope
    Integrations []Integration
    Checks []CheckDescriptor // derived from registration, never separately maintained
    Discoveries []Discovery // same index as Integrations
    Findings []Finding
    Canceled bool
}
```

Instance IDs use integration ID plus canonical location and scope. Symlink aliases to one confirmed installation share an ID.
Different prefixes and project scopes never collapse by version. Empty discovery retains its integration descriptor.
Packages/plugins with separate findings have separate instance IDs, derived from their registry/root plus validated resource identity.
Empty registries retain a scope instance; do not give sibling resources one indistinguishable FindingKey.
Facts carry readable labels and identify evidence sources, not necessarily a raw command line. Do not attach MCP strings to them.
Providers supply public reference kind `release-notes` only when established from official metadata. App and TUI use that kind generically.
Integration details derive supported checks from report descriptors; providers do not maintain a duplicate check-name list.

### Registry and inspection: Plan 1, tasks 4-5

```go
type CheckDefinition struct {
    ID, Name, Question string
    Order int // globally unique positive registration order
    Evaluate func(context.Context, *Host, Scope, Instance) []Finding
}
type Definition struct {
    Integration Integration
    Discover func(context.Context, *Host, Scope) Discovery
    Checks []CheckDefinition
}
func NewAuditEngine(defs []Definition, limits Limits) (*AuditEngine, error)
func (e *AuditEngine) Audit(ctx context.Context, host *Host, scope Scope) AuditReport
func (e *AuditEngine) Inspect(ctx context.Context, host *Host, scope Scope, key FindingKey) (Finding, error)
func BuiltinDefinitions() []Definition // first supplied in Plan 2, completed in Plan 3
```

`Host` lives in `internal/doctor/host.go`. It holds `OS`, `Home`, and `Path` strings, copied `Env map[string]string`,
`Now func() time.Time`, `RunRead func(context.Context, Command) (CommandResult, error)`, and
`Fetch func(context.Context, string) ([]byte, error)`. Its unexported inventory cache is concurrency-safe and audit-local.
Production constructors provide bounded command and HTTP behavior. Tests use temporary roots and replace these two I/O functions.
Do not build a universal filesystem interface. Direct filesystem reads go through bounded local helpers with temporary-file tests.

`Audit` performs one discovery per integration and produces generic unavailable findings when prerequisites are absent.
It orders by CheckDefinition.Order, then stable instance order. Builtin check orders 1-21 preserve the current check sequence;
a newly registered check supplies its own unused order value. Registration rejects missing/duplicate order values.
`Inspect` uses a fresh inventory context and resolves the check through registration, without a product-name switch.
Ordinary inspection failures become findings, not panics. Registry errors are constructor errors.

### Application: Plan 4, tasks 4-5

```go
func NewService(engine *doctor.AuditEngine, host *doctor.Host, scope doctor.Scope, options Options) *Service
func (s *Service) Audit(ctx context.Context) doctor.AuditReport
func (s *Service) Current() doctor.AuditReport
func (s *Service) Prepare(ctx context.Context, key doctor.FindingKey, actionID string) (PreparedAction, error)
func (s *Service) Confirm(prepared PreparedAction) (Approval, error)
func (s *Service) Apply(ctx context.Context, prepared PreparedAction, approval Approval) (ActionReport, error)
func (s *Service) History(ctx context.Context, query store.ActionQuery) (store.ActionPage, error)
func (s *Service) HistoryDetail(ctx context.Context, id string) (store.ActionRecord, error)
```

`PreparedAction` exposes `Preview() Preview` but hides execution data. `Approval` hides a one-use token bound to the prepared fingerprint.
`Confirm` is called only by the UI's explicit affirmative interaction. Tokens prevent accidental stale/repeated invocation, not dishonest callers.
`Preview` carries safe labels, facts, scope, step descriptions, side effects, disabled reason, and verification expectations.
`ActionReport` carries record ID, execution outcome, verification outcome, safe error, and a separate history error.

`Options` contains `Version`, `DBPath`, and `StateDir` strings, `Policy store.Policy`, a history-opening function,
a command-step runner, and an ownership-acquisition function. Define the three function seams in Plan 4, not a service locator.
Use consumer-owned history writer, reader, and maintenance interfaces defined in Plan 4. Their purpose is deterministic failure injection.
No store access occurs in `NewService`, `Audit`, or `Current`. Current reports and prepared actions are isolated snapshots, not shared mutable slices.

## Initial implementation limits

`DefaultLimits() Limits` in Plan 1 freezes these initial values. They are adjustable implementation defaults, not claims about installed tools.
Define the integer fields `MaxConcurrency`, `MaxCaptureBytes`, `MaxFiles`, `MaxDepth`, `MaxSteps`, `MaxTargets`, `MaxArguments`,
`MaxArgumentBytes`, `MaxIdentifierBytes`, `MaxLabelBytes`, `MaxNoteBytes`, and `MaxPathBytes`;
int64 fields `MaxFileBytes`, `MaxHTTPBytes`; and duration fields `InspectionTimeout`, `HTTPTimeout`, `AuditTimeout`,
`MaintenanceTimeout`, and `FinalizationTimeout`. Map each to the matching row below.

| Limit | Value |
| --- | --- |
| Dynamic inspection concurrency | 8 |
| Inspected file and HTTP body | 2 MiB each |
| Inspection command captured output | 1 MiB total |
| Files visited per declared resource scan | 10,000 |
| Directory depth | 32 |
| Inspection command / HTTP / whole audit deadlines | 30 seconds / 15 seconds / 2 minutes |
| Maintenance steps / targets per action | 32 / 128 |
| Arguments per command / total argument bytes | 128 / 32 KiB |
| Identifier / safe label / safe note / path bytes | 256 / 2 KiB / 4 KiB / 4 KiB |
| Maintenance deadline / finalization deadline | 10 minutes per step / 5 seconds |
| History page default / maximum | 50 / 200 |
| History age / terminal count | 90 days / 10,000 |
| History output | 16 KiB per step and 64 KiB per action |
| Optional history metadata | 8 KiB per action |

Limit exhaustion is explicit. Do not truncate executable scope, command arguments, identifiers, or critical preconditions.
Reject oversized plans and ask the user to select smaller batches. Only safe display/output excerpts can truncate with an indicator.
Caps bound payloads, not the physical SQLite file. Plan 4 tests realistic batches, retention, and fixed-field validation.

## Verification and task discipline

Every plan inherits `AGENTS.md` and spec requirements. Every behavior task has a failing test, a minimal implementation, and a passing test.
Characterization and mechanical moves use a preserved green baseline rather than deliberately changing their semantics.
No test executes an installed updater, reads real MCP configuration, or uses the default history database.

Plan 1 adds optional `TEST_PKG` and `TEST_RUN` variables to the existing `make test` target without changing its default race-enabled behavior.
After that task, use `make test TEST_PKG=... TEST_RUN=...` for focused red/green cycles.
Before each local commit, run `make vet test lint vuln`. Stage exact task files, inspect the staged diff, and use the task's English commit subject.
Commit steps assume verification succeeded. They never authorize remote activity.
Each task ends with this procedure, using that task's explicit file list and commit subject:

```sh
git diff --check
git add -- <exact task paths>
git diff --cached --check
git diff --cached
git commit -m "<task's English commit subject>"
```

The angle-bracket arguments are instructions to substitute the task's listed paths/subject, not literal shell commands.
Do not use broad staging. No dependency bump or product implementation is part of the current documentation work.

## Coverage and review barriers

| Spec sections | Responsible plans |
| --- | --- |
| 1-4: purpose, scope, verified corrections | All plans; stage boundaries above |
| 5-7, 11-12: model, discovery, states, extension, read-only engine | Plan 1; providers in Plans 2-3 |
| 8: integration, instance, update, action, and history details | Plan 5, tasks 2-4; evidence in Plans 2-4 |
| 9: all 21 checks | Plan 2, tasks 2-5; Plan 3, tasks 1-4 |
| 10: application boundaries and TUI-only interface | Plan 4, task 5; Plan 5, tasks 1 and 5 |
| 13-14: confirmed actions, history, limits, migration | Plan 4, tasks 1-5 |
| 15: platform behavior | Plans 2 and 4; native gates in Plan 5 |
| 16-17: lightweight patterns and migration | Plan 1 split, shared contracts, serial stage gates |
| 18-20: tests, sources, approved choices | Every task; completion matrix in Plan 5 |

Review plan contracts and defaults before execution. Finish Plan 1 before provider work, Plans 2-3 before final action rechecks,
and Plan 4 before enabling maintenance in the TUI. No plan performs a real database migration or update merely to prove it works.

Recommended future execution: task-by-task subagent-driven work with independent review, if the user authorizes it.
The store, subprocess, and stale-confirmation boundaries deserve independent checks. Native execution remains a valid choice,
but does not imply permission to launch a reviewer. Choose the method after plan review.
