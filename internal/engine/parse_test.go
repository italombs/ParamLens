package engine

import (
	"testing"
)

func TestParseExecuteSQL(t *testing.T) {
	s := `exec sp_executesql N'SELECT * FROM {X} [EN] WHERE ([EN].[A] IN (3, 1,2)) AND (@x = 0 OR [EN].[B] = @x) AND [EN].[C] = ''it''''s'' AND (@d = @nd OR [EN].[D] >= @d) AND [EN].[E] NOT IN (N''a'',N''b'')',N'@x bigint,@s nvarchar(50),@d datetime,@nd datetime,@m decimal(18,2)',@x=0,@s=N'a,b=c ''q''',@d='1900-01-01 00:00:00',@nd='1900-01-01 00:00:00.000',@m=-1.5`
	r, err := ParseRPC(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "sp_executesql" || len(r.Params) != 5 {
		t.Fatalf("%+v", r)
	}
	exp := []string{"0", "'a,b=c ''q'''", "'1900-01-01 00:00:00'", "'1900-01-01 00:00:00.000'", "-1.5"}
	for i, p := range r.Params {
		if p.Value != exp[i] {
			t.Errorf("param %s = %q, esperado %q", p.Name, p.Value, exp[i])
		}
	}
	if r.Params[4].Type != "decimal(18,2)" {
		t.Errorf("type %q", r.Params[4].Type)
	}
	norm, lists := NormalizeSQL(r.SQL, false)
	t.Log(norm)
	if len(lists) != 2 || lists[0].Values != "1,2,3" || lists[0].Label != "[EN].[A]" || !lists[1].Not {
		t.Errorf("lists %+v", lists)
	}
	pats := DetectPatterns(r.SQL)
	for _, p := range pats {
		t.Logf("pattern %s | %s", p.Cond(), p.Snippet)
	}
	if len(pats) != 2 {
		t.Fatalf("patterns %d", len(pats))
	}
}

func TestParsePrepExec(t *testing.T) {
	s := "declare @p1 int\nset @p1=NULL\nexec sp_prepexec @p1 output,N'@P0 int,@P1 nvarchar(10)',N'select 1 where a=@P0 and b=@P1',@P0=5,@P1=NULL\nselect @p1"
	r, err := ParseRPC(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.SQL != "select 1 where a=@P0 and b=@P1" || len(r.Params) != 2 || r.Params[1].Value != "NULL" || !r.Params[1].IsNull {
		t.Fatalf("%+v", r)
	}
}

func TestParseProc(t *testing.T) {
	r, err := ParseRPC("exec [dbo].[usp_x] @a=1, @b=N'x,y', 'pos', @c=default")
	if err != nil {
		t.Fatal(err)
	}
	if r.Proc != "dbo.usp_x" || len(r.Params) != 4 || r.Params[2].Name != "#3" || r.Params[3].Value != "DEFAULT" {
		t.Fatalf("%+v", r)
	}
}

func TestPositional(t *testing.T) {
	r, err := ParseRPC("exec sp_executesql N'select @a+@b',N'@a int,@b int',1,2")
	if err != nil {
		t.Fatal(err)
	}
	if r.Params[0].Value != "1" || r.Params[1].Value != "2" {
		t.Fatalf("%+v", r.Params)
	}
}

func TestEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0", "0", true}, {"'0'", "0", true}, {"NULL", "0", false}, {"'1900-01-01 00:00:00'", "'1900-01-01 00:00:00.000'", true},
		{"N''", "''", false}, {"'ABC'", "'abc'", true},
	}
	for _, c := range cases {
		if got, _ := sqlEqual(c.a, c.b); got != c.want {
			t.Errorf("%s = %s -> %v", c.a, c.b, got)
		}
	}
}
