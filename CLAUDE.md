# ParamLens — context for Claude Code

## What it is
A DBA tool by Italo Mesquita. It reads `rpc_completed` Extended Events (from a table, a
`.xel` file or an exported TSV file), parses each `exec sp_executesql ...` and shows how the application
calls each query: parameter value distributions, combinations, cost per pattern and, above all,
*catch-all* predicates like `@p = 0 OR col = @p` that become a *pass-through* in most executions
(typical OutSystems case: optional ORs, LEFT JOIN that could be NOT EXISTS, inline IN lists).

It replaces a hand-written analysis query (CHARINDEX + STRING_SPLIT), which broke on commas inside
strings, `',@` inside the SQL, multiple IN lists, etc.

**Never put names from the user's environment in the repository** (tables, columns, parameters, server
or database names, sample values): placeholders, demo data, docs and tests use the fictitious
orders/customers/invoices domain. Real captures stay in `sample/` (git-ignored).

## User requirements
- A single Windows `.exe`, nothing to install (SQLQueryStress style), plus a standalone `.html`
  (for machines where the `.exe` is blocked).
- **All documentation in English.** The UI opens in **English by default** and can be switched to
  **pt-BR** (header selector, remembered in the browser; `?lang=pt-BR` also works). Numbers follow the
  selected locale.
- Sensitive data (statements contain patient IDs): nothing leaves the machine, no telemetry.

## Architecture (Go 1.24, no CGO)
- Go was chosen over .NET because it produces a ~8 MB static `.exe` and cross-compiles without runtime packs.
- Driver: `github.com/microsoft/go-mssqldb` **v1.8.2** (v1.9+ requires Go 1.25). Windows auth through SSPI
  (`internal/source/auth_windows.go`); the connection is built as a `sqlserver://` URL (the driver's ADO parser does not
  accept `{}`, a password with `;` would break).
- The `.exe` serves HTTP on `127.0.0.1:<random port>` (`-bind` changes it, only for containers) and requires a per-session token (`X-Token` header or `?t=` for downloads) and checks `Host`
  (anti DNS-rebinding).
- The `.exe` is a GUI build (`-H windowsgui`): `internal/desktop` shows the page in a WebView2 window
  (`github.com/jchv/go-webview2`, pure Go; URL gets `&app=1`, which hides the Quit button) and the process
  ends when the window closes. `-browser`, `-no-browser` or a missing WebView2 runtime fall back to the browser
  with a console (`AttachConsole` to the parent terminal or `AllocConsole`). The window icon is resource #1 of
  `cmd/paramlens/winres/winres.json` (go-winres in the release workflow; the `.syso` is git-ignored).
- The UI is a single `web/index.html` (vanilla JS, no CDN). `web/web.go` embeds it and injects
  `web/engine.js` at the `<!--ENGINE-->` marker (`web.Page()`, `web.WithStaticData()`). The same assembled page is:
  the `.exe` UI, the standalone `ParamLens.html` (`-write-html`, built by the release workflow; also the online demo on
  GitHub Pages with `sample-data.tsv` from `-write-demo`, `.github/workflows/pages.yml`, `?demo=1` loads it) and the
  exported report (analysis JSON injected at `<!--STATIC-DATA-->`; static mode: no API, fixed combinations,
  no "sample" button).
- **Two engines, kept identical:** Go (`.exe`, SQL sources) and JS (`web/engine.js`, file mode in the
  `.html` and in the `.exe`). `internal/engine/parity_test.go` (package `engine_test`) writes fixtures (demo + edge cases,
  TSV in UTF-16LE/CRLF like `bcp -w`, and CSV) and `scripts/parity.js` compares every JSON field. Go semantics are reproduced in JS: limits in
  UTF-8 bytes (200 per value, 160 in titles, 3000 in failure samples, U+FFFD for a split rune), code-point
  ordering, per-rune ToUpper (ß stays ß). Sorts are deterministic on ties (stable / key tie-break).
