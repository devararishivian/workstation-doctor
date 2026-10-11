# Workstation Doctor — Comprehensive UX, Wording, and Mechanism Review

> **Status:** Draft for owner review. No code changes are included.  
> **Date:** 2026-10-11  
> **Scope:** User journey redesign, complete wording rewrite, Superpowers update mechanism fix, Herdr plugin detection
> fix, unified fix/manual flow.

---

## A. Current-State Assessment

### A.1 User Journey and Navigation Structure (Current)

The application has **11 distinct screen modes** managed by the `mode` state field in `menu_state.go`:

| Mode             | Purpose                          | Entry Points                               |
|------------------|----------------------------------|--------------------------------------------|
| `menu`           | Main menu with 5 items           | App start, Esc/b from other screens        |
| `running`        | Spinner during audit             | Pressing `1`, or auto-audit before `2`/`3` |
| `fixing`         | Spinner during fix execution     | Confirming `y` on preview                  |
| `results`        | Dashboard table of 18 components | After audit completes                      |
| `detail`         | Single component inspection      | Enter/d from results                       |
| `manual`         | Ordered manual steps list        | Pressing `2`                               |
| `preview_action` | Confirmation before fix          | Pressing `3`, or `3`/`p` from detail       |
| `action_done`    | Fix execution result             | After fix completes                        |
| `no_fixes`       | "Nothing to fix" notice          | When `3` finds no automatic actions        |
| `history`        | Past action records table        | Pressing `4`                               |
| `history_detail` | Single history record            | Enter/d from history                       |

Two additional modes exist for legacy database migration (`preview_migration`, `migration_done`).

**Current user journey flow:**

```
menu ──1──▶ running ──▶ results ──Enter──▶ detail
  │                       │                  │
  ├──2──▶ manual ◄────────┤──2───────────────┤
  │                       │                  │
  ├──3──▶ preview_action ◄┤──3/p─────────────┤
  │         │y                               
  │         ▼                                
  │      fixing ──▶ action_done ──▶ results (re-audit)
  │                                          
  ├──4──▶ history ──Enter──▶ history_detail  
  │                                          
  └──0/q──▶ exit                             
```

### A.2 UX Problems Identified

#### Problem 1: Menu screen is unnecessary for returning users

The main menu (screenshot 1) sits between the user and any useful information. A user who launches the app must always
press `1` before seeing any results. The menu provides no value that a shortcut bar on the results screen would not.

**Evidence:** `menu_state.go:87-92` — the menu items are `check`, `manual`, `fix`, `history`, `quit`. All of these are
accessible as shortcuts from the results screen.

#### Problem 2: Automated fix and manual steps are separated

Automated fix (menu `3`) and manual steps (menu `2`) are separate screens with separate entry points. A user who sees an
UPDATE status for "opencode" in the results table must:

1. Press `d` to view the detail (screenshot 4) — sees "[Action] Proposed Actions" with `[Automatic]` tag.
2. Press `Esc` to go back.
3. Press `2` to see manual steps (screenshot 5) — sees the same information reformatted as a manual command.
4. Press `Esc` to go back.
5. Press `3` to run the automatic fix (screenshot 6) — sees the same information reformatted as a confirmation preview.

The same information appears three times in three different formats. The user must mentally connect them.

**Evidence:**

- `menu_views.go:319-437` (`renderManual`) builds manual step cards from all non-OK findings.
- `menu_state.go:302-357` (`prepareAutomaticFixes`) collects all automatic actions from findings.
- `menu_details.go:179-257` (`componentDetail`) shows both attention items and proposed actions in the detail view.

#### Problem 3: No component-level selection for batch fixes

When a user presses `3` from the results screen, `prepareAutomaticFixes("")` collects ALL automatic actions across ALL
components. The user sees a batch preview (screenshot 6) but cannot deselect individual components.

When pressing `3`/`p` from the detail screen, `prepareFirstAction()` filters to the selected component only. This
difference is not communicated.

**Evidence:** `menu_state.go:302` — `prepareAutomaticFixes(filterComponent string)` uses an empty string to mean "all"
and a checkID to mean "one component".

#### Problem 4: Inconsistent navigation shortcuts

| Context     | `b` goes to      | `Esc` goes to    | `2` does        | `3` does             |
|-------------|------------------|------------------|-----------------|----------------------|
| results     | menu             | menu             | manual (global) | fix (global)         |
| detail      | results          | results          | manual (global) | fix (this component) |
| manual      | menu             | menu             | —               | fix (global)         |
| action_done | re-audit→results | re-audit→results | —               | —                    |

The `2` key from the detail screen jumps to the global manual steps view, losing the component context. The `3` key from
the detail screen applies only to the current component. This asymmetry is confusing.

**Evidence:** `menu.go:116-134` (detail keymap) — `3`/`p` calls `prepareFirstAction()` (component-scoped), but `2` calls
`runAction("manual")` (global).

#### Problem 5: Manual steps screen shows a tip to use menu [3]

Screenshot 5 shows:
`(Tip: You can apply this fix automatically by selecting 'Run automatic fix' [3] from the main menu)`. This sends the
user back to the menu, then to a different screen, to do what the manual steps screen could offer directly.

