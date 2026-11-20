# ParamLens

**Parameter profiling for SQL Server Extended Events `rpc_completed` captures.**

Point ParamLens at a table of `rpc_completed` events (or at a `.xel` file on the server, or at an
exported file) and it parses every `exec sp_executesql …`, extracts the parameters and shows how the
application really calls each query: value distributions, combinations, cost per call pattern and
*catch-all* predicates (`@p = 0 OR col = @p`) that turn into a *pass-through* in most executions.

Built for the usual ORM/low-code problems (OutSystems, Entity Framework, etc.): optional ORs,
parameter sniffing, inline `IN` lists and parameter type/size variations.

The interface is in English by default and can be switched to Portuguese (pt-BR) from the header.

## Download

From [Releases](../../releases):

- **`ParamLens.exe`**: a single executable, no installation. It starts a local interface on
  `http://127.0.0.1:<port>`, opens the browser and connects to SQL Server.
- **`ParamLens.html`**: standalone HTML version, for when the `.exe` cannot run. It opens offline in any
  browser: you export the events to a file (bcp or SSMS) and drop the file onto the page. The analysis
  runs in the browser itself and nothing leaves the machine. The page builds the export query and
  command for you; the file format is described in [docs/export-format.md](docs/export-format.md).

## What it shows

- **Catch-all predicates** detected in the SQL (`@x = 0 OR …`, `@d = @nullDate OR …`, `ISNULL(@p, …)`) and
  the % of executions in which each one is true, with the average cost in both cases.
- **Call shapes:** which filters were active together, with % of executions and % of cost.
- **Parameters:** type, cardinality, most frequent values, % NULL, classes (NULL, 0, '', null date…).
- **Combinations** of values of your choice, exportable as CSV.
- **Inline IN lists** as pseudo-parameters (values and size) and the number of distinct SQL texts they create.
- **Findings:** dominant values with unequal cost (parameter sniffing), diverging parameter
  declarations, constant parameters, disproportionately expensive combinations.
- **Test script:** `DECLARE` + SQL of a real execution, ready for SSMS.
- **Exportable HTML report** that opens offline.

## Supported sources

| Source | How |
|---|---|
| Table | Any table with the `statement` field (automatic column mapping) |
| `.xel` file | Read by SQL Server itself through `sys.fn_xe_file_target_read_file` (requires `VIEW SERVER STATE`) |
| Exported file | TSV created by `bcp` or by SSMS ([format](docs/export-format.md)); in the `.html` and in the `.exe` |

`duration`/`cpu_time` unit: microseconds (XE default) or milliseconds.

## Privacy and security

- Connects only to the SQL Server you specify. No telemetry, no external calls.
- The interface listens on `127.0.0.1` only and requires a random per-session token (it also checks `Host`).
- Passwords stay only in the process memory.
- The statements read are written to a temporary file (for the samples) and deleted on exit.
- In the HTML version, the file is read locally by the browser and never uploaded.

## Building

```bash
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o ParamLens.exe .
```

Command-line options: `-port 8080` (fixed port), `-no-browser`, `-bind` (default `127.0.0.1`) and
`-write-html ParamLens.html` (writes the standalone HTML version and exits).

The engine exists in Go (`.exe`) and in JavaScript (`web/engine.js`, HTML version). The parity test
makes sure both produce the same result:

```bash
PARAMLENS_PARITY_DIR=build/parity go test -run TestParityFixtures .
node tools/parity.js build/parity
```

`tools/ui-smoke.js` drives the interface in a real Chromium (Playwright): file mode, both languages,
the exported report and the server mode.

### Without Go installed (Docker)

```bash
docker build --target export --output dist .        # builds dist/ParamLens.exe and dist/ParamLens.html (runs vet + tests)
docker build -t paramlens .                          # Linux image
docker run --rm -it -p 127.0.0.1:8765:8765 paramlens # open the "Interface:" link printed on the console
```

Inside the container only SQL authentication works (not Windows/SSPI); to reach the SQL Server running
on the host, use `host.docker.internal` as the server.

## Layout

| File | Contents |
|---|---|
| `tsql.go` | T-SQL tokenizer (strings with `''`/`N''`, `[ident]`, nested comments, `@vars`) |
| `rpc.go` | Parsing of `sp_executesql`, `sp_prepexec` and `exec proc` (named and positional) |
| `normalize.go` | SQL normalization into families, IN lists, catch-all detection |
| `engine.go` | Ingestion and per-execution predicate evaluation |
| `stats.go` | Statistics, combinations, call shapes and findings (structured: message id + arguments) |
| `source.go` | Data sources (table, `.xel`, demo) |
| `main.go` | Local server and API |
| `web/index.html` | Interface and its translations (embedded in the executable; also the HTML version and the exported report) |
| `web/engine.js` | JavaScript engine (port of the Go engine) + exported-file reader |
| `tools/parity.js`, `parity_test.go` | Go × JS parity test |
| `tools/ui-smoke.js` | UI test in a real Chromium (Playwright) |

## Contributing

Contributions are welcome: bug reports, parser cases from real captures, translations and code.

1. **Open an issue first** for anything beyond a small fix, describing the problem or the idea.
   When reporting a statement that is parsed wrongly, **remove sensitive data** (patient IDs, names,
   values) and keep only the shape of the call.
2. Fork the repository and create a branch from `main`.
3. Make the change following the rules below, then run the checks.
4. Open a pull request against `main` explaining what changed and why. CI must pass.

Rules:

- Everything in the repository is in **English**: code, identifiers, comments, docs, commit messages
  and pull requests.
- **The engine exists twice** (Go and `web/engine.js`). Any change to parsing, normalization,
  statistics or findings must be made in both, with a test case in `parse_test.go` (and, when it
  affects the output, in `parityExtraRows` in `parity_test.go`), and the parity test must pass.
- **UI text** lives only in the dictionaries in `web/index.html`: add every new key to both `en` and
  `pt-BR`. Engines emit ids and raw values (`msg` + `args`), never sentences.
- **No external dependencies** in the front end (no CDN, no npm packages): the page must open offline.
- `web/engine.js` is inlined into the page, so it must not contain `</script` or `<!--`
  (`TestAssembledHTML` checks it).
- No telemetry and no network calls: the data analyzed is often sensitive.

Checks to run before opening the pull request (Docker works if you have no local Go/Node):

```bash
go vet ./...
go test ./...
PARAMLENS_PARITY_DIR=build/parity go test -run TestParityFixtures .
node tools/parity.js build/parity
# optional, UI in a real browser (needs Playwright):
go build -o build/paramlens . && ./build/paramlens -write-html build/ParamLens.html
node tools/ui-smoke.js build/ParamLens.html build/parity/export.tsv build/paramlens
```

## Code signing

Free code signing provided by [SignPath.io](https://signpath.io), certificate by [SignPath Foundation](https://signpath.org).
See [CODE_SIGNING_POLICY.md](CODE_SIGNING_POLICY.md).

## License

[MIT](LICENSE) © 2026 Italo Mesquita
