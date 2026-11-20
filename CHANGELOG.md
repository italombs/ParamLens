# Changelog

All notable changes to ParamLens. The format follows [Keep a Changelog](https://keepachangelog.com/)
and the project uses [semantic versioning](https://semver.org/). Downloads of every version are on the
[Releases](https://github.com/italombs/ParamLens/releases) page.

## [Unreleased]

## [1.3.1] - 2026-11-20

### Changed
- Sample data, input placeholders, default table name and documentation now use a fictitious
  orders/customers/invoices domain.

### Removed
- The historical hand-written analysis query (`docs/history/manual-query.sql`).

## [1.3.0] - 2026-11-20

### Changed
- Repository reorganized following Go conventions: `cmd/paramlens`, `internal/engine`,
  `internal/source`, `internal/server`, `web`, `scripts`, `docs`. The executable and the HTML page are
  unchanged.
- README focused on users; build and architecture notes moved to `docs/development.md`, plus a user
  guide, `CONTRIBUTING.md`, `SECURITY.md` and issue/pull request templates.

## [1.2.0] - 2026-11-20

### Added
- The file reader accepts a CSV/TXT with a header row (for example a `SELECT *` of the event table
  saved by SSMS): separator detected, columns matched by name, quoted fields with commas or line breaks.

### Changed
- Export guide: default timestamp column `timestamp (UTC)` and a note on the SSMS option that quotes
  CSV fields.

## [1.1.0] - 2026-11-20

### Added
- `ParamLens.html`: standalone offline version. Export the events with bcp or SSMS (the page builds the
  query and the command) and drop the file onto the page; the analysis runs in the browser.
- The `.exe` can also open an exported file.
- Interface in English by default, with Portuguese (pt-BR) available from the header.

### Changed
- Documentation in English.

## [1.0.0] - 2026-11-20

### Added
- First release: `ParamLens.exe`, parameter profiling of `rpc_completed` captures from a table or a
  `.xel` file, with catch-all detection, call shapes, combinations, findings and an exportable report.

[Unreleased]: https://github.com/italombs/ParamLens/compare/v1.3.1...HEAD
[1.3.1]: https://github.com/italombs/ParamLens/compare/v1.3.0...v1.3.1
[1.3.0]: https://github.com/italombs/ParamLens/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/italombs/ParamLens/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/italombs/ParamLens/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/italombs/ParamLens/releases/tag/v1.0.0
