package main

import (
	"errors"
	"strings"
)

// Param é um parâmetro de uma chamada RPC, já com o valor canônico.
type Param struct {
	Name   string // @nome (ou #1, #2 para posicionais sem declaração)
	Type   string // tipo vindo da lista de declaração (sp_executesql/sp_prepexec)
	Value  string // valor canônico (ex.: 0, 'abc', NULL)
	IsNull bool
}

// RPC é o resultado do parse do campo "statement" do rpc_completed.
type RPC struct {
	Proc   string // nome do procedimento chamado
	Kind   string // "sp_executesql", "sp_prepexec" ou "proc"
	SQL    string // SQL interno (sp_executesql/sp_prepexec)
	Decl   string // lista de declaração de parâmetros (texto original)
	Params []Param
}

type rpcArg struct {
	name   string
	tok    Token
	value  string
	isNull bool
	isStr  bool
	strVal string
}

var errNotExec = errors.New("não é uma chamada EXEC")

// ParseRPC interpreta textos como:
//
//	exec sp_executesql N'select ...',N'@a int,@b nvarchar(50)',@a=1,@b=N'x'
//	declare @p1 int set @p1=NULL exec sp_prepexec @p1 output,N'@P0 int',N'select ...',@P0=1 select @p1
//	exec dbo.MinhaProc @x=1,@y='a'
func ParseRPC(stmt string) (*RPC, error) {
	toks := Tokenize(stmt)
	i := -1
	for k, t := range toks {
		if t.Is("exec") || t.Is("execute") {
			i = k
			break
		}
	}
	if i < 0 {
		return nil, errNotExec
	}
	i++
	// exec @rc = proc
	if i+1 < len(toks) && toks[i].Kind == TkVar && toks[i+1].IsOp("=") {
		i += 2
	}
	// nome do procedimento (multi-parte)
	var nameParts []string
	for i < len(toks) {
		t := toks[i]
		if t.Kind == TkIdent || t.Kind == TkQIdent {
			nameParts = append(nameParts, unquoteIdent(t.Text))
			i++
			if i < len(toks) && toks[i].IsOp(".") {
				i++
				continue
			}
		}
		break
	}
	if len(nameParts) == 0 {
		return nil, errors.New("EXEC sem nome de procedimento")
	}
	proc := strings.Join(nameParts, ".")
	last := strings.ToLower(nameParts[len(nameParts)-1])

	// argumentos
	var args []rpcArg
	for i < len(toks) {
		var a rpcArg
		if i+1 < len(toks) && toks[i].Kind == TkVar && toks[i+1].IsOp("=") {
			a.name = toks[i].Text
			i += 2
		}
		if i >= len(toks) {
			break
		}
		t := toks[i]
		switch {
		case (t.IsOp("-") || t.IsOp("+")) && i+1 < len(toks) && toks[i+1].Kind == TkNumber:
			v := toks[i+1].Text
			if t.Text == "-" {
				v = "-" + v
			}
			a.value = v
			a.tok = toks[i+1]
			i += 2
		case t.Kind == TkString:
			a.value = CanonLiteral(t)
			a.isStr = true
			a.strVal = t.Val
			a.tok = t
			i++
		case t.Kind == TkNumber || t.Kind == TkBinary || t.Kind == TkVar:
			a.value = t.Text
			a.tok = t
			i++
		case t.Kind == TkIdent:
			if t.Is("NULL") {
				a.isNull = true
				a.value = "NULL"
			} else if t.Is("DEFAULT") {
				a.value = "DEFAULT"
			} else {
				a.value = "'" + t.Text + "'" // string sem aspas é permitida em EXEC proc
			}
			a.tok = t
			i++
		default:
			goto done
		}
		if i < len(toks) && (toks[i].Is("output") || toks[i].Is("out")) {
			i++
		}
		args = append(args, a)
		if i < len(toks) && toks[i].IsOp(",") {
			i++
			continue
		}
		break
	}
done:
	r := &RPC{Proc: proc}
	switch last {
	case "sp_executesql":
		r.Kind = "sp_executesql"
		rest := assignSystemArgs(r, args, []string{"@stmt", "@params"})
		r.Params = bindParams(r.Decl, rest)
	case "sp_prepexec":
		r.Kind = "sp_prepexec"
		// handle, params, stmt, valores...
		if len(args) > 0 {
			args = args[1:]
		}
		rest := assignSystemArgs(r, args, []string{"@params", "@stmt"})
		r.Params = bindParams(r.Decl, rest)
	default:
		r.Kind = "proc"
		for k, a := range args {
			name := a.name
			if name == "" {
				name = "#" + itoa(k+1)
			}
			r.Params = append(r.Params, Param{Name: name, Value: ClipValue(a.value), IsNull: a.isNull})
		}
	}
	return r, nil
}

