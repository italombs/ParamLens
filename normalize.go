package main

import (
	"sort"
	"strings"
)

// InList é uma lista IN (...) de literais encontrada no SQL interno.
type InList struct {
	Label  string // coluna/expressão antes do IN, ex.: [ENMESSAGE].[MESSAGETYPEID]
	Not    bool
	Count  int
	Values string // valores ordenados, separados por vírgula
}

// NormalizeSQL colapsa espaços/comentários, troca listas IN de literais por
// IN (#lista) e, opcionalmente, todos os literais por ?. O texto normalizado
// define a "família" da query.
func NormalizeSQL(sql string, allLiterals bool) (string, []InList) {
	toks := Tokenize(sql)
	var sb strings.Builder
	sb.Grow(len(sql))
	var lists []InList
	write := func(s string) {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(s)
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Is("IN") && i+1 < len(toks) && toks[i+1].IsOp("(") {
			if vals, end, ok := readLiteralList(toks, i+2); ok {
				lbl, not := labelBefore(toks, i)
				sort.Slice(vals, func(a, b int) bool { return lessValue(vals[a], vals[b]) })
				lists = append(lists, InList{Label: lbl, Not: not, Count: len(vals), Values: ClipValue(strings.Join(vals, ","))})
				write("IN (#lista)")
				i = end
				continue
			}
		}
		switch t.Kind {
		case TkIdent:
			if allLiterals && t.Is("NULL") {
				write(t.Text)
			} else {
				write(strings.ToUpper(t.Text))
			}
		case TkString, TkNumber, TkBinary:
			if allLiterals {
				write("?")
			} else {
				write(t.Text)
			}
		default:
			write(t.Text)
		}
	}
	return sb.String(), lists
}

// readLiteralList lê "lit, lit, ... )" a partir de i. Devolve o índice do ')'.
func readLiteralList(toks []Token, i int) ([]string, int, bool) {
	var vals []string
	for i < len(toks) {
		neg := ""
		if (toks[i].IsOp("-") || toks[i].IsOp("+")) && i+1 < len(toks) && toks[i+1].Kind == TkNumber {
			if toks[i].Text == "-" {
				neg = "-"
			}
			i++
		}
		t := toks[i]
		if !t.IsLiteral() {
			return nil, 0, false
		}
		vals = append(vals, neg+CanonLiteral(t))
		i++
		if i >= len(toks) {
			return nil, 0, false
		}
		if toks[i].IsOp(")") {
			return vals, i, true
		}
		if !toks[i].IsOp(",") {
			return nil, 0, false
		}
		i++
	}
	return nil, 0, false
}

func labelBefore(toks []Token, inIdx int) (string, bool) {
	j := inIdx - 1
	not := false
	if j >= 0 && toks[j].Is("NOT") {
		not = true
		j--
	}
	var parts []string
	for j >= 0 && (toks[j].Kind == TkIdent || toks[j].Kind == TkQIdent || toks[j].Kind == TkVar) {
		parts = append([]string{toks[j].Text}, parts...)
		if j-1 >= 0 && toks[j-1].IsOp(".") {
			j -= 2
			continue
		}
		break
	}
	if len(parts) == 0 {
		return "expr", not
	}
	return strings.Join(parts, "."), not
}

func lessValue(a, b string) bool {
	fa, oka := parseNum(a)
	fb, okb := parseNum(b)
	if oka && okb {
		return fa < fb
	}
	return a < b
}

// Pattern é um predicado "catch-all" (ou ISNULL/COALESCE) detectado no SQL.
type Pattern struct {
	Kind     string `json:"kind"` // "or" | "isnull"
	Var      string `json:"var"`
	Op       string `json:"op"` // "=", "<>", "IS NULL", "IS NOT NULL"
	RightVar string `json:"rightVar,omitempty"`
	RightLit string `json:"rightLit,omitempty"`
	Snippet  string `json:"snippet"`
	Count    int    `json:"count"`
}

func (p Pattern) Cond() string {
	switch {
	case p.Op == "IS NULL" || p.Op == "IS NOT NULL":
		return p.Var + " " + p.Op
	case p.RightVar != "":
		return p.Var + " " + p.Op + " " + p.RightVar
	default:
		return p.Var + " " + p.Op + " " + p.RightLit
	}
}

// DetectPatterns procura predicados do tipo "@p = 0 OR col = @p",
// "@d = @nullDate OR col >= @d", "@p IS NULL OR ..." e ISNULL(@p, col).
func DetectPatterns(sql string) []Pattern {
	toks := Tokenize(sql)
	found := map[string]*Pattern{}
	var order []string
	add := func(p Pattern) {
		key := p.Kind + "|" + p.Cond()
		if e, ok := found[key]; ok {
			e.Count++
			return
		}
		p.Count = 1
		found[key] = &p
		order = append(order, key)
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		// ISNULL(@p, ...) / COALESCE(@p, ...)
		if (t.Is("ISNULL") || t.Is("COALESCE")) && i+2 < len(toks) && toks[i+1].IsOp("(") && toks[i+2].Kind == TkVar {
			add(Pattern{Kind: "isnull", Var: toks[i+2].Text, Op: "IS NULL", Snippet: snippet(toks, i, i+2)})
			continue
		}
		p, end, ok := comparisonAt(toks, i)
		if !ok {
			continue
		}
		if adjacentOr(toks, i, end) {
			p.Kind = "or"
			p.Snippet = snippet(toks, i, end)
			add(p)
		}
		i = end
	}
	out := make([]Pattern, 0, len(order))
	for _, k := range order {
		out = append(out, *found[k])
	}
	return out
}