**Evidence:** `menu_views.go:410-414` — conditional tip text rendered for items where `item.Automatic` is true.

#### Problem 6: "Ordered Manual Steps" title is opaque

The term "Ordered Manual Steps" does not tell the user what it contains. A user who just wants to know what needs
updating must guess that this menu item shows update instructions.

#### Problem 7: Wording inconsistencies across screens

| Location         | Current Wording                                                                                    | Problem                                                                                 |
|------------------|----------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------|
| Menu subtitle    | "Developer Workstation Reliability & Safe Maintenance Engine"                                      | Marketing language, not useful                                                          |
| Menu item 2      | "Show ordered manual steps"                                                                        | "Ordered" adds no meaning; "manual steps" is vague                                      |
| Menu item 3      | "Run automatic fix"                                                                                | Singular "fix" when multiple actions may run                                            |
| Results footer   | "3: Run Automatic Fix (1 pending)"                                                                 | The parenthetical count uses different format from summary pills                        |
| Detail status    | "Status: Attention / Update Required"                                                              | "Attention / Update Required" is two labels at once                                     |
| Detail section   | "[!] Items Requiring Attention"                                                                    | "[!]" prefix is inconsistent with other section headings                                |
| Detail section   | "[Action] Proposed Actions"                                                                        | "[Action]" prefix is inconsistent                                                       |
| Manual step      | "Why: A newer upstream release exists. The update method depends on installation ownership."       | "The update method depends on installation ownership" is internal implementation detail |
| Preview title    | "Confirmed Action Preview"                                                                         | The action has NOT been confirmed yet; it is awaiting confirmation                      |
| Preview prompt   | "[?] Execute this action? [Press 'y' to confirm · Press 'n' / Esc / any other key to decline]"     | Redundant instruction after already clear y/n choice                                    |
| No fixes message | "Automatic Maintenance Notice"                                                                     | "Notice" adds no meaning                                                                |
| No fixes body    | "No automatic fixes are pending. All components are current, or require manual maintenance steps." | "or require manual maintenance steps" is a guess; could be neither                      |
| History title    | "Maintenance Action History"                                                                       | "Maintenance Action" is redundant with context                                          |
| History detail   | "Execution & Outcome (Showing 1-8 of 10 · Use ↑/↓ to scroll)"                                      | Pagination hint inside a section header                                                 |
| Action result    | "Action Result & Verification"                                                                     | Acceptable but verbose                                                                  |

#### Problem 8: `pi install` used for Superpowers update instead of `pi update`

The current update command for git-based Pi packages is:

```go
// resource_git.go:261
Label: fmt.Sprintf("Run pi install git:%s", r.Identity),
Command: Command{
Executable: piExe,
Args: []string{"install", "git:" + r.Identity},
},
```

However, `pi` has a dedicated `update` command:

```
pi update <source>       Update one package
pi update --extensions   Update installed packages only
```

The `pi install` command is designed for first installation. When the package is already installed, `pi install`
behavior depends on the package manager's internal state. For git packages, `pi install` appears to clone into
`~/.pi/agent/git/<path>`. If the directory already exists with a valid clone, the install command may not pull new
changes because it considers the installation already present.

**Verified evidence:**

- The user's reflog shows: `bb92a77 HEAD@{2026-10-11 15:06:36 +0700}: clone: from https://github.com/obra/superpowers` —
  the only entry is a fresh `clone`, confirming the user had to delete and reinstall.
- `pi update --help` documents `pi update <source>` and `--extension <source>` for updating individual packages, and
  `--force` for reinstallation.
- The current code at `resource_git.go:261` and `resource_sources.go:301` both use `pi install` rather than `pi update`.

**Root cause hypothesis (high confidence):** `pi install git:github.com/obra/superpowers` does not update an existing
checkout. The correct command for updating is `pi update git:github.com/obra/superpowers` or
`pi update --extension git:github.com/obra/superpowers`. The user had to manually delete the directory because
`pi install` treated the existing directory as "already installed."

#### Problem 9: Herdr plugin discovery checks all plugins but may appear to find them one at a time

**Investigation of the code:**

The `discoverHerdrPlugins` function in `herdr_resources.go:60-105` reads the entire `plugins.json` registry and creates
an Instance for each valid entry. It does NOT stop after finding one.

The `checkHerdrPluginSources` function in `herdr_resources.go:223` is called once per Instance (per plugin). Each call
makes independent GitHub API requests: one to resolve the default branch (if no ref specified), one to get the branch
HEAD SHA, and one to compare commits.

The `AuditEngine` in `audit_engine.go` runs checks concurrently. Each plugin instance gets its own goroutine.

**Hypothesis (medium confidence):** The user's experience of finding updates one at a time was likely caused by one of
these factors:

1. **GitHub API rate limiting:** The GitHub REST API has a 60 requests/hour limit for unauthenticated requests. Each
   plugin check makes 2-3 API calls. With 3 plugins, that is 6-9 calls. If the user had already consumed some of their
   rate limit, some calls may have failed silently, causing those plugins to remain at `OutcomeUnknown` rather than
   `OutcomeAttention`.

2. **Timing of upstream changes:** The second plugin's upstream change may genuinely not have been available (pushed) at
   the time of the first check.

