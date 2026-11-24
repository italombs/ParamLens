# User guide

## 1. Capture the events

ParamLens works on `rpc_completed` events, the ones that carry the parameter values of each
`exec sp_executesql …` call. A minimal Extended Events session:

```sql
CREATE EVENT SESSION [paramlens] ON SERVER
ADD EVENT sqlserver.rpc_completed (
    WHERE sqlserver.database_name = N'MyDatabase')     -- narrow it down: captures grow fast
ADD TARGET package0.event_file (SET filename = N'D:\XE\paramlens.xel', max_file_size = 512)
WITH (MAX_DISPATCH_LATENCY = 5 SECONDS);

ALTER EVENT SESSION [paramlens] ON SERVER STATE = START;
-- ... let the application run ...
ALTER EVENT SESSION [paramlens] ON SERVER STATE = STOP;
```

Other events in the same capture (`sp_statement_completed`, `sql_statement_completed`, …) are fine:
ParamLens skips them and tells you how many were skipped.

## 2. Analyze

### `ParamLens.exe` (connects to SQL Server)

1. Run it. The interface opens in its own window (one item in the taskbar); closing the window quits
   ParamLens. The window uses WebView2, the Edge engine that comes with Windows 10 and 11; if it is
   missing, ParamLens opens your browser instead, with a console window that quits the program when
   closed. `ParamLens.exe -browser` always uses the browser.
2. **Connection:** server (`SRV01`, `SRV01\INSTANCE`, `10.0.0.5,1433`), Windows or SQL Server
   authentication, encryption. Or paste a connection string.
3. **Event source:**
   - *Table with events* — any table with a `statement` column (for example the result of
     "Export to table" in the SSMS XEvent viewer). Columns are mapped automatically; check the unit of
     `duration`/`cpu_time` (XE uses microseconds).
   - *.xel file* — the path **on the server**, wildcards allowed; read by SQL Server itself with
     `sys.fn_xe_file_target_read_file` (requires `VIEW SERVER STATE`).
4. Optional filters: `statement LIKE`, extra `WHERE`, row limit. **Show query** displays the SQL
   that will run.
5. **Analyze.**

### `ParamLens.html` (offline, from an exported file)

1. Open the file in any browser (it works from the local disk, no installation).
2. Export the events — the right-hand panel builds the command with your server, path or table:
   - **bcp** (recommended, no size limit): copy the command and run it in PowerShell or cmd.
   - **SSMS**: run the query, right-click the grid › *Save Results As…* › *Text (Tab delimited)*.
   - **Shortcut**: `SELECT * FROM <event table> WHERE name = 'rpc_completed'` saved as CSV. Turn on
     *Tools › Options › Query Results › SQL Server › Results to Grid › Quote strings containing list
     separators when saving .csv results*, otherwise statements with commas break the columns.
3. Drop the file onto the page and click **Analyze**.

Details of the accepted files: [export-format.md](export-format.md).

## 3. Read the results

- **All findings** — start here. Each finding has a severity (high / medium / info), an explanation, the
  SQL snippet and a suggestion. They are sorted by severity and by the weight of the query.
- **Queries** (left) — one entry per distinct SQL text ("family"), sorted by total cost (logical reads, else CPU, else duration). The sort menu also orders them by the total or the average per execution of logical reads, CPU or duration, by executions or by findings. Inline `IN (…)` lists
  are normalized, so queries that only differ in those lists are grouped.
