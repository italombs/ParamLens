# Development

## Layout

```
cmd/paramlens/        entry point: command-line flags, app window or browser, writes ParamLens.html;
                      winres/ holds the icon, manifest and version resources (go-winres)
internal/desktop/     native window (WebView2) and console handling on Windows
internal/engine/      Go engine: T-SQL tokenizer, RPC parser, normalization, statistics, findings
internal/source/      SQL Server sources (table, .xel), Windows authentication, demo data
internal/server/      local HTTP server, /api/* endpoints, analysis jobs, report export
web/                  the interface (index.html), the JS engine (engine.js) and their embedding
scripts/              developer tools: Go × JS parity check, browser smoke test
docs/                 user and developer documentation
.github/              CI, release workflow, issue and pull request templates
```

| File | Contents |
|---|---|
| `internal/engine/tsql.go` | T-SQL tokenizer (strings with `''`/`N''`, `[ident]`, nested comments, `@vars`) |
| `internal/engine/rpc.go` | Parsing of `sp_executesql`, `sp_prepexec` and `exec proc` (named and positional) |
| `internal/engine/normalize.go` | SQL normalization into families, IN lists, catch-all detection |
| `internal/engine/engine.go` | Ingestion and per-execution predicate evaluation |
| `internal/engine/stats.go` | Statistics, combinations, call shapes, findings (`msg` + `args`) |
| `internal/engine/extract.go` | Exact parameter values per execution/combination (export) and metric percentiles (comparison) |
| `internal/source/source.go` | Table and `.xel` queries, connection, demo generator |
| `internal/source/insert.go` | Writes an exported parameter table into SQL Server (typed columns, one transaction) |
| `internal/desktop/` | WebView2 window, dark title bar, console only when needed; no-op stubs outside Windows |
| `internal/server/server.go` | Server, API, jobs, export |
| `web/index.html` | Interface, translations (`en`, `pt-BR`), file mode, export-query builder |
| `web/engine.js` | JavaScript port of the engine + reader of exported files (TSV and CSV) |
| `web/web.go` | `go:embed` of the page and assembly (engine injected at `<!--ENGINE-->`) |

## Two engines

The `.exe` analyzes SQL Server sources with the Go engine; the `.html` (and the file mode of the
`.exe`) uses `web/engine.js`. Both must produce the same JSON for the same input:

- `internal/engine/parity_test.go` writes fixtures (demo data + edge cases, as TSV in UTF-16LE/CRLF
  like `bcp -w`, and as CSV) and the Go output;
- `scripts/parity.js` runs the JS engine on the same files (several encodings) and compares every field.

The JS port reproduces Go's byte semantics (limits in UTF-8 bytes, code-point ordering, per-rune case
mapping). Engines are language-free: findings carry a message id and raw values, and the interface
writes the text in English or pt-BR.

## Build and test

Requires Go 1.24 (the SQL Server driver `go-mssqldb` v1.8.2 is the last one supporting it) and, for the
parity and UI checks, Node 22.

```bash
go vet ./...
go test ./...

# Windows executable and standalone HTML
# (optional) icon, DPI-aware manifest and version resources, as the release does; without them the
# window has no icon. The icon is drawn by: go run scripts/make-icon.go cmd/paramlens/winres/icon.png
(cd cmd/paramlens && go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/ParamLens.exe ./cmd/paramlens
go run ./cmd/paramlens -write-html dist/ParamLens.html

# Go × JS parity
PARAMLENS_PARITY_DIR=$PWD/dist/parity go test -count=1 -run TestParityFixtures ./internal/engine
node scripts/parity.js dist/parity

# UI smoke test in Chromium (needs Playwright)
go build -o dist/paramlens ./cmd/paramlens && ./dist/paramlens -write-html dist/ParamLens.html
node scripts/ui-smoke.js dist/ParamLens.html dist/parity/export.tsv dist/paramlens
```

`PARAMLENS_PARITY_ROWS=400000` makes the parity fixture large (performance check).

Run locally: `go run ./cmd/paramlens -port 8765` and use **Load sample data**.

Command-line options: `-port` (fixed port), `-browser` (default browser instead of the app window),
`-no-browser` (no window at all, only the link on the console), `-bind` (default `127.0.0.1`),
`-write-html <file>` and `-write-demo <file>` (the sample data as an exported TSV, `-demo-rows` events).

The release `.exe` is a GUI program (`-H windowsgui`): without a console it opens the WebView2 window and
quits when the window closes. In browser mode, or when WebView2 is missing, it attaches to the console
of the terminal that started it or opens a new one (closing it quits). Builds without `-H windowsgui`
(`go run`) keep their console, which is handy while developing.

## Screenshots and the usage GIF

The images in `docs/images/` (used by the README and `docs/tour.md`) come from the sample data only.
Regenerate them after interface changes:

```bash
go build -o dist/paramlens ./cmd/paramlens && ./dist/paramlens -write-html dist/ParamLens.html
PARAMLENS_PARITY_DIR=$PWD/dist/parity go test -count=1 -run TestParityFixtures ./internal/engine
node scripts/docs-media.js dist/paramlens dist/ParamLens.html dist/parity/export.tsv   # needs Playwright
ffmpeg -y -i dist/media/tour.webm -vf "fps=8,scale=960:-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=96:stats_mode=diff[p];[s1][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" docs/images/tour.gif
```

The promotional videos (`dist/promo/paramlens-en.mp4`, `paramlens-pt-BR.mp4`: captioned frames with
crossfades) and `docs/images/social-preview.png` come from the same sample data:

```bash
node scripts/promo-video.js dist/paramlens            # frames in dist/promo/frames-<lang>/ (Playwright)
sh scripts/promo-video.sh                           # dist/promo/*.mp4 (ffmpeg)
node scripts/promo-video.js dist/paramlens --social   # docs/images/social-preview.png
```

The comparison images use `dist/media/rewrite.tsv`, a synthetic capture of a rewrite of the sample order
count query written by the same script. Never use a real capture for documentation images.

## Docker

For machines without Go:

```bash
docker build --target export --output dist .        # dist/ParamLens.exe and dist/ParamLens.html (runs vet + tests)
docker build -t paramlens .                          # Linux image
docker run --rm -it -p 127.0.0.1:8765:8765 paramlens # open the "Interface:" link printed on the console
```

Inside the container only SQL Server authentication works (not Windows/SSPI); to reach the SQL Server on
the host, use `host.docker.internal` as the server. The parity and UI checks also run in containers
(`node:22-alpine`, `mcr.microsoft.com/playwright:v1.49.1-noble`).

## Releases

Pushing a tag `vX.Y.Z` runs `.github/workflows/release.yml` on Windows: tests, executable metadata,
build, standalone HTML, checksums and the GitHub release, whose notes are the version's section of
`CHANGELOG.md`: add it before tagging.

## Online demo

`.github/workflows/pages.yml` publishes `ParamLens.html` as `index.html` and the sample data written by
`-write-demo` as `sample-data.tsv` on GitHub Pages (<https://italombs.github.io/ParamLens/?demo=1>) on every
push to `main` that touches the code. Served over HTTP next to that file, the page shows **Load sample data**
in file mode and `?demo=1` analyzes it on load; opened from disk it is unchanged.
