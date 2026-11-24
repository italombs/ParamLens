package engine

import (
	"sort"
	"strings"
)

// ---------- hard-coded values: literals of the SQL text that could be parameters ----------

// Literal is a value written into the SQL text: an inline IN list of literals, or a literal compared
// with a column (col = 6, col <> N'x', col LIKE 'a%', col BETWEEN 1 AND 9). Literals compared with a
// variable (@p = 0, the catch-all sentinels) or with another literal (1 = 1) are not reported, nor
// literals in other places (TOP 50, count(1), varchar(10), THEN 'x').
type Literal struct {
	Name  string // what it filters: "[EN].[STATE] <>", "IN [EN].[TYPE]"
	Label string // the column or expression: [EN].[STATE]
	List  bool
	Value string // scalar: canonical literal ('text', 12, -1, 0x0a); list: values separated by commas, strings unquoted
	Type  string // scalar: int, bigint, decimal, float, money, nvarchar, varchar, varbinary; list: nvarchar
	Comma bool   // list: an item has a comma inside (STRING_SPLIT would split it)
	start int    // span in the SQL text: the literal (with its sign) or the "( ... )" of the list
	end   int
}

var cmpOps = map[string]bool{"=": true, "<>": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true, "!<": true, "!>": true}

func isCmp(t Token) bool { return (t.Kind == TkOp && cmpOps[t.Text]) || t.Is("LIKE") }

// isOperand: a variable or a literal on the other side makes the literal a sentinel, not a filter value
func isValueTok(t Token) bool { return t.Kind == TkVar || t.IsLiteral() }

// ExtractLiterals returns the hard-coded values of an SQL text, in text order. Two texts of the same
// query (same normalized SQL) give the same literals in the same order.
func ExtractLiterals(sql string) []Literal {
	toks := Tokenize(sql)
	var out []Literal
	betweenEnd, betweenLabel := -1, ""
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Is("IN") && i+1 < len(toks) && toks[i+1].IsOp("(") {
			if vals, end, ok := readLiteralList(toks, i+2); ok {
				col, not := labelBefore(toks, i)
				lbl := "IN " + col
				if not {
					lbl = "NOT IN " + col
				}
				sort.SliceStable(vals, func(a, b int) bool { return lessValue(vals[a], vals[b]) })
				plain := make([]string, 0, len(vals))
				comma := false
				for _, v := range vals {
					switch {
					case v == "NULL":
						continue
					case strings.HasPrefix(v, "'"):
						plain = append(plain, strings.ReplaceAll(v[1:len(v)-1], "''", "'"))
						comma = comma || strings.Contains(v, ",")
					default:
						plain = append(plain, v)
					}
				}
				out = append(out, Literal{Name: lbl, Label: col, List: true, Value: strings.Join(plain, ","), Type: "nvarchar", Comma: comma,
					start: toks[i+1].Pos, end: toks[end].Pos + len(toks[end].Text)})
				i = end
				continue
			}
		}
		// a literal, with an optional sign when it follows an operator or keyword
		start, last := i, i
		if (t.IsOp("-") || t.IsOp("+")) && i+1 < len(toks) && toks[i+1].Kind == TkNumber && i > 0 &&
			(isCmp(toks[i-1]) || toks[i-1].Is("BETWEEN") || toks[i-1].Is("AND")) {
			last = i + 1
		} else if t.Kind != TkString && t.Kind != TkNumber && t.Kind != TkBinary {
			continue
		}
		lit := toks[last]
		value := CanonLiteral(lit)
		if last > start {
			if t.Text == "-" {
				value = "-" + value
			}
		}
		name, label := "", ""
		switch prev := tokAt(toks, start-1); {
		case isCmp(prev):
			if isValueTok(tokAt(toks, start-2)) {
				break
			}
			lbl, not := labelBefore(toks, start-1)
			if strings.EqualFold(lbl, "END") {
				lbl = "expr" // CASE ... END = lit
			}
			op := prev.Text
			if prev.Kind == TkIdent {
				op = "LIKE"
				if not {
					op = "NOT LIKE"
				}
			}
			name, label = lbl+" "+op, lbl
		case prev.Is("BETWEEN"):
			if isValueTok(tokAt(toks, start-2)) {
				break
			}
			lbl, _ := labelBefore(toks, start-1)
			name, label = lbl+" BETWEEN", lbl
			betweenEnd, betweenLabel = last, lbl
		case prev.Is("AND") && betweenEnd == start-2:
			name, label = betweenLabel+" AND", betweenLabel
		default:
			// literal on the left: 6 = col
			next := tokAt(toks, last+1)
			if last == start && isCmp(next) && next.Kind == TkOp && !tokAt(toks, start-1).IsOp("-") && !tokAt(toks, start-1).IsOp("+") {
				other := tokAt(toks, last+2)
				if other.Kind == TkIdent || other.Kind == TkQIdent {
					if !isValueTok(other) {
						label = labelAfter(toks, last+2)
						name = label + " " + next.Text
					}
				}
			}
		}
		if name == "" {
			continue
		}
		out = append(out, Literal{Name: name, Label: label, Value: value, Type: literalType(lit),
			start: toks[start].Pos, end: lit.Pos + len(lit.Text)})
		i = last
	}
	return out
}

