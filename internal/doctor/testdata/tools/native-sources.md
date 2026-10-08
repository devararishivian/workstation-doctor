# Native version evidence

These notes describe supported read-only evidence. The tests create synthetic receipts and application bundles in temporary directories.

Herdr and Starship formula installations use validated `INSTALL_RECEIPT.json` fields and the current keg revision directory. This is installation-source evidence, not a guarantee that an executable reports the same version. A receipt timestamp describes the current revision, not the first installation.

Ghostty application bundles use XML `Info.plist` values `CFBundleIdentifier` and `CFBundleShortVersionString`. The identifier must be `com.mitchellh.ghostty`. Binary plists and unknown layouts are unsupported. Both system and user Applications directories are candidates on macOS. An application bundle alone does not prove Homebrew cask ownership.

No native version subprocess is currently supported by these new providers. The inspected Starship `main.rs` initializes logging and schedules log cleanup before argument parsing. Thus even `--version` does not establish read-only behavior. Herdr startup behavior was not verified for all supported versions. Ghostty v1.2.3's version handler prints `Ghostty <version>`, but its complete startup path was not verified. The providers retain known metadata and report unavailable versions for other layouts rather than run an unsafe command.

Official sources:

- https://github.com/herdrdev/herdr
- https://github.com/starship/starship/blob/master/src/main.rs
- https://ghostty.org/docs/install/binary
- https://github.com/ghostty-org/ghostty/blob/v1.2.3/src/cli/version.zig

GitHub release metadata uses `tag_name`, `prerelease`, `draft`, and `html_url`. Release-note links must point to the same official repository. Remote failures retain the local version evidence. An upstream release does not establish a Homebrew update decision.