- **Language-neutral engines:** findings are `{sev, family, msg, args, code}` with raw numbers; the UI
  (`FMSG` in `index.html`) writes the text in English or pt-BR. Engine ids and messages are in English
  (`(absent)`, `(no value)`, `constant/low/high`, `filter`, failure reasons, server errors) and the UI
  translates them (`PT_VALUES`, `PT_MESSAGES`).

| File | Role |
|---|---|
| `internal/engine/tsql.go` | Tolerant T-SQL tokenizer (`''`/`N''` strings, `[ident]]`, `"ident"`, nested comments, `@vars`, numbers, binary) |
| `internal/engine/rpc.go` | `ParseRPC`: `sp_executesql`, `sp_prepexec` (with the `declare @p1 ... select @p1` prelude), `exec proc`; named and positional parameters; `ParseDecl` for types |
| `internal/engine/normalize.go` | `NormalizeSQL` (family = normalized SQL; `IN (literals)` lists become `IN (#lista)`), `DetectPatterns` (catch-all OR, var=lit, var=var, IS [NOT] NULL, ISNULL/COALESCE) |
| `internal/engine/engine.go` | Ingestion: families, per-parameter value dictionary (max 50k distinct), pseudo-parameters for IN lists, statements in a temp file (offset/len) for "sample"; `sqlEqual` approximates SQL Server semantics (CI, implicit conversion, NULL); `TestScript` builds DECLARE + SQL |
| `internal/engine/stats.go` | Overview, per-family detail, parameter stats (cardinality constant/low(≤30)/high + classes), combinations, call shapes (filter/pass-through per catch-all), findings with severity 1–3 |
| `internal/engine/literals.go` | `ExtractLiterals`: hard-coded values of the SQL (inline IN lists of literals, literals compared with a column; not `@p = 0` sentinels, TOP, count(1)…), with type and text span; `literalVar` names them (`@COL`, `@COL_list`). JS: `extractLiterals` |
| `internal/engine/extract.go` | `ParamTable` (exact values per execution/combination, re-parsed from the temp file with `ParseRPCFull`, no 200-byte cut; also `literals`/`sqlParts`/row `lits` for the hard-coded values, which the page exports and turns into parameters of the replay, lists through `STRING_SPLIT`) and `Distributions` (nearest-rank p50/p90/p95/p99 per metric); JS: `paramTable` (async, `readStatementsAt`) and `distributions` |
| `internal/engine/timeline.go` | `ParseTimestamp` (ISO 8601 with `Z`/offset or none = UTC → Unix ms; `.xel`, `CONVERT(…, 121)` of a table column, exported files) and `Timeline` (executions of a query with timestamp and metrics, grouped by the page: `/api/timeline`, "Executions" dialog). JS: `parseTimestamp`, `timeline` |
| `internal/source/source.go` | Sources: table (column mapping, `TRY_CAST`, LIKE/WHERE/TOP filters), `.xel` via `sys.fn_xe_file_target_read_file` + XML, and `RunDemo` (synthetic OutSystems-style data) |
| `internal/source/insert.go` | `InsertParams`: CREATE TABLE (types validated by `ValidType`) + parameterized multi-row INSERTs in one transaction, `SET DATEFORMAT ymd` |
| `internal/desktop/` | WebView2 window, dark title bar, maximize on small screens, console handling, `Fatal` message box |
| `internal/server/server.go` | `Listen`/`Serve`/`Banner`: server, `/api/*` endpoints (incl. `/api/params`, `/api/dist`, `/api/insert`), analysis jobs in a goroutine, report export |
| `cmd/paramlens/main.go` | Flags (`-port`, `-browser`, `-no-browser`, `-bind`, `-write-html`, `-write-demo`), window or browser, `version` set by `-ldflags -X main.version` |
| `web/web.go` | `go:embed` of `index.html` + `engine.js`; `TestAssembledPage` checks the markers |
| `web/engine.js` | JS engine + reader of the exported file (streamed, encoding detection, offsets for samples) |
| `web/index.html` | UI, i18n dictionaries, file mode (`localApi` mimics the `/api/*` endpoints), export-query builder, parameter export (script/CSV/insert payload built in JS for both engines), Compare tab (baseline snapshot kept in the page, saved as `.json`) |

