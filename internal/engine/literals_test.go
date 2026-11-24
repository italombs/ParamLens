package engine

import (
	"strings"
	"testing"
)

func TestExtractLiterals(t *testing.T) {
	sql := `SELECT TOP 50 count(1), CAST(x AS varchar(10)) FROM {X} [EN]
WHERE [EN].[STATE] <> 6 AND [EN].[A] IN (3, 1,2) AND (@x = 0 OR [EN].[B] = @x)
  AND [EN].[NAME] LIKE N'ab%' AND [EN].[C] = 'it''s' AND 1 = 1 AND [EN].[V] NOT IN (N'b', N'a,c')
  AND [EN].[D] BETWEEN -5 AND 2.5 AND 7 = [EN].[K] AND [EN].[BIG] >= 3000000000 AND [EN].[BIN] = 0x0A
  AND CASE WHEN [EN].[T] = 1 THEN 'yes' ELSE 'no' END = N'yes' AND DATEADD(day, -7, [EN].[E]) < [EN].[F]`
	lits := ExtractLiterals(sql)
	type exp struct{ name, value, typ, text string }
	want := []exp{
		{"[EN].[STATE] <>", "6", "int", "6"},
		{"IN [EN].[A]", "1,2,3", "nvarchar", "(3, 1,2)"},
		{"[EN].[NAME] LIKE", "'ab%'", "nvarchar", "N'ab%'"},
		{"[EN].[C] =", "'it''s'", "varchar", "'it''s'"},
		{"NOT IN [EN].[V]", "a,c,b", "nvarchar", "(N'b', N'a,c')"},
		{"[EN].[D] BETWEEN", "-5", "int", "-5"},
		{"[EN].[D] AND", "2.5", "decimal", "2.5"},
		{"[EN].[K] =", "7", "int", "7"},
		{"[EN].[BIG] >=", "3000000000", "bigint", "3000000000"},
		{"[EN].[BIN] =", "0x0a", "varbinary", "0x0A"},
		{"[EN].[T] =", "1", "int", "1"},
		{"expr =", "'yes'", "nvarchar", "N'yes'"},
	}
	if len(lits) != len(want) {
		for _, l := range lits {
			t.Logf("%+v", l)
		}
		t.Fatalf("%d literals, expected %d", len(lits), len(want))
	}
	for i, w := range want {
		l := lits[i]
		if l.Name != w.name || l.Value != w.value || l.Type != w.typ || sql[l.start:l.end] != w.text {
			t.Errorf("literal %d: %q %q %q %q, expected %+v", i, l.Name, l.Value, l.Type, sql[l.start:l.end], w)
		}
	}
	used := map[string]bool{"@x": true, "@state": true}
	if v := literalVar(lits[0].Label, false, used); v != "@STATE_2" {
		t.Errorf("var %q", v)
	}
	if v := literalVar(lits[1].Label, true, used); v != "@A_list" {
		t.Errorf("var %q", v)
	}
	if v := literalVar("expr", false, used); v != "@value" {
		t.Errorf("var %q", v)
	}
}

func TestWidenType(t *testing.T) {
	for _, c := range [][3]string{{"int", "bigint", "bigint"}, {"int", "decimal", "decimal"}, {"money", "decimal", "decimal"},
		{"bigint", "float", "float"}, {"varchar", "nvarchar", "nvarchar"}, {"int", "varchar", "nvarchar"}, {"int", "int", "int"}} {
		if got := widenType(c[0], c[1]); got != c[2] {
			t.Errorf("widen(%s, %s) = %s", c[0], c[1], got)
		}
	}
}

func TestParamTableLiterals(t *testing.T) {
	e, err := NewEngine(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for _, s := range []string{
		`exec sp_executesql N'SELECT 1 FROM {X} [EN] WHERE [EN].[S] <> 6 AND [EN].[A] IN (1,2) AND [EN].[B] = @b',N'@b int',@b=1`,
		`exec sp_executesql N'SELECT 1 FROM {X} [EN] WHERE [EN].[S] <> 6 AND [EN].[A] IN (3, 4, 5) AND [EN].[B] = @b',N'@b int',@b=2`,
		`exec sp_executesql N'SELECT 1 FROM {X} [EN] WHERE [EN].[S] <> 6 AND [EN].[A] IN (1,2) AND [EN].[B] = @b',N'@b int',@b=1`,
	} {
		e.Ingest(Row{Stmt: s})
	}
	e.Finish()
	pt, err := e.ParamTable(0, "combos")
	if err != nil {
		t.Fatal(err)
	}
	if len(pt.Literals) != 2 || pt.Literals[0].Var != "@S" || pt.Literals[1].Var != "@A_list" || !pt.Literals[1].List {
		t.Fatalf("literals %+v", pt.Literals)
	}
	sql := pt.SQLParts[0] + "@S" + pt.SQLParts[1] + "(SELECT value FROM STRING_SPLIT(@A_list, ','))" + pt.SQLParts[2]
	if !strings.Contains(sql, "[EN].[S] <> @S AND [EN].[A] IN (SELECT value FROM STRING_SPLIT(@A_list, ',')) AND") {
		t.Errorf("parameterized SQL: %s", sql)
	}
	if len(pt.Rows) != 2 || pt.Rows[0].Count != 2 || pt.Rows[0].Lits[1] != "1,2" || pt.Rows[1].Lits[1] != "3,4,5" || pt.Rows[0].Lits[0] != "6" {
		t.Errorf("rows %+v", pt.Rows)
	}
}