3. **Build/registry state change after first update:** The `herdr plugin install` command modifies the `plugins.json`
   registry. After updating one plugin and running `herdr server reload-config`, the registry file changes. A fresh
   audit reads the new registry. If the first plugin's `resolved_commit` was updated during install, the second audit
   sees the new registry state and may detect different updates.

**Evidence from the registry:**

```
annotate: e3ca7e88ada0
herdr.auto-title: 67fa3e24f319
hhdebb.herdr-radar: dd42772da4b8
```

All three plugins are present and readable. The discovery function processes all of them.

**Missing evidence:** I cannot verify GitHub API response codes from the original audit session. The most likely
explanation is rate limiting or timing.

#### Problem 10: Herdr plugin update does not include reload-config step

The `herdrPluginUpdateProposal` function at `herdr_resources.go:350-380` generates an action with exactly one step:

```go
p.Steps = []CommandStep{{
Label: "Install the selected Herdr plugin source",
Command: Command{Executable: executable, Args: args},
}}
```

It does NOT include a `herdr server reload-config` step. The user reported having to run `reload-config` manually after
updating plugins. This is a missing post-action requirement.

**Evidence from Herdr CLI:**

```
herdr server reload-config    # documented command
```

#### Problem 11: History detail shows empty version fields

Screenshot 8 shows an action record with:

- `Installed Version` — empty
- `Target Version` — `ed03c22760d0de79bda2f6f1c216d2f1f3910e99`
- `Observed Version` — empty

The commit SHA as a "Target Version" is technically correct but not meaningful to most users. "Installed Version" is
empty because the Herdr plugin update action does not capture the currently installed commit SHA in the
`ActionStart.InstalledVersion` field (it stores this in `Preconditions` instead).

---

## B. Proposed User Journey and Navigation

### B.1 Core Principle: Results First

The main menu is eliminated. The app launches directly into an audit. When the audit completes, the user lands on the
results dashboard. All actions are accessible from the results screen or the component detail screen.

### B.2 New Screen Structure (7 modes, down from 11)

| Mode             | Purpose                                     | Replaces                    |
|------------------|---------------------------------------------|-----------------------------|
| `checking`       | Spinner during audit                        | `running`                   |
| `results`        | Dashboard table with summary and action bar | `results` + `menu`          |
| `detail`         | Component detail with inline actions        | `detail` + partial `manual` |
| `select_fixes`   | Checkbox list for batch fix selection       | NEW                         |
| `confirm`        | Preview before execution                    | `preview_action`            |
| `executing`      | Progress during fix execution               | `fixing`                    |
| `result`         | Execution result                            | `action_done` + `no_fixes`  |
| `history`        | Past action records                         | `history`                   |
| `history_detail` | Single history record                       | `history_detail`            |

**Removed:** `menu`, `manual` (merged into `detail`), `no_fixes` (merged into `result`), `fix_done` (merged into
`result`).

### B.3 New Navigation Flow

```
Launch ──▶ checking ──▶ results
                         │
                         ├──Enter──▶ detail ──────────────────┐
                         │             ├──f──▶ confirm ──y──▶ executing ──▶ result ──▶ results
                         │             │        (single component fix)
                         │             └──back──▶ results
                         │
                         ├──f──▶ select_fixes ──▶ confirm ──y──▶ executing ──▶ result ──▶ results
                         │        (multi-component selection)
                         │
                         ├──r──▶ checking ──▶ results  (re-check)
                         │
                         ├──h──▶ history ──Enter──▶ history_detail
                         │
                         └──q──▶ exit
```

### B.4 What Changed and Why

**Eliminated: Main menu**

- *Reason:* The menu adds one mandatory keypress before any useful information. Every action from the menu is reachable
  from the results bar.
- *Trade-off:* Users who preferred to read the menu labels before acting will need to read the footer bar instead. The
  footer bar must be self-explanatory.

**Eliminated: Separate manual steps screen**

- *Reason:* Manual steps duplicate information from the component detail view. The detail view already shows the same
  proposed actions. Adding a copyable command and documentation link directly into the detail is more useful than
  sending users to a separate screen.
- *Trade-off:* Users cannot see all manual steps for all components in one scrollable list. However, the results table
  already shows which components need attention, and users can navigate to each detail individually. A "needs attention"
  filter on the results screen is a potential future enhancement.

**Added: Component selection screen (`select_fixes`)**

- *Reason:* When multiple components have automatic fixes, users need to choose which ones to apply. The current design
  forces all-or-nothing (from results) or one-at-a-time (from detail).
- *Implementation:* A checkbox list showing each pending fix with its component, action label, and target version.
  Toggle with Space, confirm selection with Enter.

**Merged: `no_fixes` into `result`**

- *Reason:* Two separate screens for "nothing to fix" and "fix completed" is unnecessary. The result screen can display
  either case.

### B.5 Detailed Screen Descriptions

#### Results Screen

**Header:** `Workstation Check Results` + `18 components checked` (right-aligned)

**Table:** Same columns as current, but with improved labels:

- `COMPONENT` | `INSTALLED` | `LATEST` | `STATUS` | `SUMMARY`