func isEqOp(t Token) (string, bool) {
	if t.Kind != TkOp {
		return "", false
	}
	switch t.Text {
	case "=":
		return "=", true
	case "<>", "!=":
		return "<>", true
	}
	return "", false
}

// literal com sinal opcional a partir de i
func litAt(toks []Token, i int) (string, int, bool) {
	if i >= len(toks) {
		return "", 0, false
	}
	if (toks[i].IsOp("-") || toks[i].IsOp("+")) && i+1 < len(toks) && toks[i+1].Kind == TkNumber {
		v := toks[i+1].Text
		if toks[i].Text == "-" {
			v = "-" + v
		}
		return v, i + 1, true
	}
	if toks[i].IsLiteral() && !toks[i].Is("NULL") {
		return CanonLiteral(toks[i]), i, true
	}
	return "", 0, false
}

func comparisonAt(toks []Token, i int) (Pattern, int, bool) {
	t := toks[i]
	// o token anterior não pode ser "." nem operador aritmético (evita pegar pedaço de expressão)
	if i > 0 {
		pv := toks[i-1]
		if pv.Kind == TkOp && strings.Contains("+-*/%.&|^", pv.Text) {
			return Pattern{}, 0, false
		}
	}
	if t.Kind == TkVar && !strings.HasPrefix(t.Text, "@@") {
		// @p IS [NOT] NULL
		if i+2 < len(toks) && toks[i+1].Is("IS") {
			if toks[i+2].Is("NULL") {
				return Pattern{Var: t.Text, Op: "IS NULL"}, i + 2, true
			}
			if i+3 < len(toks) && toks[i+2].Is("NOT") && toks[i+3].Is("NULL") {
				return Pattern{Var: t.Text, Op: "IS NOT NULL"}, i + 3, true
			}
		}
		if i+2 < len(toks) {
			if op, ok := isEqOp(toks[i+1]); ok {
				if toks[i+2].Kind == TkVar && !followedByArith(toks, i+2) {
					return Pattern{Var: t.Text, Op: op, RightVar: toks[i+2].Text}, i + 2, true
				}
				if v, end, ok := litAt(toks, i+2); ok && !followedByArith(toks, end) {
					return Pattern{Var: t.Text, Op: op, RightLit: v}, end, true
				}
			}
		}
		return Pattern{}, 0, false
	}
	// lit = @p
	if v, end, ok := litAt(toks, i); ok && end+2 < len(toks) {
		if op, ok := isEqOp(toks[end+1]); ok && toks[end+2].Kind == TkVar && !followedByArith(toks, end+2) {
			return Pattern{Var: toks[end+2].Text, Op: op, RightLit: v}, end + 2, true
		}
	}
	return Pattern{}, 0, false
}

func followedByArith(toks []Token, i int) bool {
	if i+1 >= len(toks) {
		return false
	}
	n := toks[i+1]
	return n.Kind == TkOp && strings.Contains("+-*/%.&|^", n.Text)
}

func adjacentOr(toks []Token, start, end int) bool {
	j := start - 1
	for j >= 0 && toks[j].IsOp("(") {
		j--
	}
	if j >= 0 && toks[j].Is("OR") {
		return true
	}
	k := end + 1
	for k < len(toks) && toks[k].IsOp(")") {
		k++
	}
	return k < len(toks) && toks[k].Is("OR")
}

// snippet tenta devolver o grupo entre parênteses que contém o predicado.
func snippet(toks []Token, start, end int) string {
	l, r := start, end
	depth := 0
	found := false
	for j := start - 1; j >= 0 && start-j < 60; j-- {
		if toks[j].IsOp(")") {
			depth++
		} else if toks[j].IsOp("(") {
			if depth == 0 {
				l = j
				found = true
				break
			}
			depth--
		}
	}
	if found {
		depth = 0
		ok := false
		for k := end + 1; k < len(toks) && k-l < 80; k++ {
			if toks[k].IsOp("(") {
				depth++
			} else if toks[k].IsOp(")") {
				if depth == 0 {
					r = k
					ok = true
					break
				}
				depth--
			}
		}
		if !ok {
			l, r = start, end
			found = false
		}
	}
	if !found {
		l = start - 6
		if l < 0 {
			l = 0
		}
		r = end + 8
		if r >= len(toks) {
			r = len(toks) - 1
		}
	}
	parts := make([]string, 0, r-l+1)
	for k := l; k <= r; k++ {
		parts = append(parts, toks[k].Text)
	}
	s := strings.Join(parts, " ")
	s = strings.ReplaceAll(s, "( ", "(")
	s = strings.ReplaceAll(s, " )", ")")
	s = strings.ReplaceAll(s, " . ", ".")
	s = strings.ReplaceAll(s, " ,", ",")
	if !found {
		s = "… " + s + " …"
	}
	return s
}
