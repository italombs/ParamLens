package main

// Tokenizer T-SQL leve e tolerante. Não valida sintaxe: só separa tokens
// respeitando strings (com '' e prefixo N), [identificadores]], "aspas",
// comentários -- e /* aninhados */, variáveis @x, números e binários.

import (
	"strings"
)

type TokKind int

const (
	TkIdent TokKind = iota
	TkQIdent
	TkVar
	TkString
	TkNumber
	TkBinary
	TkOp
)

type Token struct {
	Kind TokKind
	Text string // texto original
	Val  string // para strings: conteúdo sem aspas e com '' desfeito
	Pos  int
}

func (t Token) Is(word string) bool {
	return t.Kind == TkIdent && strings.EqualFold(t.Text, word)
}

func (t Token) IsOp(op string) bool {
	return t.Kind == TkOp && t.Text == op
}

func (t Token) IsLiteral() bool {
	return t.Kind == TkString || t.Kind == TkNumber || t.Kind == TkBinary || t.Is("NULL")
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '#' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '$' || c == '@'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

var multiOps = []string{"<=", ">=", "<>", "!=", "!<", "!>", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "::"}

func Tokenize(s string) []Token {
	toks := make([]Token, 0, len(s)/4)
	n := len(s)
	i := 0
	for i < n {
		c := s[i]
		// espaços
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == '\v' {
			i++
			continue
		}
		// comentário de linha
		if c == '-' && i+1 < n && s[i+1] == '-' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		// comentário de bloco (aninhado)
		if c == '/' && i+1 < n && s[i+1] == '*' {
			depth := 0
			for i < n {
				if i+1 < n && s[i] == '/' && s[i+1] == '*' {
					depth++
					i += 2
				} else if i+1 < n && s[i] == '*' && s[i+1] == '/' {
					depth--
					i += 2
					if depth == 0 {
						break
					}
				} else {
					i++
				}
			}
			continue
		}
		start := i
		// strings N'..' ou '..'
		if c == '\'' || ((c == 'N' || c == 'n') && i+1 < n && s[i+1] == '\'') {
			if c != '\'' {
				i++
			}
			i++ // abre aspas
			var sb strings.Builder
			for i < n {
				if s[i] == '\'' {
					if i+1 < n && s[i+1] == '\'' {
						sb.WriteByte('\'')
						i += 2
						continue
					}
					i++
					break
				}
				sb.WriteByte(s[i])
				i++
			}
			toks = append(toks, Token{Kind: TkString, Text: s[start:i], Val: sb.String(), Pos: start})
			continue
		}
		// [identificador]
		if c == '[' {
			i++
			for i < n {
				if s[i] == ']' {
					if i+1 < n && s[i+1] == ']' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			toks = append(toks, Token{Kind: TkQIdent, Text: s[start:i], Pos: start})
			continue
		}
		// "identificador"
		if c == '"' {
			i++
			for i < n {
				if s[i] == '"' {
					if i+1 < n && s[i+1] == '"' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			toks = append(toks, Token{Kind: TkQIdent, Text: s[start:i], Pos: start})
			continue
		}
		// variável @x / @@x
		if c == '@' {
			i++
			for i < n && (isIdentChar(s[i])) {
				i++
			}
			toks = append(toks, Token{Kind: TkVar, Text: s[start:i], Pos: start})
			continue
		}
		// binário 0x...
		if c == '0' && i+1 < n && (s[i+1] == 'x' || s[i+1] == 'X') {
			i += 2
			for i < n && (isDigit(s[i]) || (s[i] >= 'a' && s[i] <= 'f') || (s[i] >= 'A' && s[i] <= 'F')) {
				i++
			}
			toks = append(toks, Token{Kind: TkBinary, Text: strings.ToLower(s[start:i]), Pos: start})
			continue
		}
		// número (inclui money $1.5, .5, 1e10)
		if isDigit(c) || (c == '.' && i+1 < n && isDigit(s[i+1])) || (c == '$' && i+1 < n && (isDigit(s[i+1]) || s[i+1] == '.')) {
			if c == '$' {
				i++
			}
			for i < n && (isDigit(s[i]) || s[i] == '.') {
				i++
			}
			if i < n && (s[i] == 'e' || s[i] == 'E') {
				j := i + 1
				if j < n && (s[j] == '+' || s[j] == '-') {
					j++
				}
				if j < n && isDigit(s[j]) {
					i = j
					for i < n && isDigit(s[i]) {
						i++
					}
				}
			}
			toks = append(toks, Token{Kind: TkNumber, Text: s[start:i], Pos: start})
			continue
		}
		// identificador / palavra-chave
		if isIdentStart(c) {
			for i < n && isIdentChar(s[i]) {
				i++
			}
			toks = append(toks, Token{Kind: TkIdent, Text: s[start:i], Pos: start})
			continue
		}
		// operadores
		matched := false
		if i+1 < n {
			two := s[i : i+2]
			for _, op := range multiOps {
				if two == op {
					toks = append(toks, Token{Kind: TkOp, Text: op, Pos: start})
					i += 2
					matched = true
					break
				}
			}
		}
		if matched {
			continue
		}
		toks = append(toks, Token{Kind: TkOp, Text: s[i : i+1], Pos: start})
		i++
	}
	return toks
}

// CanonLiteral devolve a forma canônica de um literal para estatística.
// Strings ficam entre aspas simples (sem o N), NULL vira NULL.
func CanonLiteral(t Token) string {
	switch t.Kind {
	case TkString:
		return "'" + strings.ReplaceAll(t.Val, "'", "''") + "'"
	case TkIdent:
		if t.Is("NULL") {
			return "NULL"
		}
		if t.Is("DEFAULT") {
			return "DEFAULT"
		}
		return t.Text
	case TkNumber:
		return t.Text
	}
	return t.Text
}

const maxValueLen = 200

func ClipValue(v string) string {
	if len(v) <= maxValueLen {
		return v
	}
	// corta em fronteira de rune
	cut := maxValueLen
	for cut > 0 && (v[cut]&0xC0) == 0x80 {
		cut--
	}
	return v[:cut] + "…(" + itoa(len(v)) + " bytes)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
