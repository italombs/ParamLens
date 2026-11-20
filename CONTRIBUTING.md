# Contributing to ParamLens

Thanks for helping! Bug reports, statements that are parsed wrongly, translations and code are all
welcome.

## Before you start

- **Never post real data.** Captured statements often contain personal data (patient IDs, names,
  documents). When you share a statement, replace the values and keep only the shape of the call.
- For anything beyond a small fix, **open an issue first** describing the problem or the idea, so we
  can agree on the approach.
- Security problems: do not open a public issue, see [SECURITY.md](SECURITY.md).

## Workflow

1. Fork the repository and create a branch from `main`.
2. Make the change following the rules below.
3. Run the checks (see [docs/development.md](docs/development.md); Docker works if you have no local Go/Node).
4. Update [CHANGELOG.md](CHANGELOG.md) under *Unreleased* when users will notice the change.
5. Open a pull request against `main` explaining what changed and why. CI must pass.

## Rules

- Everything in the repository is in **English**: code, identifiers, comments, docs, commit messages and
  pull requests.
- **The engine exists twice** (Go in `internal/engine` and JavaScript in `web/engine.js`). Any change to
  parsing, normalization, statistics or findings must be made in both, with a test case in
  `internal/engine/parse_test.go` (and, when it affects the output, in `parityExtraRows` in
  `internal/engine/parity_test.go`). The parity check must pass.
- **Interface text** lives only in the dictionaries in `web/index.html`: add every new key to both `en`
  and `pt-BR`. Engines emit ids and raw values (`msg` + `args`), never sentences.
- **No external dependencies** in the front end (no CDN, no npm packages): the page must open offline.
- `web/engine.js` is inlined into the page, so it must not contain `</script` or `<!--`
  (`TestAssembledPage` checks it).
- **No telemetry and no network calls** besides the SQL Server the user configures.

## Checks

```bash
go vet ./...
go test ./...
PARAMLENS_PARITY_DIR=$PWD/dist/parity go test -count=1 -run TestParityFixtures ./internal/engine
node scripts/parity.js dist/parity
```

Optionally, the browser smoke test (Playwright): see [docs/development.md](docs/development.md#build-and-test).
