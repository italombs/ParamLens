// Package engine parses rpc_completed statements, groups them into query families and computes
// parameter statistics and findings. web/engine.js is a line-by-line port of this package and
// must produce the same JSON (see parity_test.go and scripts/parity.js).
package engine

import (
	"bufio"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MDur = iota
	MCpu
	MReads
	MPhys
	MWrites
	MRows
	NMetrics
)

var MetricKeys = [NMetrics]string{"duration", "cpu", "reads", "physical", "writes", "rows"}

type Metrics [NMetrics]int64

// Row é uma linha lida da fonte (tabela ou .xel).
type Row struct {
	Stmt string
	M    Metrics
	Has  [NMetrics]bool
}

const (
	maxDistinct  = 50000
	absentValue  = "(absent)"
	otherValues  = "(other values…)"
	pseudoInVals = "in-values"
	pseudoInCnt  = "in-count"
	noValue      = "(no value)"
	inCountPfx   = "#items "
)

type ValueDict struct {
	idx      map[string]int32
	vals     []string
	overflow bool
}

func (d *ValueDict) intern(v string) int32 {
	if id, ok := d.idx[v]; ok {
		return id
	}
	if len(d.vals) >= maxDistinct {
		d.overflow = true
		return d.intern2(otherValues)
	}
	return d.intern2(v)
}

func (d *ValueDict) intern2(v string) int32 {
	if id, ok := d.idx[v]; ok {
		return id
	}
	id := int32(len(d.vals))
	d.vals = append(d.vals, v)
	d.idx[v] = id
	return id
}

type FParam struct {
	Name   string
	Pseudo string
	Types  map[string]int
	Dict   ValueDict
}

type Family struct {
	ID         int
	Proc       string
	Kind       string
	Normalized string
	SampleSQL  string
	Params     []*FParam
	pidx       map[string]int
	Decls      map[string]int
	Execs      []int32
	Patterns   []Pattern
}

func (f *Family) param(name, pseudo string) int {
	k := strings.ToLower(name)
	if i, ok := f.pidx[k]; ok {
		return i
	}
	p := &FParam{Name: name, Pseudo: pseudo, Types: map[string]int{}, Dict: ValueDict{idx: map[string]int32{}}}
	f.Params = append(f.Params, p)
	f.pidx[k] = len(f.Params) - 1
	return len(f.Params) - 1
}

// valueOf devolve o valor do parâmetro pi na execução e.
func (f *Family) valueOf(e *Exec, pi int) string {
	if pi < 0 || pi >= len(e.Vals) || e.Vals[pi] < 0 {
		return absentValue
	}
	return f.Params[pi].Dict.vals[e.Vals[pi]]
}

type Exec struct {
	Fam  int32
	Vals []int32
	M    Metrics
	Off  int64
	Len  int32
}

type ParseFail struct {
	Error string `json:"error"`
	Stmt  string `json:"stmt"`
}

type Options struct {
	NormalizeLiterals bool
	UnitMs            bool // duração/cpu vêm em ms (senão µs, padrão do XE)
}

type Engine struct {
	mu       sync.RWMutex
	opts     Options
	fams     []*Family
	byKey    map[uint64]*Family
	execs    []Exec
	has      [NMetrics]bool
	fails    []ParseFail
	failKind map[string]int

	tmp  *os.File
	tw   *bufio.Writer
	tOff int64

	Read    atomic.Int64
	Parsed  atomic.Int64
	Failed  atomic.Int64
	started time.Time
	ended   time.Time

	statsCache map[int]*FamilyDetail
}

func NewEngine(opts Options) (*Engine, error) {
	f, err := os.CreateTemp("", "paramlens-*.tmp")
	if err != nil {
		return nil, err
	}
	return &Engine{
		opts:       opts,
		byKey:      map[uint64]*Family{},
		failKind:   map[string]int{},
		tmp:        f,
		tw:         bufio.NewWriterSize(f, 1<<20),
		started:    time.Now(),
		statsCache: map[int]*FamilyDetail{},
	}, nil
}

func (e *Engine) Close() {
	if e.tmp != nil {
		name := e.tmp.Name()
		e.tmp.Close()
		os.Remove(name)
		e.tmp = nil
	}
}

