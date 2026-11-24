package engine

import (
	"testing"
	"time"
)

func TestParseTimestamp(t *testing.T) {
	ms := func(s string) int64 {
		tm, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return tm.UnixMilli()
	}
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"2026-11-20T13:10:00.123Z", ms("2026-11-20T13:10:00.123Z"), true},
		{"2026-11-20 10:10:00.1234567 -03:00", ms("2026-11-20T13:10:00.123Z"), true},
		{"2026-11-20 13:10:00.1234567", ms("2026-11-20T13:10:00.123Z"), true},
		{"2026-11-20 13:10:00.1234567 +00:00", ms("2026-11-20T13:10:00.123Z"), true},
		{"  2026-11-20T13:10:00.5+0530 ", ms("2026-11-20T07:40:00.5Z"), true},
		{"2026-11-20 13:10", ms("2026-11-20T13:10:00Z"), true},
		{"2024-02-29T23:59:59,9", ms("2024-02-29T23:59:59.9Z"), true},
		{"2000-03-01T00:00:00", ms("2000-03-01T00:00:00Z"), true},
		{"1900-01-01 00:00:00.000", ms("1900-01-01T00:00:00Z"), true},
		{"0001-01-01T00:00:00", ms("0001-01-01T00:00:00Z"), true},
		{"", 0, false},
		{"NULL", 0, false},
		{"2026-11-20", 0, false},
		{"2026-13-20 10:00", 0, false},
		{"2026-11-20 24:00", 0, false},
		{"2026-11-20 10:00:00.", 0, false},
		{"2026-11-20 10:00:00 AM", 0, false},
		{"20/11/2026 10:00", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseTimestamp(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseTimestamp(%q) = %d, %v; want %d, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestTimeline(t *testing.T) {
	e, err := NewEngine(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	s := "exec sp_executesql N'select * from t where a = @a',N'@a int',@a=1"
	e.Ingest(Row{Stmt: s, TS: "2026-11-20T10:00:00.000", M: Metrics{5}, Has: [NMetrics]bool{true}})
	e.Ingest(Row{Stmt: "SELECT 1", TS: "2026-11-20T10:00:01.000"})
	e.Ingest(Row{Stmt: s, TS: "bad", M: Metrics{7}, Has: [NMetrics]bool{true}})
	e.Finish()
	tl, err := e.Timeline(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Rows) != 2 || tl.WithTS != 1 || tl.Rows[0].TS == nil || *tl.Rows[0].TS != time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC).UnixMilli() || tl.Rows[1].TS != nil || tl.Rows[1].I != 1 || tl.Rows[1].M[0] != 7 {
		t.Fatalf("unexpected timeline: %+v", tl)
	}
	if _, err := e.Timeline(5); err == nil {
		t.Fatal("expected an error for an unknown query")
	}
}
