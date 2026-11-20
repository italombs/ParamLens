package main

import (
	"sort"
	"strings"
)

type Agg struct {
	Count  int     `json:"count"`
	Sum    Metrics `json:"sum"`
	Max    Metrics `json:"max"`
	Sample int32   `json:"sample"`
}

func (a *Agg) add(ex *Exec, idx int32) {
	if a.Count == 0 {
		a.Sample = idx
	}
	a.Count++
	for k := 0; k < NMetrics; k++ {
		a.Sum[k] += ex.M[k]
		if ex.M[k] > a.Max[k] {
			a.Max[k] = ex.M[k]
		}
	}
}

type Overview struct {
	Totals     Agg             `json:"totals"`
	Has        [NMetrics]bool  `json:"has"`
	CostMetric int             `json:"costMetric"`
	Read       int64           `json:"read"`
	Parsed     int64           `json:"parsed"`
	Failed     int64           `json:"failed"`
	FailKinds  map[string]int  `json:"failKinds"`
	Fails      []ParseFail     `json:"fails"`
	Families   []FamilySummary `json:"families"`
	Findings   []Finding       `json:"findings"`
	ElapsedMs  int64           `json:"elapsedMs"`
	Options    map[string]bool `json:"options"`
}

type FamilySummary struct {
	ID        int               `json:"id"`
	Proc      string            `json:"proc"`
	Kind      string            `json:"kind"`
	Title     string            `json:"title"`
	Agg       Agg               `json:"agg"`
	Share     [NMetrics]float64 `json:"share"`
	ExecShare float64           `json:"execShare"`
	Params    int               `json:"params"`
	Findings  int               `json:"findings"`
	MaxSev    int               `json:"maxSev"`
}

type ValueStat struct {
	Value string            `json:"value"`
	Agg   Agg               `json:"agg"`
	Pct   float64           `json:"pct"`
	Share [NMetrics]float64 `json:"share"`
}

type ParamStat struct {
	Index    int         `json:"index"`
	Name     string      `json:"name"`
	Pseudo   string      `json:"pseudo,omitempty"`
	Type     string      `json:"type"`
	Types    []KV        `json:"types,omitempty"`
	Distinct int         `json:"distinct"`
	Overflow bool        `json:"overflow"`
	Card     string      `json:"card"` // constant | low | high| baixa | alta
	NullPct  float64     `json:"nullPct"`
	Top      []ValueStat `json:"top"`
	Classes  []ValueStat `json:"classes,omitempty"`
	Others   int         `json:"others"`
}

type KV struct {
	K string `json:"k"`
	V int    `json:"v"`
}

type ComboRow struct {
	Values []string          `json:"values"`
	Agg    Agg               `json:"agg"`
	Pct    float64           `json:"pct"`
	Share  [NMetrics]float64 `json:"share"`
}

type ComboResult struct {
	Params   []string   `json:"params"`
	Indexes  []int      `json:"indexes"`
	Modes    []string   `json:"modes"`
	Distinct int        `json:"distinct"`
	Rows     []ComboRow `json:"rows"`
}

type PatternStat struct {
	Pattern
	Cond      string  `json:"cond"`
	Evaluated int     `json:"evaluated"`
	True      Agg     `json:"true"`
	False     Agg     `json:"false"`
	TruePct   float64 `json:"truePct"`
}

type Finding struct {
	Sev    int            `json:"sev"` // 3 high, 2 medium, 1 info
	Family int            `json:"family"`
	Msg    string         `json:"msg"`  // message id, rendered by the UI
	Args   map[string]any `json:"args"` // raw values for the message
	Code   string         `json:"code,omitempty"`
}

type FamilyDetail struct {
	Summary  FamilySummary `json:"summary"`
	SQL      string        `json:"sql"`
	Norm     string        `json:"norm"`
	Params   []ParamStat   `json:"params"`
	Combos   ComboResult   `json:"combos"`
	Shapes   ComboResult   `json:"shapes"`
	Patterns []PatternStat `json:"patterns"`
	Decls    []KV          `json:"decls"`
	Findings []Finding     `json:"findings"`
}

const lowCardLimit = 30

// cardinality classes and other ids shown (translated) by the UI
const (
	cardConstant = "constant"
	cardLow      = "low"
	cardHigh     = "high"
)

func (e *Engine) costMetric() int {
	for _, m := range []int{MReads, MCpu, MDur} {
		if e.has[m] {
			return m
		}
	}
	return -1
}

