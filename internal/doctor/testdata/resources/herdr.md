# Synthetic Herdr resource evidence

The Herdr plugin registry is stored as `plugins.json` next to Herdr's configuration file. In supported source metadata, the registry has plugin identity/root fields and a nested `source` record; GitHub sources carry owner, repository, optional subdirectory/ref, resolved commit, and managed checkout path. Tests use synthetic registry entries and temporary Git metadata only.

Sources pinned for implementation research:

- Registry location and JSON loading: `https://github.com/herdrdev/herdr/blob/ca1af383/src/persist/plugin_registry.rs`.
- Registry data fields: `https://github.com/herdrdev/herdr/blob/ca1af383/src/api/schema/plugins.rs`.
- Integration status target/version behavior: `https://github.com/herdrdev/herdr/blob/ca1af383/src/integration/registry.rs`; public command reference `https://github.com/herdrdev/herdr/blob/master/docs/versions/0.8.0/website/src/content/docs/session-state.mdx`.
- Plugin install semantics and effects: `https://github.com/herdrdev/herdr/blob/master/docs/versions/0.9.0/website/src/content/docs/plugins.mdx`.

Plugin manifests and plugin code are not opened or run. Local links are not remote update targets. For GitHub entries, audit reads bounded local Git status including tracked, untracked and ignored paths, with optional locks, hooks, global/system config, fsmonitor, and lazy fetch disabled; it then consults bounded public GitHub branch/tag/compare metadata. It never fetches into plugin checkouts. A moving branch yields an automatic direct-argv proposal only when the local checkout is clean and GitHub proves a simple upstream-ahead relation. Pinned commits/tags, dirty or unreadable checkouts, divergence, unsupported sources, and unavailable metadata do not become update proposals.

Herdr's installer can replace its managed plugin checkout and execute the plugin's declared build commands as the current user. Proposals disclose both effects and require explicit confirmation; the inspector does not invoke the installer. Targeted tests inject command and HTTP results, including one failing remote while preserving a successful sibling.

Integration status uses the documented `herdr integration status` read-only command. Only supported target/state text is interpreted; unexpected or incomplete output is Unknown. Integration installation is not proposed automatically because its writes affect the target product's configuration and exact effects vary by target.
