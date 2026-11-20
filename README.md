# ParamLens

**Parameter profiling for SQL Server Extended Events `rpc_completed` captures.**

ParamLens reads the `exec sp_executesql …` calls captured by Extended Events and shows how the
application really calls each query: parameter value distributions, combinations, cost per call
pattern and, above all, *catch-all* predicates (`@p = 0 OR col = @p`) that turn into a pass-through in
most executions. Built for the usual ORM/low-code problems (OutSystems, Entity Framework, …): optional
ORs, parameter sniffing, inline `IN` lists and parameter type/size variations.

![ParamLens in action with the sample data](docs/images/tour.gif)

**[See the feature tour with screenshots →](docs/tour.md)**

## Download

| | |
|---|---|
| **[ParamLens.exe](../../releases/latest/download/ParamLens.exe)** | Windows, single executable, no installation. Connects to SQL Server. |
| **[ParamLens.html](../../releases/latest/download/ParamLens.html)** | Any browser, fully offline. Analyzes a file you export from SQL Server. Use it when you cannot run the `.exe`. |

Every version ever published stays available on the [Releases](../../releases) page, each with a
`.sha256` checksum.

## Quick start

**With the `.exe`**

1. Run `ParamLens.exe`: it opens the interface in your browser.
2. Connect to the SQL Server and pick the table with the captured events (or a `.xel` file on the server).
3. Click **Analyze** and start with the **All findings** tab.

**With the `.html`**

1. Open `ParamLens.html` in the browser.
2. Export the events: the page builds the `bcp` command and the SSMS query for you. A plain `SELECT *`
   of the event table saved as CSV also works.
3. Drop the file onto the page and click **Analyze**.

No server at hand? In the `.exe`, **Load sample data** shows everything with synthetic data.
The interface is in English and can be switched to Portuguese (pt-BR) in the header.

More in the [user guide](docs/user-guide.md).

## What it shows

- **Catch-all predicates** (`@x = 0 OR …`, `@d = @nullDate OR …`, `ISNULL(@p, …)`) and the % of executions
  in which each one is true, with the average cost in both cases.
- **Call shapes:** which filters were really applied together, with % of executions and % of cost.
- **Parameters:** type, cardinality, most frequent values, % NULL, value classes (NULL, 0, '', null date…).
- **Combinations** of parameter values of your choice, exportable as CSV.
- **Inline IN lists** as pseudo-parameters, and how many distinct SQL texts they create.
- **Findings** ranked by severity: dominant values with unequal cost (parameter sniffing), diverging
  parameter declarations, constant parameters, disproportionately expensive combinations.
- **Test script:** `DECLARE` + SQL of a real execution, ready for SSMS.
- **HTML report** you can export and open offline.

## Privacy

Captured statements often hold sensitive data, so ParamLens keeps everything on your machine: it only
connects to the SQL Server you specify, has no telemetry and makes no other network calls. The `.exe`
listens on `127.0.0.1` only, with a per-session token; the `.html` reads the file in the browser and never
uploads it. See [SECURITY.md](SECURITY.md).

## Contributing

Bug reports, parser cases from real captures (anonymized!), translations and code are welcome — see
[CONTRIBUTING.md](CONTRIBUTING.md). Build and architecture notes are in
[docs/development.md](docs/development.md); changes per version in [CHANGELOG.md](CHANGELOG.md).

## Code signing policy

Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org).
Team roles, what is signed and the privacy policy: [Code signing policy](CODE_SIGNING_POLICY.md).

## License

[MIT](LICENSE) © 2026 Italo Mesquita