func shareOf(a Agg, tot Agg) [NMetrics]float64 {
	var s [NMetrics]float64
	for k := 0; k < NMetrics; k++ {
		if tot.Sum[k] > 0 {
			s[k] = float64(a.Sum[k]) / float64(tot.Sum[k])
		}
	}
	return s
}

func (e *Engine) familyAgg(f *Family) Agg {
	var a Agg
	for _, ix := range f.Execs {
		a.add(&e.execs[ix], ix)
	}
	return a
}

func (e *Engine) totals() Agg {
	var t Agg
	for i := range e.execs {
		t.add(&e.execs[i], int32(i))
	}
	return t
}

func famTitle(f *Family) string {
	if f.Kind == "proc" {
		return f.Proc
	}
	t := strings.Join(strings.Fields(f.SampleSQL), " ")
	if len(t) > 160 {
		cut := 160
		for cut > 0 && (t[cut]&0xC0) == 0x80 {
			cut--
		}
		t = t[:cut] + "…"
	}
	return t
}

// Overview: lista de famílias + achados principais.
func (e *Engine) Overview() Overview {
	e.mu.Lock() // o cache de estatísticas é escrito aqui
	defer e.mu.Unlock()
	tot := e.totals()
	ov := Overview{
		Totals: tot, Has: e.has, CostMetric: e.costMetric(),
		Read: e.Read.Load(), Parsed: e.Parsed.Load(), Failed: e.Failed.Load(),
		FailKinds: e.failKind, Fails: e.fails,
		Options: map[string]bool{"normalizeLiterals": e.opts.NormalizeLiterals},
	}
	end := e.ended
	if end.IsZero() {
		ov.ElapsedMs = 0
	} else {
		ov.ElapsedMs = end.Sub(e.started).Milliseconds()
	}
	for _, f := range e.fams {
		d := e.detailLocked(f, tot)
		ov.Families = append(ov.Families, d.Summary)
		ov.Findings = append(ov.Findings, d.Findings...)
	}
	cm := ov.CostMetric
	sort.SliceStable(ov.Families, func(i, j int) bool {
		a, b := ov.Families[i], ov.Families[j]
		if cm >= 0 && a.Agg.Sum[cm] != b.Agg.Sum[cm] {
			return a.Agg.Sum[cm] > b.Agg.Sum[cm]
		}
		return a.Agg.Count > b.Agg.Count
	})
	famShare := map[int]float64{}
	for _, s := range ov.Families {
		if cm >= 0 {
			famShare[s.ID] = s.Share[cm]
		} else {
			famShare[s.ID] = s.ExecShare
		}
	}
	sort.SliceStable(ov.Findings, func(i, j int) bool {
		a, b := ov.Findings[i], ov.Findings[j]
		if a.Sev != b.Sev {
			return a.Sev > b.Sev
		}
		return famShare[a.Family] > famShare[b.Family]
	})
	if len(ov.Findings) > 200 {
		ov.Findings = ov.Findings[:200]
	}
	return ov
}

func (e *Engine) Detail(id int) (*FamilyDetail, bool) {
	e.mu.Lock() // o cache de estatísticas é escrito aqui
	defer e.mu.Unlock()
	if id < 0 || id >= len(e.fams) {
		return nil, false
	}
	return e.detailLocked(e.fams[id], e.totals()), true
}

func (e *Engine) detailLocked(f *Family, tot Agg) *FamilyDetail {
	if d, ok := e.statsCache[f.ID]; ok {
		return d
	}
	fa := e.familyAgg(f)
	d := &FamilyDetail{SQL: f.SampleSQL, Norm: f.Normalized}
	d.Summary = FamilySummary{
		ID: f.ID, Proc: f.Proc, Kind: f.Kind, Title: famTitle(f), Agg: fa,
		Share: shareOf(fa, tot), Params: len(f.Params),
	}
	if tot.Count > 0 {
		d.Summary.ExecShare = float64(fa.Count) / float64(tot.Count)
	}
	d.Params = e.paramStats(f, fa)
	d.Patterns = e.patternStats(f)
	d.Combos = e.combosLocked(f, fa, defaultComboParams(d.Params, d.Patterns))
	d.Shapes = e.shapesLocked(f, fa, d.Patterns)
	for k, v := range f.Decls {
		d.Decls = append(d.Decls, KV{k, v})
	}
	sort.Slice(d.Decls, func(i, j int) bool { return kvLess(d.Decls[i], d.Decls[j]) })
	d.Findings = e.findings(f, d, fa)
	for _, fd := range d.Findings {
		if fd.Sev > d.Summary.MaxSev {
			d.Summary.MaxSev = fd.Sev
		}
	}
	d.Summary.Findings = len(d.Findings)
	e.statsCache[f.ID] = d
	return d
}

