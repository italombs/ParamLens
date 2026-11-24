package engine

import (
	"errors"
	"sort"
	"strings"
)

// ---------- parameter table: the exact values of every call, for replaying them ----------

// ParamColumn is a real parameter of a query (the IN-list pseudo-parameters are left out:
// their values are part of the SQL text, not parameters).
type ParamColumn struct {
	Name  string `json:"name"`
	Types []KV   `json:"types,omitempty"`
}

// ParamRow is one execution (mode "execs") or one distinct combination of values (mode "combos").
// Values are canonical literals ('text', 12, 0x0A, NULL, DEFAULT) or "(absent)"/"(no value)".
type ParamRow struct {
	Values []string `json:"values"`
	Lits   []string `json:"lits"` // hard-coded values, in the order of ParamTable.Literals
	Count  int      `json:"count"`
	Sum    Metrics  `json:"sum"`
	Exec   int32    `json:"exec"` // first execution with these values
}

type ParamTable struct {
	Family   int            `json:"family"`
	Kind     string         `json:"kind"`
	Proc     string         `json:"proc"`
	SQL      string         `json:"sql"`
	Decl     string         `json:"decl"` // most used parameter declaration
	Mode     string         `json:"mode"`
	Has      [NMetrics]bool `json:"has"`
	Lists    int            `json:"lists"` // inline IN lists
	Params   []ParamColumn  `json:"params"`
	Literals []LitColumn    `json:"literals"` // hard-coded values of the SQL text
	SQLParts []string       `json:"sqlParts"` // the SQL around the literals: parts[0] lit[0] parts[1] ... (empty without literals)
	Rows     []ParamRow     `json:"rows"`
	Failed   int            `json:"failed"` // statements that could not be read back
}

// LitColumn is a hard-coded value of the SQL text (see ExtractLiterals), exported like a parameter.
type LitColumn struct {
	Name  string `json:"name"` // "[EN].[STATE] <>", "IN [EN].[TYPE]"
	Var   string `json:"var"`  // parameter name for it: @STATE, @TYPE_list
	List  bool   `json:"list"`
	Type  string `json:"type"`  // base type over all executions (lists: nvarchar)
	Comma bool   `json:"comma"` // list: an item has a comma inside, in some execution
	Text  string `json:"text"`  // as written in the SQL text of the first execution
}

var errNoFamily = errors.New("query not found")

