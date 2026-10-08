# Capability evidence

The uv reader supports the structured receipt subset from uv 0.9.7. It reads the `tool.entrypoints` name, install path, and originating package. Ownership also requires an exact symlink match to the tool environment executable. Python distribution `METADATA` supplies the installed version. Directory traversal stays inside the selected tool environment and does not follow directory symlinks.

Sources:

- https://github.com/astral-sh/uv/blob/0.9.7/crates/uv-tool/src/receipt.rs
- https://github.com/astral-sh/uv/blob/0.9.7/crates/uv-tool/src/tool.rs
- https://docs.astral.sh/uv/reference/storage/
- https://github.com/oraios/serena

Receipt ownership does not establish that a Git source, index override, or version constraint can switch to the PyPI latest release. PyPI comparisons cover the shared semantic-version subset only. Other PEP 440 versions remain unknown. uv updates remain manual.

For Serena, an explicit `serena` location can select one `mcp.json` or `mcp-adapter.json` declaration file instead of an executable. Only aggregate capability counts leave the parser. Server names, commands, arguments, environment values, and parser errors do not leave the parser. No default MCP configuration is read in the tests.

Gortex preview safety is not inferred from its version string. The reviewed upgrade handler separates preview from execution, but complete startup safety for installed builds is not established. The new provider uses release HTTP metadata instead. Native upgrade guidance discloses configuration migration, daemon or service restart, and installer shell execution. It does not copy printed shell plans into executable proposals.

Gortex source: https://github.com/zzet/gortex/blob/e12965558e80c153805521d174efcbda2b0654d5/cmd/gortex/upgrade.go

`uv-receipt.toml` and `serena-METADATA.txt` are synthetic format examples. Test fixtures use temporary paths and never execute these files.