func classOf(v string) string {
	switch {
	case v == "NULL", isMissing(v), v == "''", v == "DEFAULT":
		return v
	}
	if f, ok := parseNum(v); ok {
		if _, quoted := unq(v); !quoted {
			if f == 0 || f == -1 || f == 1 {
				return v
			}
			return "(number ≠ 0/1/-1)"
		}
	}
	if u, quoted := unq(v); quoted {
		nd := normDate(u)
		if strings.HasPrefix(nd, "1900-01-01") || strings.HasPrefix(nd, "1753-01-01") || strings.HasPrefix(nd, "9999-12-31") {
			return v
		}
		if len(u) >= 10 && u[4] == '-' && u[7] == '-' {
			return "(date)"
		}
		return "(text)"
	}
	return "(other)"
}

func (e *Engine) paramStats(f *Family, fa Agg) []ParamStat {
	out := make([]ParamStat, 0, len(f.Params))
	for pi, p := range f.Params {
		counts := map[int32]*Agg{}
		var absent Agg
		for _, ix := range f.Execs {
			ex := &e.execs[ix]
			if pi >= len(ex.Vals) || ex.Vals[pi] < 0 {
				absent.add(ex, ix)
				continue
			}
			a := counts[ex.Vals[pi]]
			if a == nil {
				a = &Agg{}
				counts[ex.Vals[pi]] = a
			}
			a.add(ex, ix)
		}
		ps := ParamStat{Index: pi, Name: p.Name, Pseudo: p.Pseudo, Overflow: p.Dict.overflow}
		for t, c := range p.Types {
			ps.Types = append(ps.Types, KV{t, c})
		}
		sort.Slice(ps.Types, func(i, j int) bool { return kvLess(ps.Types[i], ps.Types[j]) })
		if len(ps.Types) > 0 {
			ps.Type = ps.Types[0].K
		}
		var vs []ValueStat
		for id, a := range counts {
			vs = append(vs, ValueStat{Value: p.Dict.vals[id], Agg: *a})
		}
		if absent.Count > 0 {
			vs = append(vs, ValueStat{Value: absentValue, Agg: absent})
		}
		ps.Distinct = len(vs)
		var nulls int
		for i := range vs {
			vs[i].Pct = float64(vs[i].Agg.Count) / float64(fa.Count)
			vs[i].Share = shareOf(vs[i].Agg, fa)
			if vs[i].Value == "NULL" {
				nulls = vs[i].Agg.Count
			}
		}
		ps.NullPct = float64(nulls) / float64(fa.Count)
		sort.Slice(vs, func(i, j int) bool {
			if vs[i].Agg.Count != vs[j].Agg.Count {
				return vs[i].Agg.Count > vs[j].Agg.Count
			}
			return vs[i].Value < vs[j].Value
		})
		switch {
		case ps.Distinct <= 1:
			ps.Card = cardConstant
		case ps.Distinct <= lowCardLimit && !p.Dict.overflow:
			ps.Card = cardLow
		default:
			ps.Card = cardHigh
		}
		if ps.Card == cardHigh {
			cl := map[string]*ValueStat{}
			var order []string
			for _, v := range vs {
				c := classOf(v.Value)
				x := cl[c]
				if x == nil {
					x = &ValueStat{Value: c}
					cl[c] = x
					order = append(order, c)
				}
				mergeAgg(&x.Agg, v.Agg)
			}
			for _, c := range order {
				x := cl[c]
				x.Pct = float64(x.Agg.Count) / float64(fa.Count)
				x.Share = shareOf(x.Agg, fa)
				ps.Classes = append(ps.Classes, *x)
			}
			sort.SliceStable(ps.Classes, func(i, j int) bool { return ps.Classes[i].Agg.Count > ps.Classes[j].Agg.Count })
		}
		if len(vs) > 25 {
			for _, v := range vs[25:] {
				ps.Others += v.Agg.Count
			}
			vs = vs[:25]
		}
		ps.Top = vs
		out = append(out, ps)
	}
	return out
}

func mergeAgg(dst *Agg, src Agg) {
	if dst.Count == 0 {
		dst.Sample = src.Sample
	}
	dst.Count += src.Count
	for k := 0; k < NMetrics; k++ {
		dst.Sum[k] += src.Sum[k]
		if src.Max[k] > dst.Max[k] {
			dst.Max[k] = src.Max[k]
		}
	}
}

