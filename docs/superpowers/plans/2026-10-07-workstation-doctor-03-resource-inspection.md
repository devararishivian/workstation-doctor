# Resource Inspection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for native execution or superpowers:subagent-driven-development only after operator authorization. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete all 21 check areas through safe configuration, package, plugin, integration, and skill inspection.

**Architecture:** Resource scopes become instances. Product-specific configuration resolvers follow documented precedence, and checks return per-resource findings without loading user code. Reuse Plan 2 observations instead of adding UI or storage branches.

**Tech Stack:** Go, bounded file reads, injected inspection commands, synthetic Git/package fixtures, and a YAML frontmatter parser selected through Go dependency research.

**Spec:** [Approved architecture](../specs/2026-10-07-workstation-doctor-architecture-redesign-design.md), sections 6-9 and 12.

## Global Constraints

- Requires Plans 1-2 and [roadmap contracts](2026-10-07-workstation-doctor-redesign-roadmap.md).
- Add six configuration checks and five resource checks to the ten existing checks, without placeholders.
- No configuration execution, extension loading, MCP startup, Git fetch, package install, or live remote fixtures.
- MCP configuration outputs remain aggregate-only, including errors and declared launch sources.
- Missing optional configuration is valid; unsupported schema knowledge is explicit uncertainty, never an invented repair.
- Pins, local sources, dirty repositories, and unavailable remotes must not become automatic update plans by default.
- The global `make vet test lint vuln` gate precedes each task commit.

## Review Focus

- JSON errors can quote credential-bearing tokens: Task 1's malformed MCP secret fixture.
- A cache miss or optional section omission is not obsolete configuration: Task 1's default cases.
- Linked skill directories can cycle or escape declared roots: Task 4's traversal tests.
- Remote ref inequality cannot establish behindness or safe pulling: Task 2's divergent/pinned cases.
- One failed plugin must not suppress successful siblings: Task 3's partial-result test.

## File structure and contracts

Modify `pi.go`, `herdr.go`, `ghostty.go`, `pi_packages.go`, `superpowers.go`, `herdr_plugins.go`, `skills.go`, and `builtin.go`.
Create `config_locations.go`, `resource_sources.go`, and matching tests. Add resource-specific test files and synthetic `testdata/resources/` data.

Task 1 produces:

```go
func ResolvePiConfiguration(host *Host, scope Scope) ([]Fact, error)
func ResolveHerdrConfiguration(host *Host, scope Scope) ([]Fact, error)
func ResolveGhosttyConfiguration(host *Host, scope Scope, version string) ([]Fact, error)
```

Configuration evaluators are `checkPiConfigValid`, `checkPiConfigCompatibility`, `checkHerdrConfigValid`,
`checkHerdrConfigCompatibility`, `checkGhosttyConfigValid`, and `checkGhosttyConfigCompatibility`.
Each has signature `(context.Context, *Host, Scope, Instance) []Finding`. Public check IDs remain `*-config-valid` and `*-config-version`.

Task 2 defines `ResourceSource` in `resource_sources.go` with `Kind`, `Identity`, `Path`, `RequestedRef`, `InstalledRevision`,
`Manager`, and `Scope` strings, `Pinned bool`, and safe `Fact` evidence. It produces
`DeclaredResources(ctx context.Context, host *Host, scope Scope) ([]ResourceSource, error)`.
Consumers must not serialize whole configuration or private launch strings into this type.

Register exactly these additional IDs, using the corresponding evaluator:

| Check ID | Evaluator | Task |
| --- | --- | --- |
| `ghostty-config-valid` | `checkGhosttyConfigValid` | 1 |
| `ghostty-config-version` | `checkGhosttyConfigCompatibility` | 1 |
| `herdr-config-valid` | `checkHerdrConfigValid` | 1 |
| `herdr-config-version` | `checkHerdrConfigCompatibility` | 1 |
| `pi-config-valid` | `checkPiConfigValid` | 1 |
| `pi-config-version` | `checkPiConfigCompatibility` | 1 |
| `pi-packages` | `checkPiPackageSources` | 2 |
| `superpowers` | `checkSuperpowersSource` | 2 |
| `herdr-integr` | `checkHerdrIntegrationTargets` | 3 |
| `herdr-plugins` | `checkHerdrPluginSources` | 3 |
| `skills` | `checkSkillMetadata` | 4 |