// assignSystemArgs consome os argumentos de sistema (stmt/params) na ordem
// dada e devolve o resto.
func assignSystemArgs(r *RPC, args []rpcArg, order []string) []rpcArg {
	pos := 0
	var rest []rpcArg
	for _, a := range args {
		ln := strings.ToLower(a.name)
		switch {
		case ln == "@stmt" || ln == "@statement":
			r.SQL = a.strVal
		case ln == "@params" || ln == "@parameters":
			r.Decl = a.strVal
		case a.name == "" && pos < len(order):
			if order[pos] == "@stmt" {
				r.SQL = a.strVal
			} else {
				r.Decl = a.strVal
			}
			pos++
		default:
			rest = append(rest, a)
		}
	}
	return rest
}

type declParam struct {
	Name string
	Type string
}

// ParseDecl lê "@a int,@b nvarchar(50) OUTPUT,@c decimal(18,2)".
func ParseDecl(decl string) []declParam {
	toks := Tokenize(decl)
	var out []declParam
	var cur []Token
	depth := 0
	flush := func() {
		if len(cur) > 0 && cur[0].Kind == TkVar {
			var sb strings.Builder
			for _, t := range cur[1:] {
				if t.Is("output") || t.Is("out") || t.Is("readonly") {
					continue
				}
				if sb.Len() > 0 && (t.Kind == TkIdent || t.Kind == TkQIdent) && !strings.HasSuffix(sb.String(), "(") && !strings.HasSuffix(sb.String(), ".") {
					sb.WriteByte(' ')
				}
				sb.WriteString(t.Text)
			}
			out = append(out, declParam{Name: cur[0].Text, Type: strings.ToLower(sb.String())})
		}
		cur = cur[:0]
	}
	for _, t := range toks {
		if t.IsOp("(") {
			depth++
		} else if t.IsOp(")") {
			depth--
		}
		if t.IsOp(",") && depth == 0 {
			flush()
			continue
		}
		cur = append(cur, t)
	}
	flush()
	return out
}

func bindParams(decl string, args []rpcArg) []Param {
	dps := ParseDecl(decl)
	byName := make(map[string]int, len(dps))
	for k, d := range dps {
		byName[strings.ToLower(d.Name)] = k
	}
	params := make([]Param, len(dps))
	set := make([]bool, len(dps))
	for k, d := range dps {
		params[k] = Param{Name: d.Name, Type: d.Type}
	}
	pos := 0
	var extra []Param
	for _, a := range args {
		idx := -1
		if a.name != "" {
			if k, ok := byName[strings.ToLower(a.name)]; ok {
				idx = k
			}
		} else {
			for pos < len(params) && set[pos] {
				pos++
			}
			if pos < len(params) {
				idx = pos
				pos++
			}
		}
		if idx < 0 {
			name := a.name
			if name == "" {
				name = "#" + itoa(len(extra)+1)
			}
			extra = append(extra, Param{Name: name, Value: ClipValue(a.value), IsNull: a.isNull})
			continue
		}
		params[idx].Value = ClipValue(a.value)
		params[idx].IsNull = a.isNull
		set[idx] = true
	}
	for k := range params {
		if !set[k] {
			params[k].Value = "(sem valor)"
		}
	}
	return append(params, extra...)
}

func unquoteIdent(s string) string {
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		return strings.ReplaceAll(s[1:len(s)-1], "]]", "]")
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	return s
}