Internal metrics: `duration` and `cpu_time` in **microseconds** (XE default; ms option on the source).
Default cost metric: logical reads → CPU → duration (the first one available).

Exported files accepted by the JS reader (see `docs/export-format.md`): "ParamLens TSV v1" (9 TAB
columns: event, timestamp, 6 metrics, escaped statement) or any CSV/TXT with a header row (columns
matched by name, RFC 4180 quoting, separator detected; `detectFormat`/`forEachRecord` in `engine.js`).

## Current state
- Repository layout (since v1.3.0): `cmd/`, `internal/{engine,source,server,desktop}`, `web/`, `scripts/`, `docs/`.
  User-facing README; developer notes in `docs/development.md`; `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md`.
- Published at https://github.com/italombs/ParamLens (public, MIT). Releases `v1.0.0` (exe) and
  `v1.1.0` (exe + html + i18n). Every release keeps its assets; README links `releases/latest/download/`.
- Unit tests, parity test (6k and 400k rows; TSV and CSV, UTF-8/UTF-16) and the UI smoke test
  (Playwright, both languages) pass. JS engine: ~40 s / 400k events / 1.1 GB file in Node, ~230 MB heap (Go: ~17 s).
- The user's event table is an SSMS XEvent "export to table" (standard columns such as `name`,
  `timestamp (UTC)`, `duration`, `cpu_time`, `logical_reads`, `statement`) mixing `rpc_completed`,
  `sp_statement_completed` and `sql_statement_completed`. A CSV sample of it (kept locally in `sample/`,
  git-ignored: it holds sensitive data — never commit or print its values) parses 70/70 rpc events.
- Validated on 2026-11-23 against SQL Server 2022 in Docker (SQL auth, encrypt=true, login language Brazilian):
  connection, database/table/column listing, table source, `.xel` source, parameter insert and the generated
  script with the replay loop. Still unvalidated: Windows auth (SSPI), named instances, the user's real server.
- The WebView2 window was checked on this Windows 11 machine (one taskbar item, no console, closing it ends the
  process and removes the temp file).
- The generated export queries/bcp command were not yet run on the real server.
- On the company PC the unsigned `.exe` is blocked by SmartScreen **without** a "Run anyway" option.
- The SignPath Foundation declined the free code signing application (2026-11-24: the project needs more
  community adoption first). Signing was removed from the repository; the `.exe` ships unsigned. Options later:
  reapply once the project has users, a Certum open source certificate, or Azure Trusted Signing.

## Ideas
Time-window filter (the export already carries the timestamp), detecting a
`LEFT JOIN` used only as a filter (`NOT EXISTS` candidate), per-statement cost inside an RPC using
`sp_statement_completed` (needs `attach_activity_id`), Web Worker for the JS engine, per-execution parameter declaration in the
export (the replay uses the widest types when declarations vary), timestamps in the parameter export.

## Conventions
- Docs in English. Code and identifiers in English. UI text only through the dictionaries in `index.html`
  (add every key to both `en` and `pt-BR`).
- No external front-end dependencies (must open offline).
- Every parser/engine change: a case in `parse_test.go`/`parity_test.go`, the same change in
  `web/engine.js`, and `scripts/parity.js` must pass. Update `CHANGELOG.md` (Unreleased) for user-visible changes.
- `web/engine.js` is inlined: it must not contain `</script` or `<!--` (checked by `TestAssembledPage`).
- This machine has no Go or Node: use Docker (`golang:1.24-alpine`, `node:22-alpine`,
  `mcr.microsoft.com/playwright:v1.49.1-noble`).
- Windows build: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/ParamLens.exe ./cmd/paramlens` (icon: `go-winres make` in `cmd/paramlens` first). Local output goes to `dist/` (git-ignored); binaries are published only on GitHub Releases, never committed.
- Layout and commands: `docs/development.md`. Run locally: `go run ./cmd/paramlens -port 8765` and use "Load sample data" to test the UI without a server.