func defaultComboParams(ps []ParamStat, pats []PatternStat) []int {
	var sel []int
	used := map[int]bool{}
	byName := map[string]ParamStat{}
	for _, p := range ps {
		byName[strings.ToLower(p.Name)] = p
	}
	addP := func(p ParamStat) {
		if used[p.Index] || p.Card == cardConstant || p.Pseudo != "" || len(sel) >= 6 {
			return
		}
		if p.Card == cardHigh && len(p.Classes) < 2 {
			return
		}
		used[p.Index] = true
		sel = append(sel, p.Index)
	}
	for _, pt := range pats {
		if p, ok := byName[strings.ToLower(pt.Var)]; ok {
			addP(p)
		}
	}
	for _, p := range ps {
		if p.Card == cardLow && p.Distinct <= 10 {
			addP(p)
		}
	}
	return sel
}

// shapesLocked agrupa as execuções pelo "formato" dos catch-all: em cada
// predicado, se o filtro foi aplicado ou virou pass-through.
func (e *Engine) shapesLocked(f *Family, fa Agg, pats []PatternStat) ComboResult {
	res := ComboResult{}
	var use []PatternStat
	for _, p := range pats {
		if p.Kind == "or" {
			use = append(use, p)
		}
	}
	if len(use) == 0 {
		return res
	}
	for _, p := range use {
		res.Params = append(res.Params, p.Cond)
		res.Modes = append(res.Modes, "shape")
	}
	groups := map[string]*ComboRow{}
	var keys []string
	buf := make([]byte, len(use))
	for _, ix := range f.Execs {
		ex := &e.execs[ix]
		vals := make([]string, len(use))
		for k, p := range use {
			v, ok := EvalPattern(f, p.Pattern, ex)
			switch {
			case !ok:
				vals[k], buf[k] = "?", '?'
			case v:
				vals[k], buf[k] = "pass-through", 'T'
			default:
				vals[k], buf[k] = "filter", 'F'
			}
		}
		key := string(buf)
		g := groups[key]
		if g == nil {
			g = &ComboRow{Values: vals}
			groups[key] = g
			keys = append(keys, key)
		}
		g.Agg.add(ex, ix)
	}
	res.Distinct = len(groups)
	for _, k := range keys {
		g := groups[k]
		g.Pct = float64(g.Agg.Count) / float64(fa.Count)
		g.Share = shareOf(g.Agg, fa)
		res.Rows = append(res.Rows, *g)
	}
	sort.SliceStable(res.Rows, func(i, j int) bool { return res.Rows[i].Agg.Count > res.Rows[j].Agg.Count })
	if len(res.Rows) > 100 {
		res.Rows = res.Rows[:100]
	}
	return res
}

func (e *Engine) Combos(id int, idx []int) (ComboResult, bool) {
	e.mu.Lock() // o cache de estatísticas é escrito aqui
	defer e.mu.Unlock()
	if id < 0 || id >= len(e.fams) {
		return ComboResult{}, false
	}
	f := e.fams[id]
	return e.combosLocked(f, e.familyAgg(f), idx), true
}

func (e *Engine) combosLocked(f *Family, fa Agg, idx []int) ComboResult {
	res := ComboResult{}
	useClass := make([]bool, len(idx))
	valid := idx[:0:0]
	for _, pi := range idx {
		if pi >= 0 && pi < len(f.Params) {
			valid = append(valid, pi)
		}
	}
	idx = valid
	useClass = useClass[:len(idx)]
	for k, pi := range idx {
		p := f.Params[pi]
		res.Params = append(res.Params, p.Name)
		res.Indexes = append(res.Indexes, pi)
		if len(p.Dict.vals) > lowCardLimit || p.Dict.overflow {
			useClass[k] = true
			res.Modes = append(res.Modes, "class")
		} else {
			res.Modes = append(res.Modes, "value")
		}
	}
	groups := map[string]*ComboRow{}
	var keys []string
	var sb strings.Builder
	for _, ix := range f.Execs {
		ex := &e.execs[ix]
		sb.Reset()
		vals := make([]string, len(idx))
		for k, pi := range idx {
			v := f.valueOf(ex, pi)
			if useClass[k] {
				v = classOf(v)
			}
			vals[k] = v
			sb.WriteString(v)
			sb.WriteByte(0)
		}
		key := sb.String()
		g := groups[key]
		if g == nil {
			if len(groups) >= 200000 {
				key = "\x01outros"
				g = groups[key]
				if g == nil {
					o := make([]string, len(idx))
					for k := range o {
						o[k] = otherValues
					}
					g = &ComboRow{Values: o}
					groups[key] = g
					keys = append(keys, key)
				}
			} else {
				g = &ComboRow{Values: vals}
				groups[key] = g
				keys = append(keys, key)
			}
		}
		g.Agg.add(ex, ix)
	}
	res.Distinct = len(groups)
	rows := make([]ComboRow, 0, len(groups))
	for _, k := range keys {
		g := groups[k]
		g.Pct = float64(g.Agg.Count) / float64(fa.Count)
		g.Share = shareOf(g.Agg, fa)
		rows = append(rows, *g)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Agg.Count > rows[j].Agg.Count })
	if len(rows) > 100 {
		rows = rows[:100]
	}
	res.Rows = rows
	return res
}

