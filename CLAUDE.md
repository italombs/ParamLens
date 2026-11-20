# ParamLens — context for Claude Code

## What it is
A DBA tool by Italo Mesquita. It reads `rpc_completed` Extended Events (from a table, a
`.xel` file or an exported TSV file), parses each `exec sp_executesql ...` and shows how the application
calls each query: parameter value distributions, combinations, cost per pattern and, above all,
*catch-all* predicates like `@p = 0 OR col = @p` that become a *pass-through* in most executions
(typical OutSystems case: optional ORs, LEFT JOIN that could be NOT EXISTS, inline IN lists).

It replaces the manual query in `docs/query-manual-original.sql` (CHARINDEX + STRING_SPLIT), which broke
on commas inside strings, `',@` inside the SQL, multiple IN lists, etc.

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
- The `.exe` serves HTTP on `127.0.0.1:<random port>` (`-bind` changes it, only for containers), opens the
  browser and requires a per-session token (`X-Token` header or `?t=` for downloads) and checks `Host`
  (anti DNS-rebinding).
- The UI is a single `web/index.html` (vanilla JS, no CDN). `web/web.go` embeds it and injects
  `web/engine.js` at the `<!--ENGINE-->` marker (`web.Page()`, `web.WithStaticData()`). The same assembled page is:
  the `.exe` UI, the standalone `ParamLens.html` (`-write-html`, built by the release workflow) and the
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
| `internal/source/source.go` | Sources: table (column mapping, `TRY_CAST`, LIKE/WHERE/TOP filters), `.xel` via `sys.fn_xe_file_target_read_file` + XML, and `RunDemo` (synthetic OutSystems-style data) |
| `internal/server/server.go` | `server.Run`: server, `/api/*` endpoints, analysis jobs in a goroutine, report export |
| `cmd/paramlens/main.go` | Flags (`-port`, `-no-browser`, `-bind`, `-write-html`), `version` set by `-ldflags -X main.version` |
| `web/web.go` | `go:embed` of `index.html` + `engine.js`; `TestAssembledPage` checks the markers |
| `web/engine.js` | JS engine + reader of the exported file (streamed, encoding detection, offsets for samples) |
| `web/index.html` | UI, i18n dictionaries, file mode (`localApi` mimics the `/api/*` endpoints), export-query builder |

Internal metrics: `duration` and `cpu_time` in **microseconds** (XE default; ms option on the source).
Default cost metric: logical reads → CPU → duration (the first one available).

Exported files accepted by the JS reader (see `docs/export-format.md`): "ParamLens TSV v1" (9 TAB
columns: event, timestamp, 6 metrics, escaped statement) or any CSV/TXT with a header row (columns
matched by name, RFC 4180 quoting, separator detected; `detectFormat`/`forEachRecord` in `engine.js`).

## Current state
- Repository layout (since v1.3.0): `cmd/`, `internal/{engine,source,server}`, `web/`, `scripts/`, `docs/`.
  User-facing README; developer notes in `docs/development.md`; `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md`.
- Published at https://github.com/italombs/ParamLens (public, MIT). Releases `v1.0.0` (exe) and
  `v1.1.0` (exe + html + i18n). Every release keeps its assets; README links `releases/latest/download/`.
- Unit tests, parity test (6k and 400k rows; TSV and CSV, UTF-8/UTF-16) and the UI smoke test
  (Playwright, both languages) pass. JS engine: ~40 s / 400k events / 1.1 GB file in Node, ~230 MB heap (Go: ~17 s).
- The user's real event table `dbo.__xe_event_file` (an SSMS XEvent "export to table") has 37 columns,
  among them `name`, `timestamp (UTC)`, `duration`, `cpu_time`, `logical_reads`, `physical_reads`,
  `writes`, `row_count`, `statement`, `sql_text`, and mixes `rpc_completed`, `sp_statement_completed`
  and `sql_statement_completed`. A `TOP 1000` CSV sample (kept locally in `sample/`, git-ignored: it holds
  patient data — never commit or print its values) parses 70/70 rpc events in the JS engine.
- **The `.exe` has never run against a real SQL Server.** Validate first: connection (Windows/SQL auth,
  named instance, encrypt), table/column listing, table source query, `.xel` query.
- The generated export queries/bcp command were not yet run on the real server.
- On the company PC the unsigned `.exe` is blocked by SmartScreen **without** a "Run anyway" option.
- SignPath Foundation application submitted on 2026-11-20 (maintainer type: individual); waiting for the answer.
  When approved: create the variable `SIGNPATH_ORGANIZATION_ID` and the secret `SIGNPATH_API_TOKEN` (the signing step runs only when the variable
  `SIGNPATH_ORGANIZATION_ID` and the secret `SIGNPATH_API_TOKEN` exist).

## Ideas
Time-window filter (the export already carries the timestamp), comparing two captures, detecting a
`LEFT JOIN` used only as a filter (`NOT EXISTS` candidate), per-statement cost inside an RPC using
`sp_statement_completed` (needs `attach_activity_id`), executable icon, Web Worker for the JS engine.

## Conventions
- Docs in English. Code and identifiers in English. UI text only through the dictionaries in `index.html`
  (add every key to both `en` and `pt-BR`).
- No external front-end dependencies (must open offline).
- Every parser/engine change: a case in `parse_test.go`/`parity_test.go`, the same change in
  `web/engine.js`, and `scripts/parity.js` must pass. Update `CHANGELOG.md` (Unreleased) for user-visible changes.
- `web/engine.js` is inlined: it must not contain `</script` or `<!--` (checked by `TestAssembledPage`).
- This machine has no Go or Node: use Docker (`golang:1.24-alpine`, `node:22-alpine`,
  `mcr.microsoft.com/playwright:v1.49.1-noble`).
- Windows build: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/ParamLens.exe ./cmd/paramlens`. Local output goes to `dist/` (git-ignored); binaries are published only on GitHub Releases, never committed.
- Layout and commands: `docs/development.md`. Run locally: `go run ./cmd/paramlens -port 8765` and use "Load sample data" to test the UI without a server.