**Status values:**

- `OK` (green) — current version or valid configuration
- `UPDATE` (yellow) — a newer version is available
- `UNKNOWN` (red) — check could not determine status
- `N/A` (dim) — component not installed or not applicable

**Summary bar:** `OK: 16 · Update: 2 · Unknown: 0`

**Footer:** `[Enter: View details · f: Fix updates · r: Re-check · h: History · q: Quit]`

When no updates are pending, the footer shows: `[Enter: View details · r: Re-check · h: History · q: Quit]`

#### Detail Screen

**Header:** `Component: opencode` + `workstation-doctor` (right-aligned)

**Status card:**

```
Check: opencode  Status: Update available
```

**Update card** (shown only when status is UPDATE):

```
Update Available
  opencode-ai: 1.18.35 → 1.19.0
  Source: official npm registry

  Fix automatically (f)  |  Manual command:
  $ /opt/homebrew/opt/node@24/bin/npm install --global --prefix /opt/homebrew opencode-ai@1.19.0
  Documentation: https://github.com/anomalyco/opencode
```

Both the automatic fix trigger AND the manual command are visible in the same card. The user does not need to visit a
separate screen.

**Evidence card:**

```
Evidence
  Installed version    1.18.35  [global package.json]
  Upstream latest      1.19.0   [official npm registry latest tag]
```

**References card:**

```
References
  Official project     https://github.com/anomalyco/opencode
```

**Footer:** `[Esc/b: Back · f: Fix this component · ↑/↓: Scroll · q: Quit]`

When no automatic fix is available: `[Esc/b: Back · ↑/↓: Scroll · q: Quit]`

#### Select Fixes Screen (NEW)

**Header:** `Select Updates to Apply`

**List:**

```
  [x] opencode: Update opencode-ai to 1.19.0
  [x] npm-outdated-g: Update cc-safety-net to 2.6.4
  [ ] herdr-plugins: Update hhdebb/herdr-radar (manual review recommended)
```

Items with `ActionAutomatic` are pre-selected. Items with `ActionManual` are listed but unchecked and labeled "(manual
review recommended)".

**Footer:** `[Space: Toggle · Enter: Apply selected · a: Select all · n: Deselect all · Esc: Cancel · q: Quit]`

#### Confirm Screen

**Header:** `Confirm Update`

**Single action:**

```
Action: Update opencode-ai to 1.19.0
Component: opencode
Reason: Package opencode-ai is outdated (1.18.35 → 1.19.0)

Steps:
  1. Install the updated package in its current prefix

Side effects:
  • Downloads packages and dependencies into the selected global prefix.
  • Runs package lifecycle scripts and changes executable links.
  • Updates npm cache and can require manual permission repair; no elevation is requested.
```

**Multiple actions:**

```
Batch Update: 2 actions across 2 components
  1. [opencode] Update opencode-ai to 1.19.0
  2. [npm-outdated-g] Update cc-safety-net to 2.6.4

Side effects:
  • Downloads packages and dependencies into the selected global prefix.
  • Runs package lifecycle scripts and changes executable links.
```

**Prompt:** `Apply these updates? [y: Yes · n/Esc: Cancel]`

---

## C. Proposed UI Interaction Model — Example Flows

### C.1 One component that can be updated automatically

1. App launches → checking spinner → results table shows `opencode` with `UPDATE` status.
2. User presses `Enter` on `opencode` → detail screen shows "Update Available" card with both automatic fix trigger and
   manual command.
3. User presses `f` → confirm screen shows the single action.
4. User presses `y` → executing spinner → result screen shows "Completed, Verified."
5. User presses `Enter` → app re-checks, returns to results with `opencode` now showing `OK`.

### C.2 Multiple components updated at once

1. Results table shows `opencode` and `npm-outdated-g` with `UPDATE`.
2. User presses `f` from results → select fixes screen lists both with checkboxes, both pre-selected.
3. User presses `Enter` → confirm screen shows batch of 2 actions.
4. User presses `y` → executing spinner shows progress `1/2`, then `2/2` → result screen shows "2/2 completed."
5. User presses `Enter` → re-check → results.

### C.3 Component that only supports manual action

1. Results shows `superpowers` with `UPDATE`.
2. User presses `Enter` → detail shows "Update Available" card without an `f` shortcut.  
   The card shows: "Run this command to update: `pi update git:github.com/obra/superpowers`"
3. User opens a separate terminal, runs the command, returns, presses `r` to re-check.

### C.4 Automated fix that fails

1. User selects `opencode` fix, confirms with `y`.
2. Execution fails (e.g., network error).
3. Result screen shows: "Update failed. The npm install command returned an error."  
   Below: "Manual command: `$ /opt/homebrew/.../npm install --global ...`"  
   The user can try the command manually without leaving the screen context.

### C.5 Automated fix that partially succeeds (batch)

1. User selects 3 fixes, confirms with `y`.
2. Result screen shows:
   ```
   2 of 3 updates completed.
     ✓ opencode: opencode-ai updated to 1.19.0
     ✓ npm-outdated-g: cc-safety-net updated to 2.6.4
     ✗ herdr-plugins: hhdebb/herdr-radar – command failed (exit code 1)
   
   Press Enter to re-check and return to results.
   ```

