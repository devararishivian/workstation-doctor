# Tool Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for native execution or superpowers:subagent-driven-development only after operator authorization. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Inspect eight tool families and two manager inventories without guessing installations or updaters.

**Architecture:** Each integration discovers instances and supplies focused evaluators to the common engine. Manager inventory is reused within an audit and linked to executable evidence. Production UI remains legacy until Plan 5.

**Tech Stack:** Existing Go dependencies, bounded filesystem inspection, direct HTTP metadata, synthetic fixtures, and injected read commands.

**Spec:** [Approved architecture](../specs/2026-10-07-workstation-doctor-architecture-redesign-design.md), sections 5-9, 12, and 15.

## Global Constraints

- Requires completed Plan 1 and [roadmap contracts](2026-10-07-workstation-doctor-redesign-roadmap.md).
- Preserve `pi`, `herdr`, `ghostty`, `starship`, `opencode`, `tokenjuice`, `serena`, `gortex`, `brew-outdated`, and `npm-outdated-g` coverage.
- Discovery stays read-only. No transient launchers, metadata refresh commands, or automatic installation.
- Unknown ownership disables automatic update; upstream latest is not manager availability.
- Installation timestamps require evidence and meaning; filesystem and Git timestamps are not substitutes.
- Every task uses fake commands/HTTP and the global `make vet test lint vuln` gate before its local commit.

## Review Focus

- PATH shims can point to a different manager prefix: Task 1's ownership mismatch case.
- A manager can be installed while the tool belongs elsewhere: Tasks 2-3's unknown ownership cases.
- A native macOS app can exist outside PATH: Task 3's inactive application case.
- Receipt time can refer to the current revision, not first installation: Task 1's nullable/time-meaning cases.
- Offline lookup or an unparsable release must not become a downgrade proposal: Task 5's channel/version tests.

## File structure and shared functions

Create `internal/doctor/discovery.go`, `inventory.go`, `versions.go`, `builtin.go`, and matching tests.
Modify Plan 1's `pi.go`, `herdr.go`, `ghostty.go`, `starship.go`, `npm.go`, `serena.go`, `gortex.go`, and `brew.go`.
Create dedicated corresponding test files while leaving legacy heuristic assertions isolated in `doctor_test.go`.
Use synthetic `internal/doctor/testdata/tools/` fixtures, each with a small public-source/format note.

Produce these shared functions in Task 1:

```go
func ExecutableCandidates(ctx context.Context, host *Host, scope Scope, integrationID, name string, defaults []string) Discovery
func BrewInventory(ctx context.Context, host *Host, scope Scope) (Inventory, error)
func NpmInventory(ctx context.Context, host *Host, scope Scope) (Inventory, error)
func UvInventory(ctx context.Context, host *Host, scope Scope) (Inventory, error)
func MatchProvenance(instance Instance, inventory Inventory) Provenance
func CompareVersions(installed, candidate, scheme string) (int, error)
```

`Inventory` has `Manager`, `Executable`, `Root`, `FreshnessNote` strings, `ObservedAt time.Time`, and `Items []InventoryItem`.
`InventoryItem` has `Package`, `Root`, `InstalledVersion`, `AvailableVersion`, `Channel`, `RequestedPin` strings,
`Executables []string`, `UpdateDecision Fact`, and `InstalledAt *InstallationEvidence`.
UpdateDecision is an established manager decision or explicitly unknown, not a guess from unequal versions.
Define both in `inventory.go`. Cache inventories by manager executable, prefix, and scope.
CompareVersions initially accepts scheme `semver` only and returns negative/equal/positive for older/equal/newer installed version.
Manager decisions are consumed separately from inventory evidence. Unknown schemes return an error, never inequality-as-update.

Every tool discovery/evaluation pair has these exact signatures:

```go
func discoverPi(ctx context.Context, host *Host, scope Scope) Discovery
func checkPiInstance(ctx context.Context, host *Host, scope Scope, instance Instance) []Finding
```

Replace `Pi` with `Herdr`, `Ghostty`, `Starship`, `OpenCode`, `Tokenjuice`, `Serena`, or `Gortex` for the corresponding pair.
`discoverBrew`/`checkBrewInventory` and `discoverNpm`/`checkNpmGlobal` use the same discovery/evaluation signatures.

