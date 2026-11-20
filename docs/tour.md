# Feature tour

All screenshots use the built-in **sample data** (synthetic, OutSystems-style queries over a fictitious
orders/customers/invoices schema). Try it yourself: run `ParamLens.exe` and click **Load sample data**.

![Usage: load the sample data, open a query, inspect parameters and a sample execution](images/tour.gif)

## 1. Connect

`ParamLens.exe` opens a local page in your browser. Connect with Windows or SQL Server authentication and
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

## 8. English or Portuguese

The interface is in English by default and can be switched to Portuguese (pt-BR) in the header; numbers
follow the selected language. Exported reports can switch too.

![Findings in Portuguese](images/findings-pt-br.png)

## 9. The standalone HTML version

`ParamLens.html` does not connect to SQL Server: it builds the export command for you (bcp or an SSMS
query, for a `.xel` file or a table), and you drop the exported file onto the page. Everything runs in the
browser, offline.

![File mode and the export guide](images/html-export.png)

After the analysis, the **Not parsed** tab also describes the file: format, encoding, period and the
events that were skipped.

![File details](images/html-file-info.png)