- **Selected query:**
  - *Catch-all predicates* — for each `@p = x OR …`, the % of executions in which the parameter side is
    true. When it is true, the other side of the OR filters nothing, yet the plan must serve both cases.
  - *Call shapes* — which catch-alls actually filtered together, with their cost. Shapes with a small
    share of executions but a big share of cost are the usual tuning targets.
  - *Parameters* — click a row to see the value distribution and the cost per value.
  - *Value combinations* — pick parameters and see how their values occur together; export as CSV.
  - *Sample* — the real statement of one execution and a test script (`DECLARE` + SQL) for SSMS.
  - *Export parameters* — the parameter values of every call of the query; see [section 4](#4-export-the-parameters-of-a-query).
  - *Executions* — every call of the query with its date and time and metrics, one per row or grouped per
    minute, 5 or 15 minutes, hour, day, hour of the day or day of the week (count, average, maximum and
    total). Times are shown in the local time of your computer or in UTC; click a column to sort, *sample*
    opens the statement of that call, and **CSV** saves the view. A table source needs its timestamp column
    (chosen automatically when it is called `timestamp (UTC)` or `timestamp`).
- **Not parsed** — rows that could not be read, with the reason, plus details of the file in the
  `.html` (encoding, period, skipped events).

**Export HTML report** saves a single offline page with the whole analysis to share with the team
(without the per-execution samples). The report can also switch language.

## 4. Export the parameters of a query

**Export parameters** (in *Selected query*) gives one row per **execution**, or per **distinct
combination of values** (with the number of executions), with one column per parameter and the metrics.
The values are read again from the capture and are complete: the tables on screen cut long values at
200 bytes, the export does not. Columns get the type of the parameter declaration (the widest one when
the application sent different sizes); duration and CPU are in microseconds.

- **SQL script** — `CREATE TABLE` + `INSERT`, with `SET DATEFORMAT ymd` so dates do not depend on the
  login language. Optionally a **replay loop** that calls the query once per row with `sp_executesql`,
  the application's text and the parameter types, so each call reaches Query Store and Extended Events.
  The loop only runs after you change `@run = 0` to `@run = 1`.
- **CSV** — `;` separator, UTF-8; NULL is an empty field.
- **Insert into a database** (`.exe` only) — creates the table if needed and inserts all rows in one
  transaction, on the connection of the start screen or on another server you connect to in the dialog.
  *Drop the table if it already exists* replaces it; otherwise the rows are appended.

### Hard-coded values

The dialog also lists the **hard-coded values** of the SQL text, so they can be exported like
parameters:

- **Inline `IN (…)` lists:** a `nvarchar(max)` column with the values separated by commas (`1,2,3`;
  text without quotes), ready for `STRING_SPLIT`. A warning shows when an item has a comma inside, since
  `STRING_SPLIT` would split it.
- **Literals compared with a column:** `col = 6`, `col <> N'x'`, `col LIKE 'a%'`,
  `col BETWEEN 1 AND 9`. The column gets the type of the literal (`int`, `bigint`, `decimal(p,s)`,
  `nvarchar(n)`…).

Literals compared with a variable (the `@p = 0` of a catch-all) and literals in other places (`TOP 50`,
`count(1)`, `varchar(10)`) are not listed.

**Suggested columns:** every parameter, plus the hard-coded values that change between calls; values
that are always the same start unchecked. In *distinct combination* mode the combinations are grouped
again by the columns you keep.

With hard-coded values selected, the **replay loop runs the query with them as parameters**, which is
how you would call it from the application:

```sql
-- application text:  ... WHERE state_id <> 6 AND type_id IN (1, 2) AND store_id = @store
-- replayed text:     ... WHERE state_id <> @state_id
--                        AND type_id IN (SELECT value FROM STRING_SPLIT(@type_id_list, N','))
--                        AND store_id = @store
```

`STRING_SPLIT` needs database compatibility level 130 (SQL Server 2016) or later. The replay needs
every parameter of the query, so it is turned off while one of them is unchecked.

## 5. Compare a query before and after a rewrite

The **Compare** tab measures a rewrite (for example both versions run by SQLQueryStress):

- **Both runs in the same capture** — click **Compare two queries of this analysis** and pick the
  original in *A* and the rewrite in *B*.
- **The rewrite in another capture** (another table, another `.xel` file or an exported file) — click
  **Compare with another capture…**. The analysis on screen is kept as *A* and the source screen opens,
  as for a new analysis: choose the table, the `.xel` or the file of the rewrite and click **Analyze**.
  The comparison opens as soon as the analysis finishes. **Swap A and B** helps when the rewrite was
  captured first.

For each metric (duration, CPU, logical and physical reads, writes, rows) it shows the average, min,
p50, p90, p95, p99, max and total of both, and the change relative to A (negative = B is lower). It warns
when the average number of rows returned differs (the rewrite may not return the same result) and when
either side has few executions.

**Save to a file** writes the kept capture as a small `.json` (the SQL text of the queries, shortened,
and their metric percentiles; no parameter values), so the comparison can be finished another day with
**Load a saved capture**.

## FAQ

**Windows blocks the `.exe` (SmartScreen).** The executable is not code-signed, so Windows may warn
about it: **More info › Run anyway**. If your company policy offers no "Run anyway", ask IT to allow the
file (the `.sha256` of each release identifies it) or use `ParamLens.html`, which runs in the browser.

**The browser opens instead of the app window.** The WebView2 runtime is missing (it is part of Windows
11 and of up-to-date Windows 10). Everything works the same in the browser; close the console window to
quit.

**Does any data leave my machine?** No. See [SECURITY.md](../SECURITY.md).

**Can it run in Docker?** Yes, with SQL Server authentication; see [development.md](development.md#docker).

**Durations look 1000× off.** Check the `duration`/`cpu_time` unit: Extended Events record microseconds;
some tables store milliseconds.