func tokAt(toks []Token, i int) Token {
	if i < 0 || i >= len(toks) {
		return Token{Kind: TkOp}
	}
	return toks[i]
}

// labelAfter reads "ident(.ident)*" from i
func labelAfter(toks []Token, i int) string {
	var parts []string
	for i < len(toks) && (toks[i].Kind == TkIdent || toks[i].Kind == TkQIdent) {
		parts = append(parts, toks[i].Text)
		if i+2 < len(toks) && toks[i+1].IsOp(".") {
			i += 2
			continue
		}
		break
	}
	if len(parts) == 0 {
		return "expr"
	}
	return strings.Join(parts, ".")
}

// literalType is the SQL Server type of a literal token
func literalType(t Token) string {
	switch t.Kind {
	case TkString:
		if t.Text != "" && (t.Text[0] == 'N' || t.Text[0] == 'n') {
			return "nvarchar"
		}
		return "varchar"
	case TkBinary:
		return "varbinary"
	}
	s := t.Text
	switch {
	case strings.ContainsAny(s, "eE"):
		return "float"
	case strings.Contains(s, "$"):
		return "money"
	case strings.Contains(s, "."):
		return "decimal"
	}
	d := strings.TrimLeft(s, "0")
	switch {
	case len(d) < 10 || (len(d) == 10 && d <= "2147483647"):
		return "int"
	case len(d) < 19 || (len(d) == 19 && d <= "9223372036854775807"):
		return "bigint"
	}
	return "decimal"
}

// widenType: the type that holds values of both types
func widenType(a, b string) string {
	if a == b || b == "" {
		return a
	}
	if a == "" {
		return b
	}
	rank := map[string]int{"int": 1, "bigint": 2, "decimal": 3, "money": 3, "float": 4}
	ra, rb := rank[a], rank[b]
	switch {
	case ra > 0 && rb > 0:
		if ra == 3 && rb == 3 {
			return "decimal"
		}
		if ra > rb {
			return a
		}
		return b
	case (a == "varchar" && b == "nvarchar") || (a == "nvarchar" && b == "varchar"):
		return "nvarchar"
	}
	return "nvarchar"
}

// literalVar: a parameter name for a hard-coded value, from the last part of the column it filters
// ([EN].[STATEID] -> @STATEID, lists -> @TYPEID_list), unique among used (lower case)
func literalVar(label string, list bool, used map[string]bool) string {
	s := label
	if k := strings.LastIndexByte(s, '.'); k >= 0 {
		s = s[k+1:]
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '[' || r == ']' || r == '"':
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	base := strings.Trim(b.String(), "_")
	if base == "" || base == "expr" {
		base = "value"
	}
	if base[0] >= '0' && base[0] <= '9' {
		base = "v" + base
	}
	if list {
		base += "_list"
	}
	v, n := "@"+base, 2
	for used[strings.ToLower(v)] {
		v = "@" + base + "_" + itoa(n)
		n++
	}
	used[strings.ToLower(v)] = true
	return v
}