### Task 1: Correct configuration locations, defaults, and compatibility

**Files:** Create `config_locations.go`, `config_locations_test.go`; modify `pi.go`, `herdr.go`, `ghostty.go`, `builtin.go`;
add configuration tests in their integration test files and synthetic JSON/TOML fixtures.

**Interfaces:** Produce the three resolvers and six evaluators above. Consume discovered executable/version facts from Plan 2.

- [ ] Add `TestConfigurationPrecedence`, `TestConfigurationOptionalDefaults`, `TestPiChangelogNotSchema`, and `TestMCPAggregateOnly`.
Test Pi directory override, Herdr explicit path/XDG precedence, Ghostty filename/version order, absent optional caches, and unreadable existing files.

```go
findings := checkPiConfigCompatibility(ctx, hostWithOlderChangelogMarker, scope, instance)
for _, finding := range findings {
    for _, action := range finding.Actions {
        if action.Mode == ActionAutomatic { t.Fatal("changelog marker became a repair") }
    }
}
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestConfiguration|TestPiChangelogNotSchema|TestMCPAggregateOnly'`; expect FAIL.
- [ ] Implement documented precedence and static syntax parsing. Missing optional settings use defaults. Parser failures return generic safe diagnostics, never raw secret-bearing parse errors.
Determine Pi builtin/adapter applicability without treating `mcp.json` as a universal legacy format or demanding an adapter cache.
- [ ] Evaluate compatibility only where version evidence exists. If upstream supplies no supported schema/diagnostic contract, return Unknown with safe manual inspection guidance.
Native configuration commands remain read-only diagnostics, never executable repair proposals. Minimal Herdr sections are not inherently old.
- [ ] Assert serialized findings, diagnostics, previews, and facts contain none of the synthetic MCP server name, URL, command, header, or token sentinels.
Register six IDs. Run the focused tests and global gate; expect PASS. Commit: `fix: inspect configuration using documented defaults and evidence`.

### Task 2: Discover Pi packages and Superpowers source intent

**Files:** Create `resource_sources.go`, `resource_sources_test.go`; modify `pi_packages.go`, `superpowers.go`, `builtin.go`;
create `pi_packages_test.go`, `superpowers_test.go`, and synthetic resource declarations/Git fixtures.

**Interfaces:** Produce `ResourceSource`, `DeclaredResources`, `discoverPiPackages`, `checkPiPackageSources`,
`discoverSuperpowers`, and `checkSuperpowersSource`. The discovery/evaluation signatures match the roadmap.

- [ ] Add `TestDeclaredResourceScope`, `TestPiPackageSourceKinds`, and `TestSuperpowersRequestedRef`.
Cover user/project-relative roots, explicit local roots, npm version pins, tags, commit pins, branch tracking, Git worktrees, dirty/untracked files, divergence, and offline remotes.

```go
if pinnedFinding.Outcome == OutcomeAttention && hasAutomaticAction(pinnedFinding) {
    t.Fatal("pin was interpreted as permission to advance")
}
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestDeclaredResource|TestPiPackageSource|TestSuperpowersRequestedRef'`; expect FAIL.
- [ ] Parse supported declarations without loading packages. Resolve local paths against their owning scope and discover relevant harness roots, not every repository on disk.
Read current branch, full dirty status, local revision, and remote requested ref with bounded read-only operations. Set `GIT_OPTIONAL_LOCKS=0` for supported status inspection.
- [ ] Distinguish current, pinned, unknown ancestry, ahead/divergent, and available updates using evidence. Do not fetch to resolve ancestry during audit.
Unknown ancestry produces manual guidance. Permit automatic advancement only through a supported manager/native updater with fresh prerequisites and known scope.
- [ ] Register both IDs and test no command contains `fetch`, `pull`, or package update during inspection.
Run the focused tests and global gate; expect PASS.
Commit: `feat: inspect declared Pi and Superpowers resources`.

### Task 3: Inspect Herdr integration targets and plugin instances

**Files:** Modify `herdr.go`, `herdr_plugins.go`, `builtin.go`; extend `herdr_test.go`; create `herdr_plugins_test.go` and registry fixtures.