### C.6 Update that requires reload

After a Herdr plugin update completes:

```
Update completed. Herdr server must reload its configuration for the change to take effect.

Reload now? [y: Yes · n: Skip]
```

If `y`: the app runs `herdr server reload-config`, then shows the verification result.  
If `n`: the result screen notes "Reload skipped. Run `herdr server reload-config` to apply."

### C.7 Check finds no updates needed

1. Results table shows all components as `OK`.
2. Footer shows: `[Enter: View details · r: Re-check · h: History · q: Quit]`
3. No `f` shortcut appears because no fixes are pending.

---

## D. Complete Wording Proposal

### D.1 Vocabulary Standard

These terms are used consistently across the entire application. Each term has one meaning.

| Term           | Meaning                                        | Replaces                             |
|----------------|------------------------------------------------|--------------------------------------|
| check          | Read-only inspection of a component            | audit, inspection, status            |
| update         | A newer version available from upstream        | fix, maintenance, attention          |
| fix            | Apply an update automatically                  | automatic fix, automatic maintenance |
| component      | One tool, configuration, or package group      | integration, check, item             |
| installed      | The version or state on this computer          | current, local                       |
| latest         | The newest version from the upstream source    | upstream, remote                     |
| result         | The outcome of a check or fix                  | finding, outcome                     |
| manual command | A command the user runs in a separate terminal | manual steps, ordered manual steps   |

### D.2 Screen-by-Screen Wording Changes

#### Results Screen

| Element      | Current                                                                                              | Proposed                                                                      | Reason                                                             |
|--------------|------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------|--------------------------------------------------------------------|
| Title        | "Workstation Audit Results"                                                                          | "Workstation Check Results"                                                   | "Audit" implies formal evaluation; "check" is simpler              |
| Counter      | "18 components inspected"                                                                            | "18 components checked"                                                       | Consistency with "check" vocabulary                                |
| Column: NOTE | "NOTE"                                                                                               | "SUMMARY"                                                                     | "Note" implies supplementary; "Summary" is the primary description |
| Status pill  | "NEED UPDATE: 2"                                                                                     | "Update: 2"                                                                   | Remove "NEED"; the status name is self-explanatory                 |
| Status pill  | "UNKNOWN: 0"                                                                                         | "Unknown: 0"                                                                  | Lowercase for consistency                                          |
| Footer       | "[Enter / d: View Details · 3: Run Automatic Fix (1 pending) · 2: Manual Steps · b: Menu · q: Quit]" | "[Enter: View details · f: Fix updates · r: Re-check · h: History · q: Quit]" | Letter shortcuts replace numbers; no menu reference                |

#### Detail Screen (Component OK)

| Element       | Current                                            | Proposed                                            | Reason                                                    |
|---------------|----------------------------------------------------|-----------------------------------------------------|-----------------------------------------------------------|
| Status line   | "Check: pi  Status: OK"                            | "Check: pi · Status: OK"                            | Midpoint separator for readability                        |
| Section title | "Evidence & Diagnostics"                           | "Evidence"                                          | "Diagnostics" adds no information not already in the rows |
| Evidence row  | "Installed version    1.1.0 [global package.json]" | "Installed version    1.1.0  [global package.json]" | No change needed; format is clear                         |
| Section title | "Public References"                                | "References"                                        | "Public" is redundant; no private references exist        |

#### Detail Screen (Component UPDATE)

| Element       | Current                                                                                                                             | Proposed                                                                                              | Reason                                                                                      |
|---------------|-------------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------|
| Status line   | "Check: opencode  Status: Attention / Update Required"                                                                              | "Check: opencode · Status: Update available"                                                          | "Attention / Update Required" uses two terms; "Update available" is one clear statement     |
| Section title | "[!] Items Requiring Attention"                                                                                                     | "Update Available"                                                                                    | Remove bracket prefix; state what happened                                                  |
| Item text     | "opencode-ai\nA newer upstream release exists. The update method depends on installation ownership."                                | "opencode-ai: 1.18.35 → 1.19.0\nA newer version is available from npm."                               | Remove "installation ownership" jargon; show version transition; name the source            |
| Section title | "[Action] Proposed Actions"                                                                                                         | REMOVED (merged into Update Available card)                                                           | Reduce section count; actions belong with the finding                                       |
| Action item   | "Update selected global package [Automatic]\nA newer upstream release exists. The update method depends on installation ownership." | "Fix automatically: `npm install --global opencode-ai@1.19.0`\nOr run this command in your terminal." | Show the actual command; make automatic vs manual a presentation choice, not a status label |

#### Manual Steps Screen (REMOVED — merged into Detail)

The manual steps screen is replaced by inline information in the detail screen. The manual command and documentation
link appear directly in the "Update Available" card.

#### Confirm Screen