// Started is when the analysis began (for the progress display).
func (e *Engine) Started() time.Time { return e.started }

func (e *Engine) Finish() {
	e.mu.Lock()
	e.tw.Flush()
	e.ended = time.Now()
	e.mu.Unlock()
}

func (e *Engine) fail(kind, stmt string) {
	e.Failed.Add(1)
	e.failKind[kind]++
	if len(e.fails) < 30 {
		if len(stmt) > 3000 {
			stmt = stmt[:3000] + " …"
		}
		e.fails = append(e.fails, ParseFail{Error: kind, Stmt: stmt})
	}
}

func hashKey(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

func (e *Engine) Ingest(r Row) {
	e.Read.Add(1)
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.opts.UnitMs {
		r.M[MDur] *= 1000
		r.M[MCpu] *= 1000
	}
	for k := 0; k < NMetrics; k++ {
		if r.Has[k] {
			e.has[k] = true
		}
	}
	stmt := strings.TrimSpace(r.Stmt)
	if stmt == "" {
		e.fail("empty statement", "")
		return
	}
	rpc, err := ParseRPC(stmt)
	if err != nil {
		e.fail(err.Error(), stmt)
		return
	}
	var key string
	var norm string
	var lists []InList
	if rpc.Kind == "proc" {
		key = "proc|" + strings.ToLower(rpc.Proc)
		norm = rpc.Proc
	} else {
		if rpc.SQL == "" {
			e.fail("inner SQL of "+rpc.Kind+" not found", stmt)
			return
		}
		norm, lists = NormalizeSQL(rpc.SQL, e.opts.NormalizeLiterals)
		key = "sql|" + norm
	}
	hk := hashKey(key)
	fam := e.byKey[hk]
	if fam == nil {
		fam = &Family{
			ID:         len(e.fams),
			Proc:       rpc.Proc,
			Kind:       rpc.Kind,
			Normalized: norm,
			SampleSQL:  rpc.SQL,
			pidx:       map[string]int{},
			Decls:      map[string]int{},
		}
		if rpc.Kind != "proc" {
			fam.Patterns = DetectPatterns(rpc.SQL)
		}
		e.fams = append(e.fams, fam)
		e.byKey[hk] = fam
	}
	if rpc.Decl != "" && len(fam.Decls) < 500 {
		fam.Decls[strings.Join(strings.Fields(rpc.Decl), " ")]++
	}

	type pv struct {
		pi  int
		vid int32
	}
	pairs := make([]pv, 0, len(rpc.Params)+2*len(lists))
	for _, p := range rpc.Params {
		pi := fam.param(p.Name, "")
		fp := fam.Params[pi]
		if p.Type != "" && len(fp.Types) < 50 {
			fp.Types[p.Type]++
		}
		pairs = append(pairs, pv{pi, fp.Dict.intern(p.Value)})
	}
	seen := map[string]int{}
	for _, l := range lists {
		lbl := l.Label
		if l.Not {
			lbl = "NOT IN " + lbl
		} else {
			lbl = "IN " + lbl
		}
		seen[lbl]++
		if seen[lbl] > 1 {
			lbl += " (" + itoa(seen[lbl]) + ")"
		}
		pi := fam.param(lbl, pseudoInVals)
		pairs = append(pairs, pv{pi, fam.Params[pi].Dict.intern(l.Values)})
		pc := fam.param(inCountPfx+lbl, pseudoInCnt)
		pairs = append(pairs, pv{pc, fam.Params[pc].Dict.intern(itoa(l.Count))})
	}
	vals := make([]int32, len(fam.Params))
	for k := range vals {
		vals[k] = -1
	}
	for _, p := range pairs {
		vals[p.pi] = p.vid
	}

	off := e.tOff
	n, _ := e.tw.WriteString(stmt)
	e.tOff += int64(n)

	fam.Execs = append(fam.Execs, int32(len(e.execs)))
	e.execs = append(e.execs, Exec{Fam: int32(fam.ID), Vals: vals, M: r.M, Off: off, Len: int32(n)})
	e.Parsed.Add(1)
}

func (e *Engine) Statement(execIdx int) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if execIdx < 0 || execIdx >= len(e.execs) {
		return "", os.ErrNotExist
	}
	ex := e.execs[execIdx]
	buf := make([]byte, ex.Len)
	if _, err := e.tmp.ReadAt(buf, ex.Off); err != nil {
		return "", err
	}
	return string(buf), nil
}

