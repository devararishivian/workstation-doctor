# Global inventory scope

npm package discovery reads only the selected Unix global root candidate: `{prefix}/lib/node_modules`. An explicit `NPM_CONFIG_PREFIX` takes precedence over the prefix inferred from the selected npm executable. npm configuration files are not evaluated, so an inferred prefix is a candidate, not proof of effective npm configuration. Project `package.json` files do not supply global inventory.

Each observed package has its own instance identity. Package metadata and exact binary linkage establish supported npm ownership. Automatic proposals select one version and one prefix with direct arguments. Package resources and tool-specific checks use the same maintenance target identity. Packages without confirmed binary linkage do not receive automatic proposals in this stage. Requested pins and prerelease channels disable automatic proposals.

Homebrew receipts describe the selected prefix's formula kegs. Published formula metadata supplies a separate reference value. It does not supply the installed manager's outdated decision, architecture compatibility, or local revision comparison. Casks and custom formula update decisions remain unsupported. No blanket upgrade proposal is generated.

Official sources:

- https://docs.npmjs.com/cli/v11/configuring-npm/folders
- https://docs.npmjs.com/cli/v11/commands/npm-outdated
- https://formulae.brew.sh/docs/api/
- https://formulae.brew.sh/formula/python@3.14
- https://formulae.brew.sh/formula/gtk+3

Formula basenames use a separate conservative allowlist from npm package names. The official formula pages establish that `@` version suffixes and `+` characters are valid. Synthetic tests retain those names without copying real receipts or relying on current published versions.

The tests use temporary global roots, unrelated synthetic project dependencies, injected pins, synthetic release responses, and offline failures. No real manager, project configuration, or maintenance command runs.
