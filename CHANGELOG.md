# Changelog

All notable changes to ParamLens. The format follows [Keep a Changelog](https://keepachangelog.com/)
and the project uses [semantic versioning](https://semver.org/). Downloads of every version are on the
[Releases](https://github.com/italombs/ParamLens/releases) page.

## [Unreleased]

## [1.5.0] - 2026-11-24

### Added
- Online demo: `ParamLens.html` on GitHub Pages with synthetic sample data
  (<https://italombs.github.io/ParamLens/?demo=1>), analyzed in the browser.
- `-write-demo <file>` writes the sample data as an exported TSV file.
- **Hard-coded values in the parameter export:** the dialog lists the inline `IN` lists and the literals
  compared with a column, and suggests which columns to export (every parameter plus the values that
  change between calls). `IN` lists go as `nvarchar(max)` with the values separated by commas, ready
  for `STRING_SPLIT`; the replay loop then runs the query with those values as parameters
  (`IN (SELECT value FROM STRING_SPLIT(@list, N','))`).
- **Executions** of a query: every call with its date and time and metrics, or grouped per minute, 5 or
  15 minutes, hour, day, hour of the day or day of the week (count, average, maximum and total), in local
  time or UTC, sortable and saved as CSV. The timestamp is read from the `.xel`, from the timestamp column
  of a table (new in the column mapping) and from exported files.

### Changed
- The sample data spreads its calls over three working days (timestamps also in `-write-demo`).
- Query list: sort by the total or the average per execution of logical reads, CPU or duration (the row
  shows that metric), besides executions and findings.
- Parameter export by distinct combination now also tells combinations apart by their `IN` list values
  (regrouped when that column is left out).
- README and feature tour: new usage GIF and screenshots of the parameter export and of the before × after
  comparison, and a "from diagnosis to a measured fix" walkthrough.
- Release notes now come from this changelog.

### Removed
- Code signing policy and the SignPath signing step: the `.exe` is published unsigned.

## [1.4.0] - 2026-11-23

### Added
- **Export parameters** of a query: the exact values of every call (without the 200-byte cut of the
  tables), one row per execution or per distinct combination, with typed columns and the metrics. As a
  SQL script (`CREATE TABLE` + `INSERT`, optional replay loop through `sp_executesql` for Query Store
  tests), as CSV, or inserted straight into a table of a database you choose (`.exe`).
- **Compare** tab: two queries of the same capture, or the current analysis against another capture
  (another table, `.xel` or exported file, chosen on the usual source screen). Shows the change in
  average, min, p50, p90, p95, p99, max and total of each metric, with warnings when the number of rows
  returned differs or there are few executions; A and B can be swapped, and the kept capture can be
  saved to and loaded from a `.json` file.
- Application icon.

### Changed
- The `.exe` opens its own window (WebView2) instead of a console plus a browser tab: a single item in
  the taskbar, and closing the window quits the program. Without WebView2, or with `-browser`, it uses
  the browser and a console window as before.
- Wide tables no longer make the whole page scroll horizontally, short values such as dates stay on one
  line, and the page uses the whole width of large screens; the query list is narrower on small
  screens and table headers and badges wrap.

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

[Unreleased]: https://github.com/italombs/ParamLens/compare/v1.5.0...HEAD
[1.5.0]: https://github.com/italombs/ParamLens/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/italombs/ParamLens/compare/v1.3.1...v1.4.0
[1.3.1]: https://github.com/italombs/ParamLens/compare/v1.3.0...v1.3.1
[1.3.0]: https://github.com/italombs/ParamLens/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/italombs/ParamLens/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/italombs/ParamLens/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/italombs/ParamLens/releases/tag/v1.0.0
