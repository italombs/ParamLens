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

1. Run it. A console window shows the local address and the browser opens on it.
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
- **Queries** (left) — one entry per distinct SQL text ("family"), sorted by cost. Inline `IN (…)` lists
  are normalized, so queries that only differ in those lists are grouped.
- **Selected query:**
  - *Catch-all predicates* — for each `@p = x OR …`, the % of executions in which the parameter side is
    true. When it is true, the other side of the OR filters nothing, yet the plan must serve both cases.
  - *Call shapes* — which catch-alls actually filtered together, with their cost. Shapes with a small
    share of executions but a big share of cost are the usual tuning targets.
  - *Parameters* — click a row to see the value distribution and the cost per value.
  - *Value combinations* — pick parameters and see how their values occur together; export as CSV.
  - *Sample* — the real statement of one execution and a test script (`DECLARE` + SQL) for SSMS.
- **Not parsed** — rows that could not be read, with the reason, plus details of the file in the
  `.html` (encoding, period, skipped events).

**Export HTML report** saves a single offline page with the whole analysis to share with the team
(without the per-execution samples). The report can also switch language.

## FAQ

**Windows blocks the `.exe` (SmartScreen).** The executable is not signed yet (signing through the
SignPath Foundation is pending). If your company policy offers no "Run anyway", use `ParamLens.html`.

**Does any data leave my machine?** No. See [SECURITY.md](../SECURITY.md).

**Can it run in Docker?** Yes, with SQL Server authentication; see [development.md](development.md#docker).

**Durations look 1000× off.** Check the `duration`/`cpu_time` unit: Extended Events record microseconds;
some tables store milliseconds.