// TestScript transforma a chamada em DECLAREs + SQL, pronto para o SSMS.
func TestScript(stmt string) string {
	rpc, err := ParseRPC(stmt)
	if err != nil || rpc.Kind == "proc" || rpc.SQL == "" {
		return stmt
	}
	var sb strings.Builder
	sb.WriteString("-- Generated by ParamLens from a real execution\n")
	for _, p := range rpc.Params {
		if strings.HasPrefix(p.Name, "#") || p.Type == "" {
			continue
		}
		sb.WriteString("DECLARE " + p.Name + " " + p.Type)
		if p.Value != noValue && p.Value != "DEFAULT" && !strings.HasSuffix(p.Value, " bytes)") {
			v := p.Value
			if strings.HasPrefix(v, "'") && strings.HasPrefix(p.Type, "n") {
				v = "N" + v // preserva unicode em nvarchar/nchar
			}
			sb.WriteString(" = " + v)
		}
		sb.WriteString(";\n")
	}
	sb.WriteString("\n")
	sb.WriteString(rpc.SQL)
	sb.WriteString("\n")
	return sb.String()
}

// ---------- comparação de valores (para avaliar predicados) ----------

func unq(v string) (string, bool) {
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'"), true
	}
	return v, false
}

func parseNum(v string) (float64, bool) {
	s, _ := unq(v)
	s = strings.TrimSpace(strings.TrimPrefix(s, "$"))
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

func normDate(s string) string {
	s = strings.Replace(s, "T", " ", 1)
	if i := strings.IndexByte(s, '.'); i > 0 && len(s) > 10 && s[4] == '-' {
		frac := strings.TrimRight(s[i+1:], "0")
		if frac == "" {
			s = s[:i]
		}
	}
	s = strings.TrimSuffix(s, " 00:00:00")
	return s
}

func isNullish(v string) bool {
	return v == "NULL"
}

func isMissing(v string) bool {
	return v == absentValue || v == noValue || v == otherValues
}

// sqlEqual aproxima a semântica do SQL Server (collation CI, conversão implícita).
// Devolve (resultado, conhecido).
func sqlEqual(a, b string) (bool, bool) {
	if isMissing(a) || isMissing(b) {
		return false, false
	}
	if isNullish(a) || isNullish(b) {
		return false, true // comparação com NULL nunca é verdadeira
	}
	if a == b {
		return true, true
	}
	fa, oka := parseNum(a)
	fb, okb := parseNum(b)
	if oka && okb {
		return fa == fb, true
	}
	ua, _ := unq(a)
	ub, _ := unq(b)
	if strings.EqualFold(ua, ub) {
		return true, true
	}
	if strings.EqualFold(normDate(ua), normDate(ub)) {
		return true, true
	}
	// '' vs 0 etc. tratam como diferentes
	return false, true
}

// EvalPattern avalia o predicado para uma execução. ok=false se não for avaliável.
func EvalPattern(f *Family, p Pattern, ex *Exec) (bool, bool) {
	pi, ok := f.pidx[strings.ToLower(p.Var)]
	if !ok {
		return false, false
	}
	v := f.valueOf(ex, pi)
	switch p.Op {
	case "IS NULL":
		if isMissing(v) {
			return false, false
		}
		return isNullish(v), true
	case "IS NOT NULL":
		if isMissing(v) {
			return false, false
		}
		return !isNullish(v), true
	}
	var other string
	if p.RightVar != "" {
		pj, ok := f.pidx[strings.ToLower(p.RightVar)]
		if !ok {
			return false, false
		}
		other = f.valueOf(ex, pj)
	} else {
		other = p.RightLit
	}
	eq, known := sqlEqual(v, other)
	if !known {
		return false, false
	}
	if p.Op == "<>" {
		if isNullish(v) || isNullish(other) {
			return false, true
		}
		return !eq, true
	}
	return eq, true
}