| Element             | Current                                                                                         | Proposed                                                         | Reason                                                         |
|---------------------|-------------------------------------------------------------------------------------------------|------------------------------------------------------------------|----------------------------------------------------------------|
| Title               | "Confirmed Action Preview"                                                                      | "Confirm Update"                                                 | The action is NOT confirmed yet; present tense                 |
| Action label        | "Action: Update selected global package"                                                        | "Action: Update opencode-ai to 1.19.0"                           | Name the actual package and version                            |
| Component           | "Component: opencode (npm-package)"                                                             | "Component: opencode"                                            | "(npm-package)" is internal type info                          |
| Reason              | "Reason: A newer upstream release exists. The update method depends on installation ownership." | "Reason: opencode-ai is outdated (1.18.35 → 1.19.0)"             | Remove jargon; state the version gap                           |
| Step                | "1. Install the selected package version in its proven prefix"                                  | "1. Install opencode-ai version 1.19.0 in the global npm prefix" | Name concrete values                                           |
| Side effects header | "Side Effects & Permissions:"                                                                   | "Side effects:"                                                  | Remove "Permissions" — these are side effects, not permissions |
| Prompt              | "[?] Execute this action? [Press 'y' to confirm · Press 'n' / Esc / any other key to decline]"  | "Apply this update? [y: Yes · n/Esc: Cancel]"                    | Shorter, clearer                                               |

#### Result Screen

| Element         | Current                                                                   | Proposed                                         | Reason                        |
|-----------------|---------------------------------------------------------------------------|--------------------------------------------------|-------------------------------|
| Title (success) | "Action Result & Verification"                                            | "Update Completed"                               | State the outcome             |
| Title (failure) | "Action Execution Failed"                                                 | "Update Failed"                                  | Simpler                       |
| Title (partial) | "Action Execution Incomplete"                                             | "2 of 3 Updates Completed"                       | Show the ratio                |
| Navigation hint | "Press Enter or 'b' to re-run audit and return to the results dashboard." | "Press Enter to re-check and return to results." | Shorter; "audit" → "re-check" |

#### No-Fixes State (merged into Result)

| Element | Current                                                                                            | Proposed                                                   | Reason                                 |
|---------|----------------------------------------------------------------------------------------------------|------------------------------------------------------------|----------------------------------------|
| Title   | "Automatic Maintenance Notice"                                                                     | "All Components Are Current"                               | State the fact, not the message type   |
| Body    | "No automatic fixes are pending. All components are current, or require manual maintenance steps." | "All components are up to date. No updates are available." | Remove the hedge about manual steps    |
| Hint    | "Select 'Show ordered manual steps' [2] from the menu..."                                          | REMOVED                                                    | No separate manual steps screen exists |

#### History Screen

| Element           | Current                      | Proposed                 | Reason                                   |
|-------------------|------------------------------|--------------------------|------------------------------------------|
| Title             | "Maintenance Action History" | "Update History"         | Shorter; consistent vocabulary           |
| Column: LABEL     | "LABEL"                      | "DESCRIPTION"            | "Label" is a programmer term             |
| Column: ACTION ID | "ACTION ID"                  | Truncated ID, or removed | Full action IDs are meaningless to users |

#### History Detail Screen

| Element        | Current                                                                  | Proposed                                                          | Reason                                        |
|----------------|--------------------------------------------------------------------------|-------------------------------------------------------------------|-----------------------------------------------|
| Title          | "Action Record: Review Herdr plugin update (update-herdr-plugin-179...)" | "Update Record: Herdr plugin hhdebb/herdr-radar"                  | Use the meaningful name, not the internal ID  |
| Section title  | "Execution & Outcome"                                                    | "Result"                                                          | Shorter                                       |
| Pagination     | "(Showing 1-8 of 10 · Use ↑/↓ to scroll)"                                | Show all rows; remove pagination in a detail view                 | Detail views should show complete information |
| Version fields | "Installed Version: (empty)"                                             | "Installed version: e3ca7e... (before update)" or hide if unknown | Empty fields are confusing                    |

#### Superpowers Update Wording

| Current                                                               | Proposed                                                                              |
|-----------------------------------------------------------------------|---------------------------------------------------------------------------------------|
| `Run pi install git:github.com/obra/superpowers`                      | `Run pi update git:github.com/obra/superpowers`                                       |
| "Updates the git package repository and reinstalls into Pi settings." | "Pulls the latest version from the remote repository and updates the local checkout." |

For automated fix steps:

```go
// Current:
Label: "Run pi install git:" + r.Identity
Args: []string{"install", "git:" + r.Identity}

// Proposed:
Label: "Update " + filepath.Base(r.Identity) + " via Pi package manager"
Args: []string{"update", "git:" + r.Identity}
```

#### Herdr Plugin Update Wording

| Current                                                 | Proposed                                                                                            |
|---------------------------------------------------------|-----------------------------------------------------------------------------------------------------|
| "Review Herdr plugin update" (ActionManual label)       | "Update Herdr plugin <owner/repo>"                                                                  |
| "Update Herdr plugin <target>" (ActionAutomatic label)  | "Update Herdr plugin <owner/repo> to latest"                                                        |
| Single step: "Install the selected Herdr plugin source" | Two steps: (1) "Install the latest version of <plugin>" (2) "Reload the Herdr server configuration" |

---

## E. Technical Findings and Recommendations

### E.1 Superpowers Update Mechanism — Root Cause

**Finding (high confidence):** The automated fix and manual steps both use `pi install git:<source>` to update an
existing git package. This command is designed for first installation, not for updating. The `pi update` command
(documented in `pi update --help`) is the correct mechanism.

