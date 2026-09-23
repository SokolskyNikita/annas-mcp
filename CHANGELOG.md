# Changelog

## v0.0.10

- Refactor the archive client, mirror discovery, MCP/CLI services, and npm launcher into focused modules; remove obsolete helpers and configuration paths.
- Require an account cookie for archive requests and an API key for every download, including DOI downloads. Report missing configuration before making network requests.
- Improve DOI and search parsing, mirror fallback, cancellation, credential redaction, and error reporting.
- Verify download sizes and hashes, reject empty or HTML responses, sanitize filenames across platforms, and preserve existing files.
- Harden launcher archive extraction and checksum verification, with verified offline caching and Windows support.
- Move HTML and other test inputs into external fixtures; add regression, race, MCP integration, and launcher tests with an 80% total Go coverage gate.
- Document membership requirements, weekly manual cookie renewal, fictional usage examples, configuration, tool contracts, and development workflows.
- Validate Linux, macOS, and Windows in CI; gate immutable releases on passing checks and verify published assets and MCP startup on each platform.

For earlier versions, see the [GitHub release history](https://github.com/SokolskyNikita/annas-mcp/releases).