**Interfaces:** Produce `checkHerdrIntegrationTargets`, `discoverHerdrPlugins`, and `checkHerdrPluginSources` with roadmap signatures.
Discovery supplies distinct plugin instance IDs from the owning registry path and validated resource identity.
Each plugin finding has its own FindingKey; a registry-scope instance remains discoverable when the registry is empty.

- [ ] Add `TestHerdrIntegrationApplicability`, `TestHerdrPluginRegistryLocation`, `TestHerdrPluginPartialResults`, and `TestHerdrPluginPinnedSources`.
Cover absent Pi/OpenCode, unsupported target output, missing/invalid registry, local plugins, moving branches, pinned refs, and one failed remote.

```go
if len(findings) != 2 || findings[0].Outcome == findings[1].Outcome {
    t.Fatal("partial plugin results collapsed into one unknown summary")
}
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestHerdrIntegration|TestHerdrPlugin'`; expect FAIL.
- [ ] Resolve registry relative to Herdr's documented configuration directory, including XDG and explicit overrides.
Read supported target/status formats without launching agents. Unsupported formats yield Unknown rather than inferred current hooks.
- [ ] Evaluate plugins individually with concurrency capped at eight. Unsupported sources are explicit, not silently current.
Automatic direct-argv proposals preserve validated owner/repo/subdirectory/ref and known installer side effects. No concatenated `&&` commands.
- [ ] Register both IDs and verify limits/cancellation preserve completed siblings. Run the focused tests and global gate; expect PASS.
Commit: `feat: inspect Herdr targets and plugins independently`.

### Task 4: Parse skills and close the complete registry

**Files:** Modify `skills.go`, `builtin.go`, `go.mod`, and `go.sum` only if dependency research justifies a parser;
create `skills_test.go` and synthetic `testdata/resources/skills/` fixtures; extend `builtin_test.go`.

**Interfaces:** Produce `discoverSkills`, `checkSkillMetadata`, `ParseSkillMetadata(raw []byte) (SkillMetadata, error)`,
and `ValidateSkillMetadata(metadata SkillMetadata) error`. SkillMetadata contains only Name and Description strings. Traversal uses bounded local helpers, not a universal filesystem service.

- [ ] Research maintained YAML parsing packages through `golang-pkg-go-dev`. Record exact chosen module/version, license, and rationale before dependency approval.
Do not use a regex as a YAML parser. Resolve the parser choice as part of this task's review before adding the dependency.
- [ ] Add `TestSkillFrontmatter`, `TestSkillDescriptionBoundary`, `TestSkillTraversalBounds`, and `TestBuiltinRegistryAllChecks`.
Cover quoted, folded/literal multiline Unicode, missing metadata, duplicate YAML keys, unreadable files, cycles, declared sibling roots, and external-link escapes.

```go
metadata := SkillMetadata{Name: "example-skill", Description: strings.Repeat("界", 1024)}
if err := ValidateSkillMetadata(metadata); err != nil { t.Fatal(err) }
metadata.Description += "界"
if err := ValidateSkillMetadata(metadata); err == nil { t.Fatal("accepted 1025 characters") }
```

- [ ] Run `make test TEST_PKG=./internal/doctor TEST_RUN='TestSkill|TestBuiltinRegistryAllChecks'`; expect FAIL.
- [ ] Parse frontmatter only within the 2 MiB file bound. Discover supported consumer roots and explicit roots, expand symlinks only within declared roots,
track canonical directories for cycles, and report scan limits (10,000 files/depth 32). Validate description at 1-1024 Unicode characters,
plus the Agent Skills name/directory rules from the official specification. Invalid metadata is Attention, unreadable evidence is Unknown.
- [ ] Register `skills`; assert exact set equality with the spec's 21 IDs, no duplicates, and stable order. A synthetic extra definition must work without engine/formatter/store edits.
- [ ] Run the focused tests and global gate; expect PASS. Commit: `feat: inspect skill metadata and complete dynamic check registry`.

## Stage exit

The new read-only engine covers every check ID. No heuristic requires optional files, equates changelog state with schema,
or silently reports unknown sources current. Review the complete synthetic report before Plan 4.