**Recommendation:**

1. Change all git-based Pi package update actions to use `pi update git:<identity>` instead of
   `pi install git:<identity>`.
2. Change all npm-based Pi package update actions to use `pi update npm:<identity>` instead of
   `pi install npm:<identity>`.
3. Add a precondition check: if the git directory does not exist at all, fall back to `pi install` (first installation).
   If it exists, use `pi update`.
4. If `pi update` still fails for a stale checkout, the fallback manual instruction should explain: "If the update
   fails, run `pi update --force git:<identity>` to reinstall."

**What this does NOT require:** Deleting the parent directory. The `pi update` command, or `pi update --force`, is
designed to handle these cases.

### E.2 Herdr Plugin Detection — One-at-a-Time Behavior

**Finding (medium confidence):** The code correctly discovers and checks all plugins in the registry. The most likely
cause of the user's experience is GitHub API rate limiting for unauthenticated requests (60/hour). Each plugin check
makes 2-3 API calls, and other checks (tool version lookups, Superpowers branch comparison) also use the GitHub API.

**Recommendations:**

1. Add a diagnostic when a GitHub API call fails or returns a rate-limit response (HTTP 403/429). Surface this as an
   evidence fact so the user knows which checks were incomplete.
2. Consider accepting a `GITHUB_TOKEN` environment variable for authenticated API access (5000 requests/hour). This is a
   common pattern and does not require storing tokens.
3. In the results view, distinguish between "OK" (verified current) and "Unknown" (could not check) more clearly. The
   current behavior silently falls through to `OutcomeUnknown` when API calls fail.

### E.3 Herdr Plugin Update —Missing Reload Step

**Finding (verified):** The Herdr CLI documents `herdr server reload-config` as a command. Plugin updates modify the
plugin registry file. Without a reload, the running Herdr server does not pick up the changes.

**Recommendations:**

1. Add a second `CommandStep` to the Herdr plugin update action:
   `{Label: "Reload the Herdr server configuration", Command: {Executable: herdrExe, Args: []string{"server", "reload-config"}}}`.
2. In the verification step, check whether the reload succeeded.
3. In the manual steps, include the reload command as a separate step after the install.

### E.4 History Record Version Fields

**Finding:** The `ActionStart` struct stores `TargetVersion` as a string from the proposal's `TargetVersion.Value`. For
Herdr plugins, this is a commit SHA. The `InstalledVersion` field in `ActionStart` is not populated from the proposal's
preconditions.

**Recommendation:** When building the `ActionStart`, populate `InstalledVersion` from the proposal's preconditions (look
for a fact with label containing "Installed" or "revision").

---

## F. Revised Specification and Implementation Plan

### F.1 Scope Categories

**Mandatory changes:**

1. Fix `pi install` → `pi update` for all Pi package update actions (git and npm).
2. Add `herdr server reload-config` step to Herdr plugin update actions.
3. Merge automatic fix and manual steps into the detail screen.
4. Rewrite wording across all screens per Section D.

**Recommended changes:**

5. Eliminate main menu; launch directly into audit.
6. Add component selection screen for batch fixes.
7. Add GitHub API rate-limit detection and diagnostic.
8. Populate `InstalledVersion` in history records.
9. Show manual command inline in component detail.

**Optional/future changes:**

10. Accept `GITHUB_TOKEN` for authenticated API access.
11. "Needs attention" filter on results table.
12. Inline reload prompt after Herdr plugin updates (C.6 flow).

### F.2 Architecture Changes Required

**No new packages or dependencies.** All changes are within existing files:

| File                                  | Change                                                                                                                                                                                                                |
|---------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `menu.go`                             | Remove `menu` mode and its key bindings. Add `select_fixes` mode. Change shortcut keys to letters.                                                                                                                    |
| `menu_state.go`                       | Remove `menuItem` struct and `items` list. Add `selectFixes` state fields (checkbox list). Change `startAudit()` to be called on launch. Modify `prepareAutomaticFixes()` to accept a list of selected component IDs. |
| `menu_views.go`                       | Remove `renderMenu()` and `renderManual()`. Add `renderSelectFixes()`. Modify `renderDetail()` to include inline manual command. Modify `renderResults()` footer. Rewrite all wording strings.                        |
| `menu_details.go`                     | Modify `componentDetail()` to include the manual command text in the "Update Available" section.                                                                                                                      |
| `internal/doctor/resource_git.go`     | Change `install` → `update` in action steps. Add version-specific label.                                                                                                                                              |
| `internal/doctor/resource_sources.go` | Change `install` → `update` in npm action steps. Add version-specific label.                                                                                                                                          |
| `internal/doctor/herdr_resources.go`  | Add `reload-config` step to `herdrPluginUpdateProposal`. Add better error diagnostic for GitHub API failures.                                                                                                         |
| `internal/app/service.go`             | Populate `InstalledVersion` from proposal preconditions in `Apply()`.                                                                                                                                                 |

### F.3 Expected Behavior Summary

1. App launches → immediately starts checking → results screen.
2. Results screen shows all components with clear status. Footer shows contextual shortcuts.
3. `Enter` on a component → detail shows status, update info (if any), manual command (if any), evidence, and references
   in one screen.
