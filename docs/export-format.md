# Exporting the events for the standalone HTML version

The HTML version (`ParamLens.html`) does not connect to SQL Server: you export the events to a file
and drop that file onto the page. The "How to export the events" panel builds the query and the `bcp`
command with your own names (server, `.xel` path, table and columns); the format and the templates
are below.

## "ParamLens TSV v1" format

One line per event, 9 TAB-separated columns, in this order:

| # | Column | Contents |
|---|---|---|
| 1 | `event` | event name (`rpc_completed`; other events are skipped and counted) |
| 2 | `timestamp` | ISO 8601 (`2026-11-20T10:15:00.123`) or empty |
| 3–8 | `duration`, `cpu_time`, `logical_reads`, `physical_reads`, `writes`, `row_count` | integers; empty or `NULL` = missing |
| 9 | `statement` | escaped: `\` → `\\`, CR → `\r`, LF → `\n`, TAB → `\t` |

- Optional header line (first column `event`).
- The encoding is detected automatically: UTF-16 LE/BE (`bcp -w`, with or without BOM), UTF-8 (with or
  without BOM) or Windows-1252. Line endings LF or CRLF.
- `duration`/`cpu_time` in microseconds (XE default); the page has an option for milliseconds.
- A statement exactly 65,535 characters long is treated as cut by the SSMS grid and is left out of the
  analysis (with a warning).
- The file is read in chunks (it is never loaded whole into memory); about 1 GB / 400k events takes
  ~40 s in the browser.

## Shortcut: CSV/TXT with a header row

A plain `SELECT *` of an event table saved by SSMS also works. The first row must be a header; the
columns are matched by name (case-insensitive) and the others are ignored:

| Field | Accepted header names |
|---|---|
| event | `event`, `name`, `event_name` |
| timestamp | `timestamp (UTC)`, `timestamp_utc`, `timestamp` |
| metrics | `duration`, `cpu_time`/`cpu`, `logical_reads`/`reads`, `physical_reads`, `writes`, `row_count`/`rows` |
| statement (required) | `statement` |

- The separator (comma, semicolon or TAB) is detected from the header.
- Fields follow RFC 4180: a field with a separator, quote or line break must be quoted, with quotes
  doubled. In SSMS turn on *Tools › Options › Query Results › SQL Server › Results to Grid › Quote
  strings containing list separators when saving .csv results*; without it, statements with commas
  break the columns (the page reports those rows).
- The SSMS 65,535-character cell limit still applies; bcp has no limit.

## Query for a `.xel` file

```sql
SELECT x.object_name,
       CONVERT(varchar(27), v.ts, 126),
       v.duration, v.cpu_time, v.logical_reads, v.physical_reads, v.writes, v.row_count,
       REPLACE(REPLACE(REPLACE(REPLACE(v.statement, N'\', N'\\'), NCHAR(13), N'\r'), NCHAR(10), N'\n'), NCHAR(9), N'\t')
FROM (SELECT object_name, CAST(event_data AS xml) AS e
      FROM sys.fn_xe_file_target_read_file(N'D:\XE\my_session*.xel', NULL, NULL, NULL)
      WHERE object_name = N'rpc_completed') AS x
CROSS APPLY (SELECT x.e.value('(event/@timestamp)[1]', 'datetime2(3)') AS ts,
       x.e.value('(event/data[@name=''duration'']/value)[1]', 'bigint') AS duration,
       x.e.value('(event/data[@name=''cpu_time'']/value)[1]', 'bigint') AS cpu_time,
       x.e.value('(event/data[@name=''logical_reads'']/value)[1]', 'bigint') AS logical_reads,
       x.e.value('(event/data[@name=''physical_reads'']/value)[1]', 'bigint') AS physical_reads,
       x.e.value('(event/data[@name=''writes'']/value)[1]', 'bigint') AS writes,
       x.e.value('(event/data[@name=''row_count'']/value)[1]', 'bigint') AS row_count,
       x.e.value('(event/data[@name=''statement'']/value)[1]', 'nvarchar(max)') AS statement) AS v
WHERE v.statement IS NOT NULL
OPTION (MAXDOP 1);
```

Requires `VIEW SERVER STATE`. The XPath uses doubled single quotes so the same query also works
inside the `bcp` command line.

## Query for a table

Adjust the column names; use `NULL` for metrics the table does not have. If the table mixes events
(`sp_statement_completed`, `sql_statement_completed`…), filter on `rpc_completed`: it is the only one
that carries the parameter values.

```sql
SELECT CAST([name] AS nvarchar(60)),
       CONVERT(varchar(27), TRY_CAST([timestamp (UTC)] AS datetime2(3)), 126),
       TRY_CAST([duration] AS bigint), TRY_CAST([cpu_time] AS bigint), TRY_CAST([logical_reads] AS bigint),
       TRY_CAST([physical_reads] AS bigint), TRY_CAST([writes] AS bigint), TRY_CAST([row_count] AS bigint),
       REPLACE(REPLACE(REPLACE(REPLACE(CAST([statement] AS nvarchar(max)), N'\', N'\\'), NCHAR(13), N'\r'), NCHAR(10), N'\n'), NCHAR(9), N'\t')
FROM [dbo].[__xe_event_file]
WHERE [statement] IS NOT NULL
  AND [name] = N'rpc_completed'
OPTION (MAXDOP 1);
```

## Creating the file

**bcp (recommended, no size limit)** — the query on a single line:

```
bcp "SELECT ... OPTION (MAXDOP 1);" queryout "C:\temp\paramlens-events.tsv" -S SERVER -d master -T -w
```

- `-T` = Windows authentication; for SQL Server authentication use `-U user` (bcp asks for the password).
- `-w` writes UTF-16 with TAB between columns and keeps accents/`N'...'`. Do not use `-c` (OEM code page).
- For a table, replace `-d master` with the table's database.

**SSMS**: run the query with results to grid, right-click the grid › *Save Results As…* › type
*Text (Tab delimited) (\*.txt)*. The grid cuts each cell at 65,535 characters; longer statements are
flagged as cut — use bcp in that case.