### Task 1: Resolve candidates and establish manager provenance

**Files:** Create `discovery.go`, `inventory.go`, `versions.go` and matching tests; extend `host.go` with the typed inventory cache;
create `testdata/tools/README.md`, `brew.json`, `npm.json`, and `uv.txt`.

**Interfaces:** Consume Host, Scope, Fact, Instance, and limits. Produce the six shared functions and inventory types listed above.

- [ ] Add `TestExecutableCandidates`, `TestManagerOwnership`, `TestInventoryCacheScope`, `TestReceiptInstallationTime`, and `TestCompareVersions`.
Use named cases for aliases, separate prefixes, invalid explicit overrides, unreadable paths, absent managers, and unknown provenance.

```go
provenance := MatchProvenance(otherPrefixInstance, inventory)
if provenance.State == EvidenceKnown {
    t.Fatal("manager presence incorrectly established ownership")
}
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestExecutableCandidates|TestManagerOwnership|TestInventory|TestReceipt|TestCompareVersions'`; expect FAIL.
- [ ] For each inventory command/format, read the spec's official sources and record the supported version/format in fixture notes.
Reject queries that initialize caches or refresh metadata. Use direct metadata reads or report unknown when command safety is unresolved.
- [ ] Implement candidate normalization, documented roots, prefix-scoped inventory, and provenance matching. Never scan all installed version-manager environments or source shell profiles.
Receipt timestamps remain nil when missing/invalid and carry revision-install meaning when established. Compare semantic versions including prereleases, leading `v`, and newer local versions.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: discover tool candidates and manager ownership`.

### Task 2: Implement Pi, OpenCode, and Tokenjuice instances

**Files:** Modify `pi.go`, `npm.go`; create `pi_test.go`, `opencode.go`, `opencode_test.go`, `tokenjuice.go`, `tokenjuice_test.go`, `builtin.go`, `builtin_test.go`.

**Interfaces:** Consume candidate and inventory functions. Produce the six named discovery/evaluation functions and initial `BuiltinDefinitions() []Definition`.
Register these three version checks only at this point. Do not use placeholders for unfinished integrations.
Every provider supplies labeled facts, instance scope/source, and established official PublicReference links; missing release links stay unavailable.

- [ ] Add `TestPiInstanceSources`, `TestOpenCodeManagerSelection`, and `TestTokenjuiceManagerSelection`.
Test npm ownership, Homebrew ownership where documented, native/unmanaged installations, Pi agent-directory override, and absent executables.

```go
finding := checkOpenCodeInstance(ctx, host, scope, unmanagedInstance)[0]
for _, action := range finding.Actions {
    if action.Mode == ActionAutomatic {
        t.Fatal("unmanaged instance received a guessed updater")
    }
}
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestPiInstanceSources|TestOpenCode|TestTokenjuice'`; expect FAIL.
- [ ] Implement source-aware discovery and version checks. Use registry HTTP metadata without invoking npm download/cache operations during audit.
Automatic plans use exact proven manager/prefix/package ownership. Pi native/Nix or other unimplemented update methods remain explicit manual/unsupported capabilities, not npm fallbacks.
- [ ] Register the implemented checks and test their IDs, generic findings, and direct-argv proposals. Do not execute proposals.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: inspect Pi OpenCode and Tokenjuice instances`.

### Task 3: Implement Herdr, Ghostty, and Starship instances

**Files:** Modify `herdr.go`, `ghostty.go`, `starship.go`, `builtin.go`; create `herdr_test.go`, `ghostty_test.go`, `starship_test.go`.

**Interfaces:** Produce their named discovery/evaluation pairs. Consume manager-provenance and installation-evidence contracts.

- [ ] Add `TestHerdrInstallSources`, `TestGhosttyCandidates`, and `TestStarshipUpdateOwnership`.
Cases include custom Homebrew roots, Linux PATH binaries, `/Applications` and user application candidates, unknown managers, and inactive app discovery.

```go
discovery := discoverGhostty(ctx, macHostWithoutPathBinary, Scope{})
if discovery.Availability != AvailabilityPresent || discovery.Instances[0].Active {
    t.Fatal("app discovery confused presence with PATH activation")
}
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestHerdrInstallSources|TestGhosttyCandidates|TestStarshipUpdateOwnership'`; expect FAIL.
- [ ] Implement discovery with explicit-location precedence and OS-local application candidates. Document the exact supported version output in fixtures.
Unsupported Linux update managers keep local version details and manual guidance. Homebrew cask ownership must match the actual app, not merely a cask name.
- [ ] Add the three checks to `builtin.go`; no engine or formatter edits. Assert local versions survive offline failures.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: inspect Herdr Ghostty and Starship installations`.

### Task 4: Implement Serena and Gortex instances

**Files:** Modify `serena.go`, `gortex.go`, `builtin.go`; create `serena_test.go`, `gortex_test.go` and synthetic launcher/version fixtures.

**Interfaces:** Produce `discoverSerena`, `checkSerenaInstance`, `discoverGortex`, and `checkGortexInstance`.
Declared launch sources can identify a capability without asserting a runnable installed executable.

- [ ] Add `TestSerenaPersistentAndDeclaredSources` and `TestGortexPlanModeSafety`.
Test uv-owned tools, absent persistent binary with a declaration, unknown uv/Git source, supported and unsupported preview versions, and offline plans.

```go
_ = discoverSerena(ctx, hostWithDeclaredTransientSource, scope)
if transientLauncherCalls != 0 {
    t.Fatal("discovery launched a transient environment")
}
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestSerenaPersistent|TestGortexPlanMode'`; expect FAIL.
- [ ] Implement uv ownership and PyPI/source-aware update interpretation. Keep MCP-derived information aggregate-only; do not expose launcher strings from secret configuration.
Use Gortex preview only for versions whose no-write behavior is established. Otherwise read release metadata or return an explicit limitation.
- [ ] Register both checks. Native updater proposals must disclose known side effects, including migrations or service restart behavior, rather than copying a printed shell plan.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: inspect Serena and Gortex capabilities safely`.

### Task 5: Implement manager-wide findings and complete tool-stage registration

**Files:** Modify `brew.go`, `npm.go`, `builtin.go`; create `brew_test.go`, `npm_test.go`; extend `builtin_test.go` and `versions_test.go`.

**Interfaces:** Produce `discoverBrew`, `checkBrewInventory`, `discoverNpm`, and `checkNpmGlobal`.
Automatic proposals target selected packages and include proven root/source; they do not issue blanket `brew upgrade` or `npm update -g`.

- [ ] Add `TestNpmGlobalScope`, `TestBrewInventoryFreshness`, `TestToolRegistryTenChecks`, and `TestUpdateCandidateOrdering`.
Test local/global inventory separation, pinned dependencies, stale manager metadata, offline registries, prereleases, and a local version newer than upstream.

```go
if got := globalFinding.Evidence[0].Source; got == localProjectSource {
    t.Fatal("global check used project scope")
}
if registryCheckCount != 10 { t.Fatalf("got %d checks, want 10", registryCheckCount) }
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestNpmGlobalScope|TestBrewInventoryFreshness|TestToolRegistryTenChecks|TestUpdateCandidateOrdering'`; expect FAIL.
- [ ] Implement findings from prefix-scoped inventory plus bounded remote reads. Manager cached information is labeled with freshness, not presented as guaranteed upstream latest.
Build one proposal per explicit target or bounded selected batch. Use shared target identity so Plan 4 can deduplicate aggregate and tool-specific proposals.
- [ ] Test an empty workstation: integrations remain visible, no applicable tool instance is healthy/failed by assumption, and zero automatic proposals exist.
- [ ] Run focused tests and the global gate; expect PASS. Commit: `feat: inspect global package inventories with explicit scope`.

## Stage exit

The new registry has ten implemented check IDs. Typed reports distinguish presence, uncertainty, installed facts, and manager availability.
Legacy maintenance remains disabled, and no production TUI or database is cut over. Review provider evidence before Plan 3.