4. `f` from detail → confirm screen for that component → `y` → execute → result → re-check → results.
5. `f` from results → select fixes screen (checkboxes) → `Enter` → confirm batch → `y` → execute → result → re-check →
   results.
6. `r` from results → re-check.
7. `h` from results → history.
8. Herdr plugin updates include reload-config step.
9. Pi package updates use `pi update` instead of `pi install`.

### F.4 Error Handling and Edge Cases

| Condition                                         | Behavior                                                                                                                  |
|---------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------|
| No internet                                       | Checks that need network show `Unknown` status with explanation: "Could not reach the update server."                     |
| GitHub rate limited                               | Evidence fact added: "GitHub API rate limit reached. Some checks are incomplete." Status remains `Unknown`.               |
| `pi update` command not found                     | Fall back to showing manual command only. No automatic fix offered.                                                       |
| `herdr server reload-config` fails                | Result shows: "Plugin installed, but the server did not reload. Run `herdr server reload-config` manually."               |
| User selects zero fixes                           | `Enter` on empty selection shows: "No updates selected." Returns to select screen.                                        |
| Preconditions changed between prepare and execute | Existing behavior preserved: "Preconditions changed. The update was not applied."                                         |
| Fix succeeds but verification fails               | Result shows: "The update command ran, but the check still reports an older version. Run a re-check or inspect manually." |

### F.5 Testing Strategy

1. **Unit tests:** Update existing `menu_state_test.go` and `menu_details_test.go` for new screen modes, wording, and
   checkbox state.
2. **Unit tests:** Update `resource_sources_test.go` and `herdr_resources_test.go` to verify `pi update` args and
   `reload-config` step.
3. **Integration test (manual):** Run the app on the owner's workstation to verify:
    - Direct launch into check.
    - Detail screen shows inline manual command.
    - `pi update` successfully updates a package.
    - Herdr plugin update includes reload.
4. **Wording review:** Grep all string literals in `menu_views.go`, `menu_details.go`, and `menu_state.go` for
   consistency with the vocabulary standard.

### F.6 Implementation Order and Dependencies

| Step                                      | Dependency       | Risk                                   |
|-------------------------------------------|------------------|----------------------------------------|
| 1. Fix `pi install` → `pi update`         | None             | Low — behavior change in two files     |
| 2. Add `herdr server reload-config` step  | None             | Low — additional step in one function  |
| 3. Rewrite all wording strings            | None             | Low — text-only change                 |
| 4. Eliminate main menu; launch into audit | None             | Medium — changes initial state machine |
| 5. Merge manual steps into detail screen  | Step 3 (wording) | Medium — restructures detail rendering |
| 6. Add select_fixes screen                | Step 4 (no menu) | Medium — new screen mode and state     |
| 7. Add GitHub rate-limit detection        | None             | Low — conditional diagnostic           |
| 8. Populate InstalledVersion in history   | None             | Low — data plumbing                    |

Steps 1-3 are independent and can be done in any order. Steps 4-6 should be done in sequence. Steps 7-8 are independent
of everything else.

---

## Appendix: Screenshot Analysis

I was able to confirm the screenshot contents through the screenshots embedded in the conversation:

1. **Screenshot 1 (15:16:12):** Main menu with 5 items. Shows "Last Audit: 324 OK · 2 Need Update · 0 Unknown". Confirms
   the menu screen exists and shows stale summary.

2. **Screenshot 2 (15:16:26):** Results dashboard. Shows 18 components, `opencode` and `npm-outdated-g` with UPDATE
   status. Footer: "[Enter / d: View Details · 3: Run Automatic Fix (1 pending) · 2: Manual Steps · b: Menu · q: Quit]".
   Note: "1 pending" despite 2 UPDATE components — because only opencode has an Automatic action; npm-outdated-g's
   action is counted separately.

3. **Screenshot 3 (15:17:00):** Detail view for `pi` (OK status). Shows Evidence & Diagnostics and Public References
   sections. Clean layout with boxed sections.

4. **Screenshot 4 (15:17:17):** Detail view for `opencode` (UPDATE status). Shows "[!] Items Requiring Attention" and
   "[Action] Proposed Actions" sections. Footer:
   "[Esc / b: Back to Results · 3 / p: Run Automatic Fix · 2: Manual Steps · ↑/↓: Scroll · q: Quit]".

5. **Screenshot 5 (15:17:42):** "Ordered Manual Steps" screen. Shows Step 1 of 1: [opencode] with the npm install
   command, documentation URL, and the tip about using menu [3].

6. **Screenshot 6 (15:17:51):** "Confirmed Action Preview" for opencode. Shows Action, Planned Steps, Side Effects.
   Prompt: "[?] Execute this action?"

7. **Screenshot 7 (15:18:02):** "Maintenance Action History". Shows 4 past actions including Herdr plugin updates and Pi
   package updates. All Completed/Passed.

8. **Screenshot 8 (15:18:17):** Action Record detail for a Herdr plugin update. Shows Execution & Outcome and Version
   Information sections. Target Version is a commit SHA. Installed Version and Observed Version are empty.
