# Feature tour

All screenshots use the built-in **sample data** (synthetic, OutSystems-style queries over a fictitious
orders/customers/invoices schema). Try it yourself: run `ParamLens.exe` and click **Load sample data**.

![Usage: load the sample data, open a query, inspect its parameters, export them and compare the query with its rewrite](images/tour.gif)

## 1. Connect

`ParamLens.exe` opens in its own window (closing it quits). Connect with Windows or SQL Server authentication and
pick the source: a table with the captured events or a `.xel` file on the server.

![Connection and event source](images/connect.png)

## 2. Findings first

The first screen lists every finding, ranked by severity and by how much the query weighs in the
workload, each with an explanation, the SQL snippet and a suggestion. The left side lists the distinct
queries ("families") by cost.

![All findings](images/screenshot.png)

## 3. Catch-all predicates

For each optional filter like `@p = 0 OR col = @p`, the % of executions in which the parameter side is
true — that is, in which the OR filtered nothing — and the average cost in both cases.

![Catch-all predicates](images/catch-all.png)

## 4. Call shapes

Which catch-alls actually filtered together, with their share of executions and of cost. Here, the
shape that filters only by carrier is 2.9% of the calls but 26% of the logical reads (about 150,000
reads per call against about 1,000 when nothing is filtered): a classic candidate for a separate query
or `OPTION(RECOMPILE)`.

![Call shapes](images/call-shapes.png)

## 5. Parameters

Type, cardinality, most common value and % NULL of every parameter. Click a row to see its value
distribution and the cost per value. Inline `IN (…)` lists show up as pseudo-parameters (values and size).

![Parameters with the value distribution of one of them](images/parameters.png)

## 6. Value combinations

Pick the parameters that matter and see how their values occur together and what each combination costs.
High-cardinality parameters enter by class (NULL, 0, '', null date, (number)…). Export as CSV.

![Value combinations](images/combinations.png)

## 7. A real execution, ready to test

Any **sample** button opens one real execution of that row: the original statement, or a test script with
the `DECLARE`s and the SQL, ready to paste into SSMS and look at the plan.

![Sample execution as a test script](images/sample-script.png)

## 8. Export the parameters

**Export parameters** takes the exact values of every call of the query (read again from the capture,
without the 200-byte cut of the tables), one row per execution or per distinct combination: as a SQL
script (`CREATE TABLE` + `INSERT`, with an optional replay loop through `sp_executesql` for Query Store
tests), as CSV, or inserted straight into a table of a database you choose.

![Parameter export](images/export-params.png)

## 9. Compare the original with the rewrite

Replay the parameters against the original query and against your rewrite, capture both runs and open
the **Compare** tab: pick two queries of the same capture, or keep the current analysis and analyze
another table, `.xel` or file. You get the change in average, min, p50 to p99, max and total of every
metric, with a warning when the number of rows returned differs or there are few executions. Here the
sample data is compared with a synthetic capture of a rewrite of its order count query (`EXISTS` /
`NOT EXISTS` instead of the `LEFT JOIN`s, `OPTION (RECOMPILE)`).

![Before × after comparison](images/compare.png)

## 10. English or Portuguese

The interface is in English by default and can be switched to Portuguese (pt-BR) in the header; numbers
follow the selected language. Exported reports can switch too.

![Findings in Portuguese](images/findings-pt-br.png)

## 11. The standalone HTML version

`ParamLens.html` does not connect to SQL Server: it builds the export command for you (bcp or an SSMS
query, for a `.xel` file or a table), and you drop the exported file onto the page. Everything runs in the
browser, offline.

![File mode and the export guide](images/html-export.png)

After the analysis, the **Not parsed** tab also describes the file: format, encoding, period and the
events that were skipped.

![File details](images/html-file-info.png)