// ParamTable re-reads the statements of a query and returns the exact value of each parameter
// (without the 200-byte cut of the statistics), one row per execution or per distinct combination.
func (e *Engine) ParamTable(id int, mode string) (*ParamTable, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if id < 0 || id >= len(e.fams) {
		return nil, errNoFamily
	}
	f := e.fams[id]
	if mode != "combos" {
		mode = "execs"
	}
	pt := &ParamTable{Family: f.ID, Kind: f.Kind, Proc: f.Proc, SQL: f.SampleSQL, Mode: mode, Has: e.has, Params: []ParamColumn{}, Literals: []LitColumn{}, SQLParts: []string{}, Rows: []ParamRow{}}
	var best KV
	found := false
	for k, v := range f.Decls {
		if kv := (KV{k, v}); !found || kvLess(kv, best) {
			best, found = kv, true
		}
	}
	pt.Decl = best.K
	var cols []int
	colOf := make([]int, len(f.Params))
	for i, p := range f.Params {
		colOf[i] = -1
		if p.Pseudo != "" {
			if p.Pseudo == pseudoInVals {
				pt.Lists++
			}
			continue
		}
		c := ParamColumn{Name: p.Name}
		for k, v := range p.Types {
			c.Types = append(c.Types, KV{k, v})
		}
		sort.Slice(c.Types, func(a, b int) bool { return kvLess(c.Types[a], c.Types[b]) })
		pt.Params = append(pt.Params, c)
		colOf[i] = len(cols)
		cols = append(cols, i)
	}
	if f.Kind != "proc" {
		if lits := ExtractLiterals(f.SampleSQL); len(lits) > 0 {
			used := map[string]bool{}
			for _, p := range f.Params {
				used[strings.ToLower(p.Name)] = true
			}
			for _, t := range Tokenize(f.SampleSQL) {
				if t.Kind == TkVar {
					used[strings.ToLower(t.Text)] = true
				}
			}
			prev := 0
			for _, l := range lits {
				pt.SQLParts = append(pt.SQLParts, f.SampleSQL[prev:l.start])
				pt.Literals = append(pt.Literals, LitColumn{Name: l.Name, Var: literalVar(l.Label, l.List, used), List: l.List, Type: l.Type, Comma: l.Comma, Text: f.SampleSQL[l.start:l.end]})
				prev = l.end
			}
			pt.SQLParts = append(pt.SQLParts, f.SampleSQL[prev:])
		}
	}
	byKey := map[string]int{}
	var buf []byte
	for _, ix := range f.Execs {
		ex := &e.execs[ix]
		vals := make([]string, len(cols))
		for k := range vals {
			vals[k] = absentValue
		}
		lits := make([]string, len(pt.Literals))
		for k := range lits {
			lits[k] = absentValue
		}
		if cap(buf) < int(ex.Len) {
			buf = make([]byte, ex.Len)
		}
		b := buf[:ex.Len]
		var rpc *RPC
		_, err := e.tmp.ReadAt(b, ex.Off)
		if err == nil {
			rpc, err = ParseRPCFull(string(b))
		}
		if err != nil {
			pt.Failed++
		} else {
			for _, p := range rpc.Params {
				if pi, ok := f.pidx[strings.ToLower(p.Name)]; ok && colOf[pi] >= 0 {
					vals[colOf[pi]] = p.Value
				}
			}
			if len(lits) > 0 {
				if el := ExtractLiterals(rpc.SQL); len(el) == len(lits) {
					for k, l := range el {
						lits[k] = l.Value
						pt.Literals[k].Type = widenType(pt.Literals[k].Type, l.Type)
						pt.Literals[k].Comma = pt.Literals[k].Comma || l.Comma
					}
				}
			}
		}
		if mode == "execs" {
			pt.Rows = append(pt.Rows, ParamRow{Values: vals, Lits: lits, Count: 1, Sum: ex.M, Exec: ix})
			continue
		}
		key := strings.Join(vals, "\x00") + "\x01" + strings.Join(lits, "\x00")
		if r, ok := byKey[key]; ok {
			row := &pt.Rows[r]
			row.Count++
			for m := 0; m < NMetrics; m++ {
				row.Sum[m] += ex.M[m]
			}
			continue
		}
		byKey[key] = len(pt.Rows)
		pt.Rows = append(pt.Rows, ParamRow{Values: vals, Lits: lits, Count: 1, Sum: ex.M, Exec: ix})
	}
	if mode == "combos" {
		sort.SliceStable(pt.Rows, func(a, b int) bool {
			if pt.Rows[a].Count != pt.Rows[b].Count {
				return pt.Rows[a].Count > pt.Rows[b].Count
			}
			return pt.Rows[a].Exec < pt.Rows[b].Exec
		})
	}
	return pt, nil
}

// ---------- metric distributions per query (to compare two captures) ----------

// Dist summarizes one metric of one query: nearest-rank percentiles.
type Dist struct {
	Min int64 `json:"min"`
	P50 int64 `json:"p50"`
	P90 int64 `json:"p90"`
	P95 int64 `json:"p95"`
	P99 int64 `json:"p99"`
	Max int64 `json:"max"`
}

type FamilyDist struct {
	ID    int             `json:"id"`
	Count int             `json:"count"`
	Sum   Metrics         `json:"sum"`
	D     [NMetrics]*Dist `json:"d"` // nil for metrics missing from the capture
}

// rank returns the nearest-rank percentile p (in %) of the sorted values.
func rank(v []int64, p int) int64 {
	i := (p*len(v)+99)/100 - 1
	if i < 0 {
		i = 0
	}
	return v[i]
}

// Distributions returns, for every query, the percentiles of each metric present in the capture.
func (e *Engine) Distributions() []FamilyDist {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]FamilyDist, 0, len(e.fams))
	var v []int64
	for _, f := range e.fams {
		fd := FamilyDist{ID: f.ID, Count: len(f.Execs)}
		for _, ix := range f.Execs {
			for m := 0; m < NMetrics; m++ {
				fd.Sum[m] += e.execs[ix].M[m]
			}
		}
		if len(f.Execs) > 0 {
			for m := 0; m < NMetrics; m++ {
				if !e.has[m] {
					continue
				}
				v = v[:0]
				for _, ix := range f.Execs {
					v = append(v, e.execs[ix].M[m])
				}
				sort.Slice(v, func(a, b int) bool { return v[a] < v[b] })
				fd.D[m] = &Dist{Min: v[0], P50: rank(v, 50), P90: rank(v, 90), P95: rank(v, 95), P99: rank(v, 99), Max: v[len(v)-1]}
			}
		}
		out = append(out, fd)
	}
	return out
}
