# Synthetic resource evidence

Configuration and package tests construct temporary JSON, TOML and Git metadata. No fixture is copied from a live workstation. MCP sentinels are intentionally synthetic and must never appear in serialized findings.

Supported configuration evidence:

- Pi configuration and package declarations at upstream commit `eb326d265ae0b88489a6d10319307780df827cdf`: `packages/coding-agent/docs/configuration.md` and `packages/coding-agent/docs/packages.md`.
- Herdr configuration locations and optional defaults at upstream commit `a124eed73c1f911ddf89a6ac5b2f7ab70d76f5c2`: `src/config/io.rs`.
- https://ghostty.org/docs/config: configuration is optional; `config.ghostty` is supported from 1.2.3, with XDG paths before macOS-specific paths.
- https://git-scm.com/docs/git-status: porcelain status, untracked files, and optional-lock suppression. https://git-scm.com/docs/git documents `GIT_CONFIG_PARAMETERS`, configuration overrides and `GIT_NO_LAZY_FETCH`.

Pi source parsing supports npm names with optional version constraints, public GitHub repository declarations with optional refs, and absolute or explicitly relative local paths. Relative paths use the declaring settings directory. Other source forms are unsupported, not silently current. Project declarations are observations of the selected project, not assertions of Pi project trust or effective package loading.

Explicit ref constraints remain conservative: tags, commits and unclassified requested refs disable advancement. The Git evaluator can inspect a separately established tracking branch, but declaration parsing does not guess whether a ref names a branch or tag. Automatic resource proposals are unavailable.

Git tests inject command results and HTTP responses. They exercise clean, dirty/untracked, offline, constrained refs, traversal rejection, explicit root precedence, executable configuration rejection, and worktree metadata resolution. No Git command touches a real user checkout. Inspected repositories with filters, includes, submodules, partial clones or worktree configuration are refused. Global/system configuration, fsmonitor and hooks are disabled for supported local inspection. Remote observations use bounded public HTTP; no remote transport, fetch or pull runs.

Unsupported native configuration semantics remain Unknown. JSON/TOML checks describe static syntax only; no extension, configuration expression, native diagnostic or MCP server runs.