func (e *Engine) patternStats(f *Family) []PatternStat {
	var out []PatternStat
	for _, p := range f.Patterns {
		ps := PatternStat{Pattern: p, Cond: p.Cond()}
		for _, ix := range f.Execs {
			ex := &e.execs[ix]
			v, ok := EvalPattern(f, p, ex)
			if !ok {
				continue
			}
			ps.Evaluated++
			if v {
				ps.True.add(ex, ix)
			} else {
				ps.False.add(ex, ix)
			}
		}
		if ps.Evaluated == 0 {
			continue // variável local do SQL, não é parâmetro
		}
		ps.TruePct = float64(ps.True.Count) / float64(ps.Evaluated)
		out = append(out, ps)
	}
	return out
}

func avg(a Agg, m int) float64 {
	if a.Count == 0 || m < 0 {
		return 0
	}
	return float64(a.Sum[m]) / float64(a.Count)
}

// findings produces structured findings (Msg + Args with raw numbers). The text, in
// English or pt-BR, is built by the UI (web/index.html), so both engines stay language-free.
func (e *Engine) findings(f *Family, d *FamilyDetail, fa Agg) []Finding {
	var out []Finding
	cm := e.costMetric()
	famShare := d.Summary.ExecShare
	if cm >= 0 {
		famShare = d.Summary.Share[cm]
	}
	relevant := famShare >= 0.05
	bump := func(s int) int {
		if relevant && s < 3 {
			return s + 1
		}
		return s
	}
	// average cost when the condition is true vs false
	withCost := func(args map[string]any, t, fl Agg) map[string]any {
		if cm >= 0 && t.Count > 0 && fl.Count > 0 {
			args["cost"] = map[string]any{"m": cm, "t": avg(t, cm), "f": avg(fl, cm)}
		}
		return args
	}

	// 1) catch-all predicates
	for _, p := range d.Patterns {
		tp := p.TruePct
		if p.Kind == "isnull" {
			sev := 1
			if tp >= 0.8 || (tp > 0.2 && tp < 0.8) {
				sev = 2
			}
			out = append(out, Finding{Sev: bump(sev), Family: f.ID, Msg: "isnull", Code: p.Snippet,
				Args: withCost(map[string]any{"var": p.Var, "pct": tp}, p.True, p.False)})
			continue
		}
		var sev int
		var msg string
		switch {
		case tp >= 0.999:
			sev, msg = 3, "catchall.always"
		case tp >= 0.8:
			sev, msg = 3, "catchall.mostly"
		case tp <= 0.001:
			sev, msg = 1, "catchall.never"
		case tp <= 0.2:
			sev, msg = 2, "catchall.rare"
		default:
			sev, msg = 2, "catchall.split"
		}
		out = append(out, Finding{Sev: bump(sev), Family: f.ID, Msg: msg, Code: p.Snippet,
			Args: withCost(map[string]any{"cond": p.Cond, "pct": tp}, p.True, p.False)})
	}

	// 2) dominant values / unequal cost per value
	inPattern := map[string]bool{}
	for _, p := range d.Patterns {
		inPattern[strings.ToLower(p.Var)] = true
	}
	var constants []string
	for _, ps := range d.Params {
		if ps.Card == cardConstant && ps.Pseudo == "" && fa.Count >= 20 {
			v := ""
			if len(ps.Top) > 0 {
				v = ps.Top[0].Value
			}
			constants = append(constants, ps.Name+" = "+v)
			continue
		}
		if ps.Card == cardConstant || ps.Pseudo == pseudoInVals || len(ps.Top) == 0 {
			continue
		}
		top := ps.Top[0]
		if top.Pct >= 0.9 && fa.Count >= 20 && ps.Pseudo == "" {
			fd := Finding{Sev: 1, Family: f.ID, Msg: "dominant",
				Args: map[string]any{"name": ps.Name, "value": top.Value, "pct": top.Pct}}
			if cm >= 0 {
				var rest Agg
				for _, v := range ps.Top[1:] {
					mergeAgg(&rest, v.Agg)
				}
				if rest.Count >= 5 {
					a1, a2 := avg(top.Agg, cm), avg(rest, cm)
					if a1 > 0 && (a2/a1 >= 10 || a1/a2 >= 10) {
						fd.Sev = 3
						fd.Msg = "dominant.sniff"
						fd.Args["m"], fd.Args["a1"], fd.Args["a2"] = cm, a1, a2
					}
				}
			}
			if fd.Sev < 3 && inPattern[strings.ToLower(ps.Name)] {
				continue // already covered by the catch-all finding
			}
			fd.Sev = bump(fd.Sev)
			out = append(out, fd)
			continue
		}
		if ps.Card == cardLow && cm >= 0 {
			minA, maxA := -1.0, -1.0
			var minV, maxV string
			thr := fa.Count / 100
			if thr < 5 {
				thr = 5
			}
			for _, v := range ps.Top {
				if v.Agg.Count < thr {
					continue
				}
				a := avg(v.Agg, cm)
				if minA < 0 || a < minA {
					minA, minV = a, v.Value
				}
				if a > maxA {
					maxA, maxV = a, v.Value
				}
			}
			if minA > 0 && maxA/minA >= 20 {
				out = append(out, Finding{Sev: bump(2), Family: f.ID, Msg: "unequal",
					Args: map[string]any{"name": ps.Name, "ratio": maxA / minA, "m": cm, "max": maxA, "maxV": maxV, "min": minA, "minV": minV}})
			}
		}
	}
	if len(constants) > 0 {
		sort.Strings(constants)
		show, more := constants, 0
		if len(show) > 8 {
			show, more = show[:8], len(constants)-8
		}
		out = append(out, Finding{Sev: 1, Family: f.ID, Msg: "constants",
			Args: map[string]any{"count": len(constants), "items": show, "more": more}})
	}

	// 3) disproportionately expensive combinations
	if cm >= 0 && len(d.Combos.Params) > 0 {
		for _, r := range d.Combos.Rows {
			if r.Pct > 0 && r.Share[cm] >= 0.2 && r.Share[cm]/r.Pct >= 3 {
				parts := []string{}
				for k, v := range r.Values {
					parts = append(parts, d.Combos.Params[k]+"="+v)
				}
				out = append(out, Finding{Sev: bump(2), Family: f.ID, Msg: "combo.costly",
					Args: map[string]any{"pct": r.Pct, "share": r.Share[cm], "m": cm, "parts": parts}})
			}
		}
	}

	// 4) parameter declaration variations (plan cache pollution)
	if len(d.Decls) > 1 {
		out = append(out, Finding{Sev: bump(1), Family: f.ID, Msg: "decls", Args: map[string]any{"count": len(d.Decls)}})
	}

	// 5) inline IN lists
	for _, ps := range d.Params {
		if ps.Pseudo != pseudoInVals || ps.Distinct <= 1 {
			continue
		}
		sev := 1
		if ps.Distinct >= 20 {
			sev = 2
		}
		out = append(out, Finding{Sev: bump(sev), Family: f.ID, Msg: "inlist.distinct",
			Args: map[string]any{"name": ps.Name, "count": ps.Distinct}})
	}
	for _, ps := range d.Params {
		if ps.Pseudo != pseudoInCnt {
			continue
		}
		maxN := 0
		for _, v := range ps.Top {
			if n, ok := parseNum(v.Value); ok && int(n) > maxN {
				maxN = int(n)
			}
		}
		if maxN >= 500 {
			out = append(out, Finding{Sev: bump(1), Family: f.ID, Msg: "inlist.size",
				Args: map[string]any{"name": strings.TrimPrefix(ps.Name, inCountPfx), "max": maxN}})
		}
	}
	return out
}

// kvLess ordena por contagem decrescente e, no empate, pela chave (ordem determinística,
// igual à do motor JS).
func kvLess(a, b KV) bool {
	if a.V != b.V {
		return a.V > b.V
	}
	return a.K < b.K
}
