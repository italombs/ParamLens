/* ParamLens — motor de análise em JavaScript.
 *
 * Porta fiel de tsql.go, rpc.go, normalize.go, engine.go e stats.go: para a mesma
 * entrada produz o mesmo JSON que o motor Go (verificado por tools/parity.js).
 * Inclui o leitor do arquivo de exportação ("ParamLens TSV v1", ver docs/exportacao.md).
 *
 * Detalhes de compatibilidade com o Go (que trabalha em bytes UTF-8):
 *  - limites de tamanho (200 bytes por valor, 160 no título, 3000 nos exemplos de falha)
 *    são contados em bytes UTF-8;
 *  - ordenação de strings por code point (= ordem de bytes UTF-8);
 *  - ToUpper/ToLower por caractere (ß continua ß);
 *  - formatação %.Nf com arredondamento do Go.
 *
 * Roda no navegador (inclusive em file://) e no Node. Sem dependências.
 * Não use a sequência de fechamento de script nem abertura de comentário HTML aqui:
 * este arquivo é embutido inline no index.html.
 */
(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  else root.PL = api;
})(typeof self !== "undefined" ? self : this, function () {
"use strict";

/* ===================== utilidades compatíveis com o Go ===================== */

// espaços do unicode.IsSpace do Go (gerados por código: o arquivo fica 100% ASCII nesse trecho)
const WS_CODES = [9, 10, 11, 12, 13, 32, 0x85, 0xA0, 0x1680, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000];
const WS_CHARS = String.fromCharCode.apply(null, WS_CODES);
const FIELDS_RE = new RegExp("[" + WS_CODES.map(c => "\\u{" + c.toString(16) + "}").join("") + "]+", "u");
const ASCII_RE = /^[\x00-\x7f]*$/;

function isWS(ch) { return WS_CHARS.indexOf(ch) >= 0; }

// strings.TrimSpace
function trimSpace(s) {
  let a = 0, b = s.length;
  while (a < b && isWS(s[a])) a++;
  while (b > a && isWS(s[b - 1])) b--;
  return a === 0 && b === s.length ? s : s.slice(a, b);
}

// strings.Fields
function fields(s) {
  const t = trimSpace(s);
  return t === "" ? [] : t.split(FIELDS_RE);
}

// strings.ToUpper / ToLower: mapeamento por caractere (o Go não expande ß em SS).
function mapRunes(s, f) {
  if (ASCII_RE.test(s)) return f(s);
  let out = "";
  for (const ch of s) {
    const u = f(ch);
    out += u.length === ch.length ? u : ch;
  }
  return out;
}
const upperGo = s => mapRunes(s, x => x.toUpperCase());
const lowerGo = s => mapRunes(s, x => x.toLowerCase());
const equalFold = (a, b) => a === b || lowerGo(a) === lowerGo(b);

// tamanho em bytes UTF-8
function utf8Len(s) {
  let n = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 0x80) n += 1;
    else if (c < 0x800) n += 2;
    else if (c >= 0xD800 && c <= 0xDBFF && i + 1 < s.length && (s.charCodeAt(i + 1) & 0xFC00) === 0xDC00) { n += 4; i++; }
    else n += 3;
  }
  return n;
}

// maior prefixo (sem quebrar caracteres) com no máximo max bytes UTF-8
function utf8Prefix(s, max) {
  let n = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    let w = 3, u = 1;
    if (c < 0x80) w = 1;
    else if (c < 0x800) w = 2;
    else if (c >= 0xD800 && c <= 0xDBFF && i + 1 < s.length && (s.charCodeAt(i + 1) & 0xFC00) === 0xDC00) { w = 4; u = 2; }
    if (n + w > max) return s.slice(0, i);
    n += w;
    i += u - 1;
  }
  return s;
}

// byte i da codificação UTF-8 de s (só é comparado com ASCII; bytes não ASCII viram 0x80)
function byteAt(s, i) {
  let n = 0;
  for (let k = 0; k < s.length; k++) {
    const c = s.charCodeAt(k);
    let w = 3;
    if (c < 0x80) w = 1;
    else if (c < 0x800) w = 2;
    else if (c >= 0xD800 && c <= 0xDBFF && k + 1 < s.length && (s.charCodeAt(k + 1) & 0xFC00) === 0xDC00) w = 4;
    if (i < n + w) return w === 1 ? c : 0x80;
    n += w;
    if (w === 4) k++;
  }
  return -1;
}

// comparação por code point (igual à comparação de bytes UTF-8 do Go)
function cmpStr(a, b) {
  if (a === b) return 0;
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) {
    let x = a.charCodeAt(i), y = b.charCodeAt(i);
    if (x === y) continue;
    if (x >= 0xD800) x = x >= 0xE000 ? x - 0x800 : x + 0x2000;
    if (y >= 0xD800) y = y >= 0xE000 ? y - 0x800 : y + 0x2000;
    return x < y ? -1 : 1;
  }
  return a.length < b.length ? -1 : a.length > b.length ? 1 : 0;
}

// strconv.ParseFloat (sem hexadecimal)
const FLOAT_RE = /^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$/;
const INF_RE = /^[+-]?(?:inf|infinity)$/i;
function goParseFloat(s) {
  if (FLOAT_RE.test(s)) {
    const f = Number(s);
    return isFinite(f) ? f : null;
  }
  if (INF_RE.test(s)) return s[0] === "-" ? -Infinity : Infinity;
  if (/^nan$/i.test(s)) return NaN;
  return null;
}

/* ===================== tokenizer (tsql.go) ===================== */

const TK = { IDENT: 0, QIDENT: 1, VAR: 2, STRING: 3, NUMBER: 4, BINARY: 5, OP: 6 };
const MULTI_OPS = new Set(["<=", ">=", "<>", "!=", "!<", "!>", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "::"]);

// palavra em minúsculas
function is(t, w) { return t.k === TK.IDENT && t.t.length === w.length && t.t.toLowerCase() === w; }
function isOp(t, op) { return t.k === TK.OP && t.t === op; }
function isLiteral(t) { return t.k === TK.STRING || t.k === TK.NUMBER || t.k === TK.BINARY || is(t, "null"); }

function isIdentStart(c) { return c === 95 || c === 35 || (c >= 97 && c <= 122) || (c >= 65 && c <= 90) || c >= 0x80; }
function isIdentChar(c) { return isIdentStart(c) || (c >= 48 && c <= 57) || c === 36 || c === 64; }
function isDigit(c) { return c >= 48 && c <= 57; }
function isHex(c) { return isDigit(c) || (c >= 97 && c <= 102) || (c >= 65 && c <= 70); }

function tokenize(s) {
  const toks = [];
  const n = s.length;
  let i = 0;
  while (i < n) {
    const c = s.charCodeAt(i);
    if (c === 32 || c === 9 || c === 13 || c === 10 || c === 12 || c === 11) { i++; continue; }
    // -- comentário
    if (c === 45 && i + 1 < n && s.charCodeAt(i + 1) === 45) {
      while (i < n && s.charCodeAt(i) !== 10) i++;
      continue;
    }
    // /* comentário aninhado */
    if (c === 47 && i + 1 < n && s.charCodeAt(i + 1) === 42) {
      let depth = 0;
      while (i < n) {
        if (i + 1 < n && s.charCodeAt(i) === 47 && s.charCodeAt(i + 1) === 42) { depth++; i += 2; }
        else if (i + 1 < n && s.charCodeAt(i) === 42 && s.charCodeAt(i + 1) === 47) { depth--; i += 2; if (depth === 0) break; }
        else i++;
      }
      continue;
    }
    const start = i;
    // 'texto' ou N'texto'
    if (c === 39 || ((c === 78 || c === 110) && i + 1 < n && s.charCodeAt(i + 1) === 39)) {
      if (c !== 39) i++;
      i++;
      let val = "", seg = i;
      while (i < n) {
        if (s.charCodeAt(i) === 39) {
          if (i + 1 < n && s.charCodeAt(i + 1) === 39) { val += s.slice(seg, i + 1); i += 2; seg = i; continue; }
          val += s.slice(seg, i); i++; seg = -1;
          break;
        }
        i++;
      }
      if (seg >= 0) val += s.slice(seg, i);
      toks.push({ k: TK.STRING, t: s.slice(start, i), v: val, p: start });
      continue;
    }
    // [identificador]
    if (c === 91) {
      i++;
      while (i < n) {
        if (s.charCodeAt(i) === 93) {
          if (i + 1 < n && s.charCodeAt(i + 1) === 93) { i += 2; continue; }
          i++; break;
        }
        i++;
      }
      toks.push({ k: TK.QIDENT, t: s.slice(start, i), v: "", p: start });
      continue;
    }
    // "identificador"
    if (c === 34) {
      i++;
      while (i < n) {
        if (s.charCodeAt(i) === 34) {
          if (i + 1 < n && s.charCodeAt(i + 1) === 34) { i += 2; continue; }
          i++; break;
        }
        i++;
      }
      toks.push({ k: TK.QIDENT, t: s.slice(start, i), v: "", p: start });
      continue;
    }
    // @variável
    if (c === 64) {
      i++;
      while (i < n && isIdentChar(s.charCodeAt(i))) i++;
      toks.push({ k: TK.VAR, t: s.slice(start, i), v: "", p: start });
      continue;
    }
    // 0x binário
    if (c === 48 && i + 1 < n && (s.charCodeAt(i + 1) === 120 || s.charCodeAt(i + 1) === 88)) {
      i += 2;
      while (i < n && isHex(s.charCodeAt(i))) i++;
      toks.push({ k: TK.BINARY, t: s.slice(start, i).toLowerCase(), v: "", p: start });
      continue;
    }
    // número (inclui money $1.5, .5, 1e10)
    if (isDigit(c) || (c === 46 && i + 1 < n && isDigit(s.charCodeAt(i + 1))) ||
        (c === 36 && i + 1 < n && (isDigit(s.charCodeAt(i + 1)) || s.charCodeAt(i + 1) === 46))) {
      if (c === 36) i++;
      while (i < n && (isDigit(s.charCodeAt(i)) || s.charCodeAt(i) === 46)) i++;
      if (i < n && (s.charCodeAt(i) === 101 || s.charCodeAt(i) === 69)) {
        let j = i + 1;
        if (j < n && (s.charCodeAt(j) === 43 || s.charCodeAt(j) === 45)) j++;
        if (j < n && isDigit(s.charCodeAt(j))) {
          i = j;
          while (i < n && isDigit(s.charCodeAt(i))) i++;
        }
      }
      toks.push({ k: TK.NUMBER, t: s.slice(start, i), v: "", p: start });
      continue;
    }
    // identificador / palavra-chave
    if (isIdentStart(c)) {
      while (i < n && isIdentChar(s.charCodeAt(i))) i++;
      toks.push({ k: TK.IDENT, t: s.slice(start, i), v: "", p: start });
      continue;
    }
    // operadores
    if (i + 1 < n) {
      const two = s.slice(i, i + 2);
      if (MULTI_OPS.has(two)) { toks.push({ k: TK.OP, t: two, v: "", p: start }); i += 2; continue; }
    }
    toks.push({ k: TK.OP, t: s[i], v: "", p: start });
    i++;
  }
  return toks;
}

function canonLiteral(t) {
  switch (t.k) {
    case TK.STRING: return "'" + t.v.replace(/'/g, "''") + "'";
    case TK.IDENT:
      if (is(t, "null")) return "NULL";
      if (is(t, "default")) return "DEFAULT";
      return t.t;
  }
  return t.t;
}

const MAX_VALUE_LEN = 200;
function clipValue(v) {
  const n = utf8Len(v);
  if (n <= MAX_VALUE_LEN) return v;
  return utf8Prefix(v, MAX_VALUE_LEN) + "…(" + n + " bytes)";
}
function keepValue(v) { return v; }

/* ===================== parse do RPC (rpc.go) ===================== */

const ERR_NOT_EXEC = "not an EXEC call";

function unquoteIdent(s) {
  if (s.length >= 2 && s[0] === "[" && s[s.length - 1] === "]") return s.slice(1, -1).split("]]").join("]");
  if (s.length >= 2 && s[0] === '"' && s[s.length - 1] === '"') return s.slice(1, -1).split('""').join('"');
  return s;
}

// full = true keeps the values whole (no 200-byte cut), like ParseRPCFull
function parseRPC(stmt, full) {
  const cv = full ? keepValue : clipValue;
  const toks = tokenize(stmt);
  let i = -1;
  for (let k = 0; k < toks.length; k++) {
    if (is(toks[k], "exec") || is(toks[k], "execute")) { i = k; break; }
  }
  if (i < 0) return { err: ERR_NOT_EXEC };
  i++;
  if (i + 1 < toks.length && toks[i].k === TK.VAR && isOp(toks[i + 1], "=")) i += 2;
  const nameParts = [];
  while (i < toks.length) {
    const t = toks[i];
    if (t.k === TK.IDENT || t.k === TK.QIDENT) {
      nameParts.push(unquoteIdent(t.t));
      i++;
      if (i < toks.length && isOp(toks[i], ".")) { i++; continue; }
    }
    break;
  }
  if (!nameParts.length) return { err: "EXEC without a procedure name" };
  const proc = nameParts.join(".");
  const last = nameParts[nameParts.length - 1].toLowerCase();

  const args = [];
  while (i < toks.length) {
    const a = { name: "", value: "", isNull: false, isStr: false, strVal: "" };
    if (i + 1 < toks.length && toks[i].k === TK.VAR && isOp(toks[i + 1], "=")) { a.name = toks[i].t; i += 2; }
    if (i >= toks.length) break;
    const t = toks[i];
    if ((isOp(t, "-") || isOp(t, "+")) && i + 1 < toks.length && toks[i + 1].k === TK.NUMBER) {
      let v = toks[i + 1].t;
      if (t.t === "-") v = "-" + v;
      a.value = v;
      i += 2;
    } else if (t.k === TK.STRING) {
      a.value = canonLiteral(t); a.isStr = true; a.strVal = t.v; i++;
    } else if (t.k === TK.NUMBER || t.k === TK.BINARY || t.k === TK.VAR) {
      a.value = t.t; i++;
    } else if (t.k === TK.IDENT) {
      if (is(t, "null")) { a.isNull = true; a.value = "NULL"; }
      else if (is(t, "default")) a.value = "DEFAULT";
      else a.value = "'" + t.t + "'"; // string sem aspas é permitida em EXEC proc
      i++;
    } else break;
    if (i < toks.length && (is(toks[i], "output") || is(toks[i], "out"))) i++;
    args.push(a);
    if (i < toks.length && isOp(toks[i], ",")) { i++; continue; }
    break;
  }

  const r = { proc, kind: "", sql: "", decl: "", params: [] };
  if (last === "sp_executesql") {
    r.kind = "sp_executesql";
    const rest = assignSystemArgs(r, args, ["@stmt", "@params"]);
    r.params = bindParams(r.decl, rest, cv);
  } else if (last === "sp_prepexec") {
    r.kind = "sp_prepexec";
    // handle, params, stmt, valores...
    const rest = assignSystemArgs(r, args.slice(1), ["@params", "@stmt"]);
    r.params = bindParams(r.decl, rest, cv);
  } else {
    r.kind = "proc";
    args.forEach((a, k) => {
      r.params.push({ name: a.name || "#" + (k + 1), type: "", value: cv(a.value), isNull: a.isNull });
    });
  }
  return r;
}

// consome @stmt/@params (nomeados ou na ordem dada) e devolve os demais argumentos
function assignSystemArgs(r, args, order) {
  let pos = 0;
  const rest = [];
  for (const a of args) {
    const ln = a.name.toLowerCase();
    if (ln === "@stmt" || ln === "@statement") r.sql = a.strVal;
    else if (ln === "@params" || ln === "@parameters") r.decl = a.strVal;
    else if (a.name === "" && pos < order.length) {
      if (order[pos] === "@stmt") r.sql = a.strVal; else r.decl = a.strVal;
      pos++;
    } else rest.push(a);
  }
  return rest;
}

function parseDecl(decl) {
  const toks = tokenize(decl);
  const out = [];
  let cur = [];
  let depth = 0;
  const flush = () => {
    if (cur.length && cur[0].k === TK.VAR) {
      let sb = "";
      for (const t of cur.slice(1)) {
        if (is(t, "output") || is(t, "out") || is(t, "readonly")) continue;
        if (sb.length > 0 && (t.k === TK.IDENT || t.k === TK.QIDENT) && !sb.endsWith("(") && !sb.endsWith(".")) sb += " ";
        sb += t.t;
      }
      out.push({ name: cur[0].t, type: lowerGo(sb) });
    }
    cur = [];
  };
  for (const t of toks) {
    if (isOp(t, "(")) depth++;
    else if (isOp(t, ")")) depth--;
    if (isOp(t, ",") && depth === 0) { flush(); continue; }
    cur.push(t);
  }
  flush();
  return out;
}

function bindParams(decl, args, cv) {
  const dps = parseDecl(decl);
  const byName = new Map();
  dps.forEach((d, k) => byName.set(lowerGo(d.name), k));
  const params = dps.map(d => ({ name: d.name, type: d.type, value: "", isNull: false }));
  const set = new Array(dps.length).fill(false);
  let pos = 0;
  const extra = [];
  for (const a of args) {
    let idx = -1;
    if (a.name !== "") {
      const k = byName.get(lowerGo(a.name));
      if (k !== undefined) idx = k;
    } else {
      while (pos < params.length && set[pos]) pos++;
      if (pos < params.length) { idx = pos; pos++; }
    }
    if (idx < 0) {
      extra.push({ name: a.name || "#" + (extra.length + 1), type: "", value: cv(a.value), isNull: a.isNull });
      continue;
    }
    params[idx].value = cv(a.value);
    params[idx].isNull = a.isNull;
    set[idx] = true;
  }
  params.forEach((p, k) => { if (!set[k]) p.value = NO_VALUE; });
  return params.concat(extra);
}

/* ===================== normalização e padrões (normalize.go) ===================== */

function normalizeSQL(sql, allLiterals) {
  const toks = tokenize(sql);
  const parts = [];
  const lists = [];
  for (let i = 0; i < toks.length; i++) {
    const t = toks[i];
    if (is(t, "in") && i + 1 < toks.length && isOp(toks[i + 1], "(")) {
      const rl = readLiteralList(toks, i + 2);
      if (rl) {
        const lb = labelBefore(toks, i);
        const vals = rl.vals.slice().sort((a, b) => lessValue(a, b) ? -1 : lessValue(b, a) ? 1 : 0);
        lists.push({ label: lb.label, not: lb.not, count: vals.length, values: clipValue(vals.join(",")) });
        parts.push("IN (#lista)");
        i = rl.end;
        continue;
      }
    }
    switch (t.k) {
      case TK.IDENT:
        parts.push(allLiterals && is(t, "null") ? t.t : upperGo(t.t));
        break;
      case TK.STRING: case TK.NUMBER: case TK.BINARY:
        parts.push(allLiterals ? "?" : t.t);
        break;
      default:
        parts.push(t.t);
    }
  }
  return { norm: parts.join(" "), lists };
}

function readLiteralList(toks, i) {
  const vals = [];
  while (i < toks.length) {
    let neg = "";
    if ((isOp(toks[i], "-") || isOp(toks[i], "+")) && i + 1 < toks.length && toks[i + 1].k === TK.NUMBER) {
      if (toks[i].t === "-") neg = "-";
      i++;
    }
    const t = toks[i];
    if (!isLiteral(t)) return null;
    vals.push(neg + canonLiteral(t));
    i++;
    if (i >= toks.length) return null;
    if (isOp(toks[i], ")")) return { vals, end: i };
    if (!isOp(toks[i], ",")) return null;
    i++;
  }
  return null;
}

function labelBefore(toks, inIdx) {
  let j = inIdx - 1;
  let not = false;
  if (j >= 0 && is(toks[j], "not")) { not = true; j--; }
  const parts = [];
  while (j >= 0 && (toks[j].k === TK.IDENT || toks[j].k === TK.QIDENT || toks[j].k === TK.VAR)) {
    parts.unshift(toks[j].t);
    if (j - 1 >= 0 && isOp(toks[j - 1], ".")) { j -= 2; continue; }
    break;
  }
  return { label: parts.length ? parts.join(".") : "expr", not };
}

function lessValue(a, b) {
  const fa = parseNum(a), fb = parseNum(b);
  if (fa !== null && fb !== null) return fa < fb;
  return cmpStr(a, b) < 0;
}

/* ---------- hard-coded values (literals.go) ---------- */

const CMP_OPS = new Set(["=", "<>", "!=", "<", ">", "<=", ">=", "!<", "!>"]);
const NONE_TOK = { k: TK.OP, t: "", v: "", p: 0 };
function isCmp(t) { return (t.k === TK.OP && CMP_OPS.has(t.t)) || is(t, "like"); }
function isValueTok(t) { return t.k === TK.VAR || isLiteral(t); }
function tokAt(toks, i) { return i < 0 || i >= toks.length ? NONE_TOK : toks[i]; }

// IN lists of literals and literals compared with a column, in text order (see literals.go)
function extractLiterals(sql) {
  const toks = tokenize(sql);
  const out = [];
  let betweenEnd = -1, betweenLabel = "";
  for (let i = 0; i < toks.length; i++) {
    const t = toks[i];
    if (is(t, "in") && i + 1 < toks.length && isOp(toks[i + 1], "(")) {
      const rl = readLiteralList(toks, i + 2);
      if (rl) {
        const lb = labelBefore(toks, i);
        const vals = rl.vals.slice().sort((a, b) => lessValue(a, b) ? -1 : lessValue(b, a) ? 1 : 0);
        const plain = [];
        let comma = false;
        for (const v of vals) {
          if (v === "NULL") continue;
          if (v[0] === "'") { plain.push(v.slice(1, -1).replace(/''/g, "'")); comma = comma || v.includes(","); }
          else plain.push(v);
        }
        const e = toks[rl.end];
        out.push({ name: (lb.not ? "NOT IN " : "IN ") + lb.label, label: lb.label, list: true, value: plain.join(","), type: "nvarchar", comma,
          start: toks[i + 1].p, end: e.p + e.t.length });
        i = rl.end;
        continue;
      }
    }
    const start = i;
    let last = i;
    if ((isOp(t, "-") || isOp(t, "+")) && i + 1 < toks.length && toks[i + 1].k === TK.NUMBER && i > 0 &&
      (isCmp(toks[i - 1]) || is(toks[i - 1], "between") || is(toks[i - 1], "and"))) {
      last = i + 1;
    } else if (t.k !== TK.STRING && t.k !== TK.NUMBER && t.k !== TK.BINARY) {
      continue;
    }
    const lit = toks[last];
    let value = canonLiteral(lit);
    if (last > start && t.t === "-") value = "-" + value;
    let name = "", label = "";
    const prev = tokAt(toks, start - 1);
    if (isCmp(prev)) {
      if (!isValueTok(tokAt(toks, start - 2))) {
        const lb = labelBefore(toks, start - 1);
        let lbl = lb.label;
        if (lowerGo(lbl) === "end") lbl = "expr";
        let op = prev.t;
        if (prev.k === TK.IDENT) op = lb.not ? "NOT LIKE" : "LIKE";
        name = lbl + " " + op; label = lbl;
      }
    } else if (is(prev, "between")) {
      if (!isValueTok(tokAt(toks, start - 2))) {
        const lbl = labelBefore(toks, start - 1).label;
        name = lbl + " BETWEEN"; label = lbl;
        betweenEnd = last; betweenLabel = lbl;
      }
    } else if (is(prev, "and") && betweenEnd === start - 2) {
      name = betweenLabel + " AND"; label = betweenLabel;
    } else {
      const next = tokAt(toks, last + 1);
      if (last === start && isCmp(next) && next.k === TK.OP && !isOp(tokAt(toks, start - 1), "-") && !isOp(tokAt(toks, start - 1), "+")) {
        const other = tokAt(toks, last + 2);
        if ((other.k === TK.IDENT || other.k === TK.QIDENT) && !isValueTok(other)) {
          label = labelAfter(toks, last + 2);
          name = label + " " + next.t;
        }
      }
    }
    if (!name) continue;
    out.push({ name, label, list: false, value, type: literalType(lit), comma: false, start: toks[start].p, end: lit.p + lit.t.length });
    i = last;
  }
  return out;
}

function labelAfter(toks, i) {
  const parts = [];
  while (i < toks.length && (toks[i].k === TK.IDENT || toks[i].k === TK.QIDENT)) {
    parts.push(toks[i].t);
    if (i + 2 < toks.length && isOp(toks[i + 1], ".")) { i += 2; continue; }
    break;
  }
  return parts.length ? parts.join(".") : "expr";
}

function literalType(t) {
  if (t.k === TK.STRING) return t.t[0] === "N" || t.t[0] === "n" ? "nvarchar" : "varchar";
  if (t.k === TK.BINARY) return "varbinary";
  const s = t.t;
  if (/[eE]/.test(s)) return "float";
  if (s.includes("$")) return "money";
  if (s.includes(".")) return "decimal";
  const d = s.replace(/^0+/, "");
  if (d.length < 10 || (d.length === 10 && d <= "2147483647")) return "int";
  if (d.length < 19 || (d.length === 19 && d <= "9223372036854775807")) return "bigint";
  return "decimal";
}

const TYPE_RANK = { int: 1, bigint: 2, decimal: 3, money: 3, float: 4 };
function widenType(a, b) {
  if (a === b || b === "") return a;
  if (a === "") return b;
  const ra = TYPE_RANK[a] || 0, rb = TYPE_RANK[b] || 0;
  if (ra > 0 && rb > 0) {
    if (ra === 3 && rb === 3) return "decimal";
    return ra > rb ? a : b;
  }
  return "nvarchar";
}

function literalVar(label, list, used) {
  let s = label;
  const k = s.lastIndexOf(".");
  if (k >= 0) s = s.slice(k + 1);
  let b = "";
  for (const r of s) {
    if (r === "[" || r === "]" || r === '"') continue;
    b += /^[A-Za-z0-9_]$/.test(r) ? r : "_";
  }
  let base = b.replace(/^_+|_+$/g, "");
  if (base === "" || base === "expr") base = "value";
  if (base[0] >= "0" && base[0] <= "9") base = "v" + base;
  if (list) base += "_list";
  let v = "@" + base, n = 2;
  while (used.has(lowerGo(v))) v = "@" + base + "_" + (n++);
  used.add(lowerGo(v));
  return v;
}

function patternCond(p) {
  if (p.op === "IS NULL" || p.op === "IS NOT NULL") return p.var + " " + p.op;
  if (p.rightVar) return p.var + " " + p.op + " " + p.rightVar;
  return p.var + " " + p.op + " " + p.rightLit;
}

function mkPattern(v, op, rightVar, rightLit) {
  return { kind: "", var: v, op, rightVar: rightVar || "", rightLit: rightLit || "", snippet: "", count: 0 };
}

function detectPatterns(sql) {
  const toks = tokenize(sql);
  const found = new Map();
  const add = p => {
    const key = p.kind + "|" + patternCond(p);
    const e = found.get(key);
    if (e) { e.count++; return; }
    p.count = 1;
    found.set(key, p);
  };
  for (let i = 0; i < toks.length; i++) {
    const t = toks[i];
    if ((is(t, "isnull") || is(t, "coalesce")) && i + 2 < toks.length && isOp(toks[i + 1], "(") && toks[i + 2].k === TK.VAR) {
      const p = mkPattern(toks[i + 2].t, "IS NULL");
      p.kind = "isnull";
      p.snippet = snippet(toks, i, i + 2);
      add(p);
      continue;
    }
    const c = comparisonAt(toks, i);
    if (!c) continue;
    if (adjacentOr(toks, i, c.end)) {
      c.p.kind = "or";
      c.p.snippet = snippet(toks, i, c.end);
      add(c.p);
    }
    i = c.end;
  }
  return Array.from(found.values());
}

function isEqOp(t) {
  if (t.k !== TK.OP) return null;
  if (t.t === "=") return "=";
  if (t.t === "<>" || t.t === "!=") return "<>";
  return null;
}

function litAt(toks, i) {
  if (i >= toks.length) return null;
  if ((isOp(toks[i], "-") || isOp(toks[i], "+")) && i + 1 < toks.length && toks[i + 1].k === TK.NUMBER) {
    let v = toks[i + 1].t;
    if (toks[i].t === "-") v = "-" + v;
    return { v, end: i + 1 };
  }
  if (isLiteral(toks[i]) && !is(toks[i], "null")) return { v: canonLiteral(toks[i]), end: i };
  return null;
}

const ARITH = "+-*/%.&|^";

function comparisonAt(toks, i) {
  const t = toks[i];
  if (i > 0) {
    const pv = toks[i - 1];
    if (pv.k === TK.OP && ARITH.includes(pv.t)) return null;
  }
  if (t.k === TK.VAR && !t.t.startsWith("@@")) {
    if (i + 2 < toks.length && is(toks[i + 1], "is")) {
      if (is(toks[i + 2], "null")) return { p: mkPattern(t.t, "IS NULL"), end: i + 2 };
      if (i + 3 < toks.length && is(toks[i + 2], "not") && is(toks[i + 3], "null")) return { p: mkPattern(t.t, "IS NOT NULL"), end: i + 3 };
    }
    if (i + 2 < toks.length) {
      const op = isEqOp(toks[i + 1]);
      if (op) {
        if (toks[i + 2].k === TK.VAR && !followedByArith(toks, i + 2)) return { p: mkPattern(t.t, op, toks[i + 2].t), end: i + 2 };
        const l = litAt(toks, i + 2);
        if (l && !followedByArith(toks, l.end)) return { p: mkPattern(t.t, op, "", l.v), end: l.end };
      }
    }
    return null;
  }
  const l = litAt(toks, i);
  if (l && l.end + 2 < toks.length) {
    const op = isEqOp(toks[l.end + 1]);
    if (op && toks[l.end + 2].k === TK.VAR && !followedByArith(toks, l.end + 2)) {
      return { p: mkPattern(toks[l.end + 2].t, op, "", l.v), end: l.end + 2 };
    }
  }
  return null;
}

function followedByArith(toks, i) {
  if (i + 1 >= toks.length) return false;
  const n = toks[i + 1];
  return n.k === TK.OP && ARITH.includes(n.t);
}

function adjacentOr(toks, start, end) {
  let j = start - 1;
  while (j >= 0 && isOp(toks[j], "(")) j--;
  if (j >= 0 && is(toks[j], "or")) return true;
  let k = end + 1;
  while (k < toks.length && isOp(toks[k], ")")) k++;
  return k < toks.length && is(toks[k], "or");
}

function snippet(toks, start, end) {
  let l = start, r = end, depth = 0, found = false;
  for (let j = start - 1; j >= 0 && start - j < 60; j--) {
    if (isOp(toks[j], ")")) depth++;
    else if (isOp(toks[j], "(")) {
      if (depth === 0) { l = j; found = true; break; }
      depth--;
    }
  }
  if (found) {
    depth = 0;
    let ok = false;
    for (let k = end + 1; k < toks.length && k - l < 80; k++) {
      if (isOp(toks[k], "(")) depth++;
      else if (isOp(toks[k], ")")) {
        if (depth === 0) { r = k; ok = true; break; }
        depth--;
      }
    }
    if (!ok) { l = start; r = end; found = false; }
  }
  if (!found) {
    l = Math.max(0, start - 6);
    r = end + 8;
    if (r >= toks.length) r = toks.length - 1;
  }
  const parts = [];
  for (let k = l; k <= r; k++) parts.push(toks[k].t);
  let s = parts.join(" ");
  s = s.split("( ").join("(").split(" )").join(")").split(" . ").join(".").split(" ,").join(",");
  if (!found) s = "… " + s + " …";
  return s;
}

/* ===================== comparação de valores (engine.go) ===================== */

const ABSENT = "(absent)";
const OTHER_VALUES = "(other values…)";
const NO_VALUE = "(no value)";
const IN_COUNT_PFX = "#items ";
const CARD_CONSTANT = "constant", CARD_LOW = "low", CARD_HIGH = "high";
const PSEUDO_IN_VALS = "in-values";
const PSEUDO_IN_CNT = "in-count";
const MAX_DISTINCT = 50000;
const LOW_CARD = 30;

const M = { DUR: 0, CPU: 1, READS: 2, PHYS: 3, WRITES: 4, ROWS: 5 };
const NM = 6;

function unq(v) {
  if (v.length >= 2 && v[0] === "'" && v[v.length - 1] === "'") return { s: v.slice(1, -1).split("''").join("'"), q: true };
  return { s: v, q: false };
}

// número ou null
function parseNum(v) {
  let s = unq(v).s;
  if (s.startsWith("$")) s = s.slice(1);
  s = trimSpace(s);
  if (s === "") return null;
  return goParseFloat(s);
}

function normDate(s) {
  s = s.replace("T", " ");
  const i = s.indexOf(".");
  if (i > 0 && utf8Len(s) > 10 && byteAt(s, 4) === 45) {
    if (s.slice(i + 1).replace(/0+$/, "") === "") s = s.slice(0, i);
  }
  if (s.endsWith(" 00:00:00")) s = s.slice(0, -9);
  return s;
}

const isNullish = v => v === "NULL";
const isMissing = v => v === ABSENT || v === NO_VALUE || v === OTHER_VALUES;

// sqlEqual: {eq, known}
function sqlEqual(a, b) {
  if (isMissing(a) || isMissing(b)) return { eq: false, known: false };
  if (isNullish(a) || isNullish(b)) return { eq: false, known: true };
  if (a === b) return { eq: true, known: true };
  const fa = parseNum(a), fb = parseNum(b);
  if (fa !== null && fb !== null) return { eq: fa === fb, known: true };
  const ua = unq(a).s, ub = unq(b).s;
  if (equalFold(ua, ub)) return { eq: true, known: true };
  if (equalFold(normDate(ua), normDate(ub))) return { eq: true, known: true };
  return { eq: false, known: true };
}

/* ===================== motor (engine.go) ===================== */

class ValueDict {
  constructor() { this.idx = new Map(); this.vals = []; this.overflow = false; }
  intern(v) {
    const id = this.idx.get(v);
    if (id !== undefined) return id;
    if (this.vals.length >= MAX_DISTINCT) { this.overflow = true; return this.intern2(OTHER_VALUES); }
    return this.intern2(v);
  }
  intern2(v) {
    const id = this.idx.get(v);
    if (id !== undefined) return id;
    const n = this.vals.length;
    this.vals.push(v);
    this.idx.set(v, n);
    return n;
  }
}

class Family {
  constructor(id, rpc, norm) {
    this.id = id; this.proc = rpc.proc; this.kind = rpc.kind; this.normalized = norm; this.sampleSQL = rpc.sql;
    this.params = []; this.pidx = new Map(); this.decls = new Map(); this.execs = []; this.patterns = [];
  }
  param(name, pseudo) {
    const k = lowerGo(name);
    const i = this.pidx.get(k);
    if (i !== undefined) return i;
    this.params.push({ name, pseudo, types: new Map(), dict: new ValueDict() });
    this.pidx.set(k, this.params.length - 1);
    return this.params.length - 1;
  }
  valueOf(ex, pi) {
    if (pi < 0 || pi >= ex.vals.length || ex.vals[pi] < 0) return ABSENT;
    return this.params[pi].dict.vals[ex.vals[pi]];
  }
}

function evalPattern(f, p, ex) {
  const pi = f.pidx.get(lowerGo(p.var));
  if (pi === undefined) return null;
  const v = f.valueOf(ex, pi);
  if (p.op === "IS NULL") return isMissing(v) ? null : isNullish(v);
  if (p.op === "IS NOT NULL") return isMissing(v) ? null : !isNullish(v);
  let other;
  if (p.rightVar) {
    const pj = f.pidx.get(lowerGo(p.rightVar));
    if (pj === undefined) return null;
    other = f.valueOf(ex, pj);
  } else other = p.rightLit;
  const r = sqlEqual(v, other);
  if (!r.known) return null;
  if (p.op === "<>") {
    if (isNullish(v) || isNullish(other)) return false;
    return !r.eq;
  }
  return r.eq;
}

// event timestamp -> Unix ms (UTC), or null; an explicit offset is applied, none means UTC (timeline.go)
function parseTimestamp(s) {
  s = trimSpace(s == null ? "" : String(s));
  let p = 0;
  const isD = c => c >= "0" && c <= "9";
  const num = n => {
    if (p + n > s.length) return null;
    let v = 0;
    for (let k = 0; k < n; k++) { const c = s[p + k]; if (!isD(c)) return null; v = v * 10 + (c.charCodeAt(0) - 48); }
    p += n;
    return v;
  };
  const lit = set => { if (p < s.length && set.includes(s[p])) { p++; return true; } return false; };
  let y = num(4); if (y === null || !lit("-")) return null;
  const mo = num(2); if (mo === null || !lit("-")) return null;
  const d = num(2); if (d === null || !lit("T ")) return null;
  const h = num(2); if (h === null || !lit(":")) return null;
  const mi = num(2); if (mi === null) return null;
  let sec = 0, ms = 0;
  if (lit(":")) {
    sec = num(2); if (sec === null) return null;
    if (lit(".,")) {
      let k = 0;
      while (p < s.length && isD(s[p])) { if (k < 3) ms = ms * 10 + (s.charCodeAt(p) - 48); k++; p++; }
      if (k === 0) return null;
      for (; k < 3; k++) ms *= 10;
    }
  }
  while (p < s.length && s[p] === " ") p++;
  let off = 0;
  if (lit("Zz")) { /* UTC */ } else if (p < s.length && (s[p] === "+" || s[p] === "-")) {
    const sign = s[p] === "-" ? -1 : 1;
    p++;
    const oh = num(2); if (oh === null) return null;
    lit(":");
    const om = num(2); if (om === null || oh > 14 || om > 59) return null;
    off = sign * (oh * 60 + om);
  }
  if (p !== s.length || y < 1 || mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || sec > 59) return null;
  if (mo <= 2) y--;
  const era = Math.floor(y / 400), yoe = y - era * 400, mp = (mo + 9) % 12;
  const doy = Math.floor((153 * mp + 2) / 5) + d - 1;
  const doe = yoe * 365 + Math.floor(yoe / 4) - Math.floor(yoe / 100) + doy;
  const days = era * 146097 + doe - 719468;
  return (((days * 24 + h) * 60 + mi - off) * 60 + sec) * 1000 + ms;
}

class Engine {
  constructor(opts) {
    this.opts = { normalizeLiterals: !!(opts && opts.normalizeLiterals), unitMs: !!(opts && opts.unitMs) };
    this.fams = []; this.byKey = new Map(); this.execs = [];
    this.has = [false, false, false, false, false, false];
    this.fails = []; this.failKind = new Map();
    this.read = 0; this.parsed = 0; this.failed = 0;
    this.started = Date.now(); this.ended = 0;
    this.cache = new Map();
  }

  fail(kind, stmt) {
    this.failed++;
    this.failKind.set(kind, (this.failKind.get(kind) || 0) + 1);
    if (this.fails.length < 30) {
      if (utf8Len(stmt) > 3000) {
        // o Go corta no byte 3000; bytes de um caractere partido viram U+FFFD no JSON
        const p = utf8Prefix(stmt, 3000);
        stmt = p + String.fromCharCode(0xFFFD).repeat(3000 - utf8Len(p)) + " …";
      }
      this.fails.push({ error: kind, stmt });
    }
  }

  // linha que nem chegou a ser um statement (formato inválido, corte do SSMS…)
  ingestFailure(kind, stmt) {
    this.read++;
    this.fail(kind, trimSpace(stmt || ""));
  }

  // m: 6 números (µs, ou ms se unitMs); has: 6 booleanos; off/len: posição da linha no arquivo;
  // ts: timestamp as captured (parseTimestamp)
  ingest(stmtRaw, m, has, off, len, ts) {
    this.read++;
    const tms = parseTimestamp(ts);
    m = m.slice();
    if (this.opts.unitMs) { m[M.DUR] *= 1000; m[M.CPU] *= 1000; }
    for (let k = 0; k < NM; k++) if (has[k]) this.has[k] = true;
    const stmt = trimSpace(stmtRaw);
    if (stmt === "") { this.fail("empty statement", ""); return; }
    const rpc = parseRPC(stmt);
    if (rpc.err) { this.fail(rpc.err, stmt); return; }
    let key, norm, lists = [];
    if (rpc.kind === "proc") {
      key = "proc|" + lowerGo(rpc.proc);
      norm = rpc.proc;
    } else {
      if (rpc.sql === "") { this.fail("inner SQL of " + rpc.kind + " not found", stmt); return; }
      const nr = normalizeSQL(rpc.sql, this.opts.normalizeLiterals);
      norm = nr.norm; lists = nr.lists;
      key = "sql|" + norm;
    }
    let fam = this.byKey.get(key);
    if (!fam) {
      fam = new Family(this.fams.length, rpc, norm);
      if (rpc.kind !== "proc") fam.patterns = detectPatterns(rpc.sql);
      this.fams.push(fam);
      this.byKey.set(key, fam);
    }
    if (rpc.decl !== "" && fam.decls.size < 500) {
      const d = fields(rpc.decl).join(" ");
      fam.decls.set(d, (fam.decls.get(d) || 0) + 1);
    }
    const pairs = [];
    for (const p of rpc.params) {
      const pi = fam.param(p.name, "");
      const fp = fam.params[pi];
      if (p.type !== "" && fp.types.size < 50) fp.types.set(p.type, (fp.types.get(p.type) || 0) + 1);
      pairs.push(pi, fp.dict.intern(p.value));
    }
    const seen = new Map();
    for (const l of lists) {
      let lbl = (l.not ? "NOT IN " : "IN ") + l.label;
      const c = (seen.get(lbl) || 0) + 1;
      seen.set(lbl, c);
      if (c > 1) lbl += " (" + c + ")";
      const pi = fam.param(lbl, PSEUDO_IN_VALS);
      pairs.push(pi, fam.params[pi].dict.intern(l.values));
      const pc = fam.param(IN_COUNT_PFX + lbl, PSEUDO_IN_CNT);
      pairs.push(pc, fam.params[pc].dict.intern(String(l.count)));
    }
    const vals = new Int32Array(fam.params.length).fill(-1);
    for (let k = 0; k < pairs.length; k += 2) vals[pairs[k]] = pairs[k + 1];
    fam.execs.push(this.execs.length);
    this.execs.push({ fam: fam.id, vals, m, off: off || 0, len: len || 0, ts: tms });
    this.parsed++;
  }

  finish() { this.ended = Date.now(); }

  // executions of a query in capture order, with timestamp and metrics (timeline.go)
  timeline(id) {
    const f = this.fams[id];
    if (!f) return null;
    const tl = { family: f.id, has: this.has.slice(), withTs: 0, rows: [] };
    for (const ix of f.execs) {
      const e = this.execs[ix];
      if (e.ts !== null) tl.withTs++;
      tl.rows.push({ i: ix, ts: e.ts, m: e.m.slice() });
    }
    return tl;
  }

  execRef(ix) {
    const e = this.execs[ix];
    return e ? { off: e.off, len: e.len } : null;
  }

  /* ---------- parameter table and distributions (extract.go) ---------- */

  // exact values of every parameter of a query, one row per execution ("execs") or per distinct
  // combination ("combos"); readBatch(refs) re-reads the statements: Promise<string[]>
  async paramTable(id, mode, readBatch) {
    const f = this.fams[id];
    if (!f) return null;
    if (mode !== "combos") mode = "execs";
    const pt = { family: f.id, kind: f.kind, proc: f.proc, sql: f.sampleSQL, decl: "", mode, has: this.has.slice(), lists: 0, params: [], literals: [], sqlParts: [], rows: [], failed: 0 };
    let best = null;
    for (const [k, v] of f.decls) if (!best || v > best.v || (v === best.v && cmpStr(k, best.k) < 0)) best = { k, v };
    if (best) pt.decl = best.k;
    const colOf = new Array(f.params.length).fill(-1);
    let ncols = 0;
    f.params.forEach((p, i) => {
      if (p.pseudo) { if (p.pseudo === PSEUDO_IN_VALS) pt.lists++; return; }
      const types = [];
      for (const [k, v] of p.types) types.push({ k, v });
      types.sort((a, b) => (b.v - a.v) || cmpStr(a.k, b.k));
      pt.params.push({ name: p.name, types });
      colOf[i] = ncols++;
    });
    if (f.kind !== "proc") {
      const lits = extractLiterals(f.sampleSQL);
      if (lits.length) {
        const used = new Set();
        for (const p of f.params) used.add(lowerGo(p.name));
        for (const t of tokenize(f.sampleSQL)) if (t.k === TK.VAR) used.add(lowerGo(t.t));
        let prev = 0;
        for (const l of lits) {
          pt.sqlParts.push(f.sampleSQL.slice(prev, l.start));
          pt.literals.push({ name: l.name, var: literalVar(l.label, l.list, used), list: l.list, type: l.type, comma: l.comma, text: f.sampleSQL.slice(l.start, l.end) });
          prev = l.end;
        }
        pt.sqlParts.push(f.sampleSQL.slice(prev));
      }
    }
    const nlits = pt.literals.length;
    const byKey = new Map();
    const B = 2000;
    for (let s = 0; s < f.execs.length; s += B) {
      const ixs = f.execs.slice(s, s + B);
      const stmts = await readBatch(ixs.map(ix => this.execRef(ix)));
      ixs.forEach((ix, k) => {
        const ex = this.execs[ix];
        const vals = new Array(ncols).fill(ABSENT);
        const lits = new Array(nlits).fill(ABSENT);
        const rpc = stmts[k] == null ? null : parseRPC(stmts[k], true);
        if (!rpc || rpc.err) pt.failed++;
        else {
          for (const p of rpc.params) {
            const pi = f.pidx.get(lowerGo(p.name));
            if (pi !== undefined && colOf[pi] >= 0) vals[colOf[pi]] = p.value;
          }
          if (nlits) {
            const el = extractLiterals(rpc.sql);
            if (el.length === nlits) el.forEach((l, k) => { lits[k] = l.value; pt.literals[k].type = widenType(pt.literals[k].type, l.type); pt.literals[k].comma = pt.literals[k].comma || l.comma; });
          }
        }
        if (mode === "execs") { pt.rows.push({ values: vals, lits, count: 1, sum: ex.m.slice(), exec: ix }); return; }
        const key = vals.join("\x00") + "\x01" + lits.join("\x00");
        const r = byKey.get(key);
        if (r !== undefined) {
          const row = pt.rows[r];
          row.count++;
          for (let m = 0; m < NM; m++) row.sum[m] += ex.m[m];
          return;
        }
        byKey.set(key, pt.rows.length);
        pt.rows.push({ values: vals, lits, count: 1, sum: ex.m.slice(), exec: ix });
      });
    }
    if (mode === "combos") pt.rows.sort((a, b) => (b.count - a.count) || (a.exec - b.exec));
    return pt;
  }

  // nearest-rank percentiles of each metric, per query
  distributions() {
    const rank = (v, p) => v[Math.max(0, Math.floor((p * v.length + 99) / 100) - 1)];
    return this.fams.map(f => {
      const fd = { id: f.id, count: f.execs.length, sum: [0, 0, 0, 0, 0, 0], d: [null, null, null, null, null, null] };
      for (const ix of f.execs) for (let m = 0; m < NM; m++) fd.sum[m] += this.execs[ix].m[m];
      if (f.execs.length) {
        for (let m = 0; m < NM; m++) {
          if (!this.has[m]) continue;
          const v = Float64Array.from(f.execs, ix => this.execs[ix].m[m]).sort();
          fd.d[m] = { min: v[0], p50: rank(v, 50), p90: rank(v, 90), p95: rank(v, 95), p99: rank(v, 99), max: v[v.length - 1] };
        }
      }
      return fd;
    });
  }

  /* ---------- estatísticas (stats.go) ---------- */

  costMetric() {
    for (const m of [M.READS, M.CPU, M.DUR]) if (this.has[m]) return m;
    return -1;
  }

  familyAgg(f) {
    const a = newAgg();
    for (const ix of f.execs) aggAdd(a, this.execs[ix], ix);
    return a;
  }

  totals() {
    const t = newAgg();
    for (let i = 0; i < this.execs.length; i++) aggAdd(t, this.execs[i], i);
    return t;
  }

  overview() {
    const tot = this.totals();
    const failKinds = {};
    for (const [k, v] of this.failKind) failKinds[k] = v;
    const ov = {
      totals: tot, has: this.has.slice(), costMetric: this.costMetric(),
      read: this.read, parsed: this.parsed, failed: this.failed,
      failKinds, fails: this.fails.slice(),
      families: [], findings: [],
      elapsedMs: this.ended ? this.ended - this.started : 0,
      options: { normalizeLiterals: this.opts.normalizeLiterals },
    };
    for (const f of this.fams) {
      const d = this.detailOf(f, tot);
      ov.families.push(d.summary);
      for (const x of d.findings) ov.findings.push(x);
    }
    const cm = ov.costMetric;
    ov.families.sort((a, b) => {
      if (cm >= 0 && a.agg.sum[cm] !== b.agg.sum[cm]) return b.agg.sum[cm] - a.agg.sum[cm];
      return b.agg.count - a.agg.count;
    });
    const famShare = new Map();
    for (const s of ov.families) famShare.set(s.id, cm >= 0 ? s.share[cm] : s.execShare);
    ov.findings.sort((a, b) => {
      if (a.sev !== b.sev) return b.sev - a.sev;
      const x = famShare.get(a.family), y = famShare.get(b.family);
      return x > y ? -1 : x < y ? 1 : 0;
    });
    if (ov.findings.length > 200) ov.findings.length = 200;
    return ov;
  }

  detail(id) {
    if (!(id >= 0 && id < this.fams.length)) return null;
    return this.detailOf(this.fams[id], this.totals());
  }

  combos(id, idx) {
    if (!(id >= 0 && id < this.fams.length)) return null;
    const f = this.fams[id];
    return this.combosOf(f, this.familyAgg(f), idx || []);
  }

  detailOf(f, tot) {
    const cached = this.cache.get(f.id);
    if (cached) return cached;
    const fa = this.familyAgg(f);
    const d = { summary: null, sql: f.sampleSQL, norm: f.normalized, params: [], combos: null, shapes: null, patterns: [], decls: [], findings: [] };
    d.summary = {
      id: f.id, proc: f.proc, kind: f.kind, title: famTitle(f), agg: fa,
      share: shareOf(fa, tot), execShare: tot.count > 0 ? fa.count / tot.count : 0,
      params: f.params.length, findings: 0, maxSev: 0,
    };
    d.params = this.paramStats(f, fa);
    d.patterns = this.patternStats(f);
    d.combos = this.combosOf(f, fa, defaultComboParams(d.params, d.patterns));
    d.shapes = this.shapesOf(f, fa, d.patterns);
    for (const [k, v] of f.decls) d.decls.push({ k, v });
    d.decls.sort((a, b) => (b.v - a.v) || cmpStr(a.k, b.k));
    d.findings = this.findingsOf(f, d, fa);
    for (const fd of d.findings) if (fd.sev > d.summary.maxSev) d.summary.maxSev = fd.sev;
    d.summary.findings = d.findings.length;
    this.cache.set(f.id, d);
    return d;
  }

  paramStats(f, fa) {
    const out = [];
    f.params.forEach((p, pi) => {
      const counts = new Map();
      const absent = newAgg();
      for (const ix of f.execs) {
        const ex = this.execs[ix];
        if (pi >= ex.vals.length || ex.vals[pi] < 0) { aggAdd(absent, ex, ix); continue; }
        let a = counts.get(ex.vals[pi]);
        if (!a) { a = newAgg(); counts.set(ex.vals[pi], a); }
        aggAdd(a, ex, ix);
      }
      const ps = { index: pi, name: p.name, pseudo: p.pseudo, type: "", types: [], distinct: 0, overflow: p.dict.overflow,
        card: "", nullPct: 0, top: [], classes: [], others: 0 };
      for (const [t, c] of p.types) ps.types.push({ k: t, v: c });
      ps.types.sort((a, b) => (b.v - a.v) || cmpStr(a.k, b.k));
      if (ps.types.length) ps.type = ps.types[0].k;
      let vs = [];
      for (const [id, a] of counts) vs.push({ value: p.dict.vals[id], agg: a, pct: 0, share: null });
      if (absent.count > 0) vs.push({ value: ABSENT, agg: absent, pct: 0, share: null });
      ps.distinct = vs.length;
      let nulls = 0;
      for (const v of vs) {
        v.pct = v.agg.count / fa.count;
        v.share = shareOf(v.agg, fa);
        if (v.value === "NULL") nulls = v.agg.count;
      }
      ps.nullPct = nulls / fa.count;
      vs.sort((a, b) => (b.agg.count - a.agg.count) || cmpStr(a.value, b.value));
      if (ps.distinct <= 1) ps.card = CARD_CONSTANT;
      else if (ps.distinct <= LOW_CARD && !p.dict.overflow) ps.card = CARD_LOW;
      else ps.card = CARD_HIGH;
      if (ps.card === CARD_HIGH) {
        const cl = new Map();
        for (const v of vs) {
          const c = classOf(v.value);
          let x = cl.get(c);
          if (!x) { x = { value: c, agg: newAgg(), pct: 0, share: null }; cl.set(c, x); }
          mergeAgg(x.agg, v.agg);
        }
        for (const x of cl.values()) {
          x.pct = x.agg.count / fa.count;
          x.share = shareOf(x.agg, fa);
          ps.classes.push(x);
        }
        ps.classes.sort((a, b) => b.agg.count - a.agg.count);
      }
      if (vs.length > 25) {
        for (const v of vs.slice(25)) ps.others += v.agg.count;
        vs = vs.slice(0, 25);
      }
      ps.top = vs;
      out.push(ps);
    });
    return out;
  }

  shapesOf(f, fa, pats) {
    const res = { params: [], indexes: [], modes: [], distinct: 0, rows: [] };
    const use = pats.filter(p => p.kind === "or");
    if (!use.length) return res;
    for (const p of use) { res.params.push(p.cond); res.modes.push("shape"); }
    const groups = new Map();
    for (const ix of f.execs) {
      const ex = this.execs[ix];
      const vals = new Array(use.length);
      let key = "";
      use.forEach((p, k) => {
        const v = evalPattern(f, p, ex);
        if (v === null) { vals[k] = "?"; key += "?"; }
        else if (v) { vals[k] = "pass-through"; key += "T"; }
        else { vals[k] = "filter"; key += "F"; }
      });
      let g = groups.get(key);
      if (!g) { g = { values: vals, agg: newAgg(), pct: 0, share: null }; groups.set(key, g); }
      aggAdd(g.agg, ex, ix);
    }
    res.distinct = groups.size;
    for (const g of groups.values()) {
      g.pct = g.agg.count / fa.count;
      g.share = shareOf(g.agg, fa);
      res.rows.push(g);
    }
    res.rows.sort((a, b) => b.agg.count - a.agg.count);
    if (res.rows.length > 100) res.rows.length = 100;
    return res;
  }

  combosOf(f, fa, idxIn) {
    const res = { params: [], indexes: [], modes: [], distinct: 0, rows: [] };
    const idx = idxIn.filter(pi => pi >= 0 && pi < f.params.length);
    const useClass = idx.map(pi => {
      const p = f.params[pi];
      res.params.push(p.name);
      res.indexes.push(pi);
      const c = p.dict.vals.length > LOW_CARD || p.dict.overflow;
      res.modes.push(c ? "class" : "value");
      return c;
    });
    const groups = new Map();
    for (const ix of f.execs) {
      const ex = this.execs[ix];
      const vals = new Array(idx.length);
      let key = "";
      for (let k = 0; k < idx.length; k++) {
        let v = f.valueOf(ex, idx[k]);
        if (useClass[k]) v = classOf(v);
        vals[k] = v;
        key += v + "\u0000";
      }
      let g = groups.get(key);
      if (!g) {
        if (groups.size >= 200000) {
          key = "\u0001outros";
          g = groups.get(key);
          if (!g) {
            g = { values: idx.map(() => OTHER_VALUES), agg: newAgg(), pct: 0, share: null };
            groups.set(key, g);
          }
        } else {
          g = { values: vals, agg: newAgg(), pct: 0, share: null };
          groups.set(key, g);
        }
      }
      aggAdd(g.agg, ex, ix);
    }
    res.distinct = groups.size;
    const rows = [];
    for (const g of groups.values()) {
      g.pct = g.agg.count / fa.count;
      g.share = shareOf(g.agg, fa);
      rows.push(g);
    }
    rows.sort((a, b) => b.agg.count - a.agg.count);
    if (rows.length > 100) rows.length = 100;
    res.rows = rows;
    return res;
  }

  patternStats(f) {
    const out = [];
    for (const p of f.patterns) {
      const ps = Object.assign({}, p, { cond: patternCond(p), evaluated: 0, true: newAgg(), false: newAgg(), truePct: 0 });
      for (const ix of f.execs) {
        const ex = this.execs[ix];
        const v = evalPattern(f, p, ex);
        if (v === null) continue;
        ps.evaluated++;
        aggAdd(v ? ps.true : ps.false, ex, ix);
      }
      if (ps.evaluated === 0) continue; // variável local do SQL, não é parâmetro
      ps.truePct = ps.true.count / ps.evaluated;
      out.push(ps);
    }
    return out;
  }

  // Structured findings (msg + args with raw numbers); the UI renders the text.
  findingsOf(f, d, fa) {
    const out = [];
    const cm = this.costMetric();
    const famShare = cm >= 0 ? d.summary.share[cm] : d.summary.execShare;
    const relevant = famShare >= 0.05;
    const bump = s => (relevant && s < 3 ? s + 1 : s);
    const withCost = (args, t, fl) => {
      if (cm >= 0 && t.count > 0 && fl.count > 0) args.cost = { m: cm, t: avg(t, cm), f: avg(fl, cm) };
      return args;
    };
    const F = (sev, msg, args, code) => {
      const x = { sev, family: f.id, msg, args };
      if (code) x.code = code;
      return x;
    };

    // 1) catch-all predicates
    for (const p of d.patterns) {
      const tp = p.truePct;
      if (p.kind === "isnull") {
        const sev = tp >= 0.8 || (tp > 0.2 && tp < 0.8) ? 2 : 1;
        out.push(F(bump(sev), "isnull", withCost({ var: p.var, pct: tp }, p.true, p.false), p.snippet));
        continue;
      }
      let sev, msg;
      if (tp >= 0.999) { sev = 3; msg = "catchall.always"; }
      else if (tp >= 0.8) { sev = 3; msg = "catchall.mostly"; }
      else if (tp <= 0.001) { sev = 1; msg = "catchall.never"; }
      else if (tp <= 0.2) { sev = 2; msg = "catchall.rare"; }
      else { sev = 2; msg = "catchall.split"; }
      out.push(F(bump(sev), msg, withCost({ cond: p.cond, pct: tp }, p.true, p.false), p.snippet));
    }

    // 2) dominant values / unequal cost per value
    const inPattern = new Set(d.patterns.map(p => lowerGo(p.var)));
    const constants = [];
    for (const ps of d.params) {
      if (ps.card === CARD_CONSTANT && ps.pseudo === "" && fa.count >= 20) {
        constants.push(ps.name + " = " + (ps.top.length ? ps.top[0].value : ""));
        continue;
      }
      if (ps.card === CARD_CONSTANT || ps.pseudo === PSEUDO_IN_VALS || !ps.top.length) continue;
      const top = ps.top[0];
      if (top.pct >= 0.9 && fa.count >= 20 && ps.pseudo === "") {
        const fd = F(1, "dominant", { name: ps.name, value: top.value, pct: top.pct });
        if (cm >= 0) {
          const rest = newAgg();
          for (const v of ps.top.slice(1)) mergeAgg(rest, v.agg);
          if (rest.count >= 5) {
            const a1 = avg(top.agg, cm), a2 = avg(rest, cm);
            if (a1 > 0 && (a2 / a1 >= 10 || a1 / a2 >= 10)) {
              fd.sev = 3;
              fd.msg = "dominant.sniff";
              fd.args.m = cm; fd.args.a1 = a1; fd.args.a2 = a2;
            }
          }
        }
        if (fd.sev < 3 && inPattern.has(lowerGo(ps.name))) continue; // already covered by the catch-all
        fd.sev = bump(fd.sev);
        out.push(fd);
        continue;
      }
      if (ps.card === CARD_LOW && cm >= 0) {
        let minA = -1, maxA = -1, minV = "", maxV = "";
        let thr = Math.trunc(fa.count / 100);
        if (thr < 5) thr = 5;
        for (const v of ps.top) {
          if (v.agg.count < thr) continue;
          const a = avg(v.agg, cm);
          if (minA < 0 || a < minA) { minA = a; minV = v.value; }
          if (a > maxA) { maxA = a; maxV = v.value; }
        }
        if (minA > 0 && maxA / minA >= 20) {
          out.push(F(bump(2), "unequal", { name: ps.name, ratio: maxA / minA, m: cm, max: maxA, maxV, min: minA, minV }));
        }
      }
    }
    if (constants.length) {
      constants.sort(cmpStr);
      const more = constants.length > 8 ? constants.length - 8 : 0;
      out.push(F(1, "constants", { count: constants.length, items: constants.slice(0, 8), more }));
    }

    // 3) disproportionately expensive combinations
    if (cm >= 0 && d.combos.params.length) {
      for (const r of d.combos.rows) {
        if (r.pct > 0 && r.share[cm] >= 0.2 && r.share[cm] / r.pct >= 3) {
          const parts = r.values.map((v, k) => d.combos.params[k] + "=" + v);
          out.push(F(bump(2), "combo.costly", { pct: r.pct, share: r.share[cm], m: cm, parts }));
        }
      }
    }

    // 4) parameter declaration variations (plan cache pollution)
    if (d.decls.length > 1) out.push(F(bump(1), "decls", { count: d.decls.length }));

    // 5) inline IN lists
    for (const ps of d.params) {
      if (ps.pseudo !== PSEUDO_IN_VALS || ps.distinct <= 1) continue;
      out.push(F(bump(ps.distinct >= 20 ? 2 : 1), "inlist.distinct", { name: ps.name, count: ps.distinct }));
    }
    for (const ps of d.params) {
      if (ps.pseudo !== PSEUDO_IN_CNT) continue;
      let maxN = 0;
      for (const v of ps.top) {
        const n = parseNum(v.value);
        if (n !== null && Math.trunc(n) > maxN) maxN = Math.trunc(n);
      }
      if (maxN >= 500) {
        out.push(F(bump(1), "inlist.size", { name: ps.name.startsWith(IN_COUNT_PFX) ? ps.name.slice(IN_COUNT_PFX.length) : ps.name, max: maxN }));
      }
    }
    return out;
  }
}

function newAgg() { return { count: 0, sum: [0, 0, 0, 0, 0, 0], max: [0, 0, 0, 0, 0, 0], sample: 0 }; }
function aggAdd(a, ex, idx) {
  if (a.count === 0) a.sample = idx;
  a.count++;
  for (let k = 0; k < NM; k++) {
    a.sum[k] += ex.m[k];
    if (ex.m[k] > a.max[k]) a.max[k] = ex.m[k];
  }
}
function mergeAgg(dst, src) {
  if (dst.count === 0) dst.sample = src.sample;
  dst.count += src.count;
  for (let k = 0; k < NM; k++) {
    dst.sum[k] += src.sum[k];
    if (src.max[k] > dst.max[k]) dst.max[k] = src.max[k];
  }
}
function shareOf(a, tot) {
  const s = [0, 0, 0, 0, 0, 0];
  for (let k = 0; k < NM; k++) if (tot.sum[k] > 0) s[k] = a.sum[k] / tot.sum[k];
  return s;
}
function avg(a, m) { return a.count === 0 || m < 0 ? 0 : a.sum[m] / a.count; }

function famTitle(f) {
  if (f.kind === "proc") return f.proc;
  const t = fields(f.sampleSQL).join(" ");
  if (utf8Len(t) > 160) return utf8Prefix(t, 160) + "…";
  return t;
}

function classOf(v) {
  if (v === "NULL" || isMissing(v) || v === "''" || v === "DEFAULT") return v;
  const q = unq(v);
  const f = parseNum(v);
  if (f !== null && !q.q) {
    if (f === 0 || f === -1 || f === 1) return v;
    return "(number ≠ 0/1/-1)";
  }
  if (q.q) {
    const u = q.s;
    const nd = normDate(u);
    if (nd.startsWith("1900-01-01") || nd.startsWith("1753-01-01") || nd.startsWith("9999-12-31")) return v;
    if (utf8Len(u) >= 10 && byteAt(u, 4) === 45 && byteAt(u, 7) === 45) return "(date)";
    return "(text)";
  }
  return "(other)";
}

function defaultComboParams(ps, pats) {
  const sel = [];
  const used = new Set();
  const byName = new Map();
  for (const p of ps) byName.set(lowerGo(p.name), p);
  const addP = p => {
    if (used.has(p.index) || p.card === CARD_CONSTANT || p.pseudo !== "" || sel.length >= 6) return;
    if (p.card === CARD_HIGH && p.classes.length < 2) return;
    used.add(p.index);
    sel.push(p.index);
  };
  for (const pt of pats) { const p = byName.get(lowerGo(pt.var)); if (p) addP(p); }
  for (const p of ps) if (p.card === CARD_LOW && p.distinct <= 10) addP(p);
  return sel;
}

// TestScript transforma a chamada em DECLAREs + SQL, pronto para o SSMS.
function testScript(stmt) {
  const rpc = parseRPC(stmt);
  if (rpc.err || rpc.kind === "proc" || rpc.sql === "") return stmt;
  let sb = "-- Generated by ParamLens from a real execution\n";
  for (const p of rpc.params) {
    if (p.name.startsWith("#") || p.type === "") continue;
    sb += "DECLARE " + p.name + " " + p.type;
    if (p.value !== NO_VALUE && p.value !== "DEFAULT" && !p.value.endsWith(" bytes)")) {
      let v = p.value;
      if (v.startsWith("'") && p.type.startsWith("n")) v = "N" + v; // preserva unicode em nvarchar/nchar
      sb += " = " + v;
    }
    sb += ";\n";
  }
  return sb + "\n" + rpc.sql + "\n";
}

/* ===================== arquivo de exportação (TSV v1) =====================
 * Uma linha por evento, colunas separadas por TAB:
 *   event, timestamp, duration, cpu_time, logical_reads, physical_reads, writes, row_count, statement
 * No statement: \ -> \\, CR -> \r, LF -> \n, TAB -> \t. Métricas vazias ou NULL = ausentes.
 * Cabeçalho opcional (1ª coluna "event"). Codificação detectada: UTF-16LE/BE (bcp -w),
 * UTF-8 (com ou sem BOM) ou Windows-1252.
 */

const EXPORT_COLS = 9;
const SSMS_CELL_LIMIT = 65535;
// failure reasons of the file reader (translated by the UI, like the engine ones)
const FAIL_FORMAT = "line does not match the format (expected " + EXPORT_COLS + " TAB-separated columns";
const FAIL_CSV = "; looks like CSV: save as tab-delimited text";
const FAIL_CUT = "statement cut at 65,535 characters (SSMS grid limit) — export with bcp";
const INT_RE = /^[+-]?\d+$/;

function detectEncoding(b) {
  if (b.length >= 2 && b[0] === 0xFF && b[1] === 0xFE) return { enc: "utf-16le", skip: 2, unit: 2, le: true };
  if (b.length >= 2 && b[0] === 0xFE && b[1] === 0xFF) return { enc: "utf-16be", skip: 2, unit: 2, le: false };
  if (b.length >= 3 && b[0] === 0xEF && b[1] === 0xBB && b[2] === 0xBF) return { enc: "utf-8", skip: 3, unit: 1, le: true };
  let zEven = 0, zOdd = 0;
  const n = Math.min(b.length, 8192) & ~1;
  for (let i = 0; i < n; i += 2) { if (b[i] === 0) zEven++; if (b[i + 1] === 0) zOdd++; }
  if (n >= 4) {
    if (zOdd > n / 2 * 0.3 && zOdd > zEven * 4) return { enc: "utf-16le", skip: 0, unit: 2, le: true };
    if (zEven > n / 2 * 0.3 && zEven > zOdd * 4) return { enc: "utf-16be", skip: 0, unit: 2, le: false };
  }
  try {
    // ignora um possível caractere cortado no fim da amostra
    let end = Math.min(b.length, 65536);
    if (end < b.length) { while (end > 0 && (b[end - 1] & 0xC0) === 0x80) end--; if (end > 0 && b[end - 1] >= 0xC0) end--; }
    new TextDecoder("utf-8", { fatal: true }).decode(b.subarray(0, end));
    return { enc: "utf-8", skip: 0, unit: 1, le: true };
  } catch (e) {
    return { enc: "windows-1252", skip: 0, unit: 1, le: true };
  }
}

const ENC_LABEL = { "utf-16le": "UTF-16 LE", "utf-16be": "UTF-16 BE", "utf-8": "UTF-8", "windows-1252": "Windows-1252" };

function unescapeStmt(s) {
  if (s.indexOf("\\") < 0) return s;
  return s.replace(/\\([\\rnt])/g, (m, c) => (c === "\\" ? "\\" : c === "r" ? "\r" : c === "n" ? "\n" : "\t"));
}

// divide uma linha do TSV; null se não tiver as 9 colunas
function splitExportLine(line) {
  const f = [];
  let pos = 0;
  for (let k = 0; k < EXPORT_COLS - 1; k++) {
    const t = line.indexOf("\t", pos);
    if (t < 0) return null;
    f.push(line.slice(pos, t));
    pos = t + 1;
  }
  f.push(line.slice(pos));
  return f;
}

function parseMetric(s) {
  s = s.trim();
  if (s === "" || !INT_RE.test(s)) return null;
  return Number(s);
}

// %, _ e [..] do LIKE (case-insensitive, como uma collation CI)
function likeToRegExp(pat) {
  let re = "";
  for (let i = 0; i < pat.length; i++) {
    const c = pat[i];
    if (c === "%") re += "[\\s\\S]*";
    else if (c === "_") re += "[\\s\\S]";
    else if (c === "[") {
      const j = pat.indexOf("]", i + 1);
      if (j < 0) { re += "\\["; continue; }
      let body = pat.slice(i + 1, j);
      let neg = false;
      if (body[0] === "^") { neg = true; body = body.slice(1); }
      re += "[" + (neg ? "^" : "") + body.replace(/[\\\]]/g, "\\$&") + "]";
      i = j;
    } else re += c.replace(/[.*+?^${}()|[\]\\\/]/g, "\\$&");
  }
  return new RegExp("^" + re + "$", "i");
}

const tick = () => new Promise(r => setTimeout(r, 0));

/* Percorre as linhas de um Blob/File sem carregá-lo inteiro na memória.
 * onLine(texto, offset, tamanhoEmBytes) — offset/tamanho excluem o terminador (LF ou CRLF). */
async function forEachLine(blob, encInfo, onLine, onProgress, isCanceled) {
  const dec = new TextDecoder(encInfo.enc);
  const reader = blob.slice(encInfo.skip).stream().getReader();
  const u2 = encInfo.unit === 2, le = encInfo.le;
  let carry = null, base = encInfo.skip, lastYield = Date.now();
  const emit = (buf, s, e) => {
    if (u2) { if (e - s >= 2 && (le ? buf[e - 2] === 0x0D && buf[e - 1] === 0 : buf[e - 2] === 0 && buf[e - 1] === 0x0D)) e -= 2; }
    else if (e > s && buf[e - 1] === 0x0D) e--;
    onLine(dec.decode(buf.subarray(s, e)), base + s, e - s);
  };
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    let buf = value;
    if (carry && carry.length) {
      buf = new Uint8Array(carry.length + value.length);
      buf.set(carry, 0);
      buf.set(value, carry.length);
    }
    let s = 0;
    if (!u2) {
      for (let j = buf.indexOf(0x0A, s); j >= 0; j = buf.indexOf(0x0A, s)) { emit(buf, s, j); s = j + 1; }
    } else {
      let j = s;
      for (;;) {
        j = buf.indexOf(0x0A, j);
        if (j < 0) break;
        const lead = le ? j : j - 1; // posição do 1º byte da unidade LF
        if ((lead - s) % 2 === 0 && lead >= s && lead + 1 < buf.length && (le ? buf[j + 1] === 0 : buf[j - 1] === 0)) {
          emit(buf, s, lead);
          s = lead + 2;
          j = s;
        } else j++;
      }
    }
    base += s;
    carry = buf.subarray(s);
    if (isCanceled && isCanceled()) { try { reader.cancel(); } catch (e) { /* ignora */ } return false; }
    if (Date.now() - lastYield > 60) {
      if (onProgress) onProgress(base - encInfo.skip, blob.size - encInfo.skip);
      await tick();
      lastYield = Date.now();
    }
  }
  if (carry && carry.length) emit(carry, 0, carry.length);
  if (onProgress) onProgress(blob.size - encInfo.skip, blob.size - encInfo.skip);
  return true;
}

/* ---------- delimited files with a header row (CSV/TXT saved from SSMS, SELECT *) ---------- */

// header names accepted for each field (case-insensitive, first match wins)
const HEADER_COLS = {
  event: ["event", "name", "event_name"],
  ts: ["timestamp (utc)", "timestamp_utc", "timestamp"],
  m: [["duration"], ["cpu_time", "cpu"], ["logical_reads", "reads"], ["physical_reads"], ["writes"], ["row_count", "rows"]],
  stmt: ["statement"],
};
const FAIL_COLS = "record does not match the header (expected {0} columns, found {1}; fields with commas or line breaks must be quoted)";

// one RFC 4180 record -> fields (quotes only open a field when they are its first character)
function parseDelimited(text, d) {
  const out = [];
  let cur = "", st = 0; // 0 field start, 1 unquoted, 2 quoted, 3 quote inside quoted
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (st === 2) { if (c === '"') st = 3; else cur += c; continue; }
    if (st === 3) {
      if (c === '"') { cur += '"'; st = 2; continue; }
      if (c === d) { out.push(cur); cur = ""; st = 0; continue; }
      cur += c; st = 1; continue;
    }
    if (c === d) { out.push(cur); cur = ""; st = 0; continue; }
    if (st === 0 && c === '"') { st = 2; continue; }
    cur += c; st = 1;
  }
  out.push(cur);
  return out;
}

// decides how to read the file from its first line
function detectFormat(firstLine) {
  const tabs = firstLine.split("\t");
  const low = s => s.trim().replace(/^"|"$/g, "").toLowerCase();
  if (tabs.length === EXPORT_COLS && low(tabs[0]) === "event" && low(tabs[EXPORT_COLS - 1]) === "statement") {
    return { kind: "tsv", header: true, label: "ParamLens TSV v1" };
  }
  const counts = [["\t", tabs.length - 1], [",", firstLine.split(",").length - 1], [";", firstLine.split(";").length - 1]];
  counts.sort((a, b) => b[1] - a[1]);
  const d = counts[0][0];
  if (counts[0][1] > 0) {
    const names = parseDelimited(firstLine, d).map(low);
    const find = cands => { for (const c of cands) { const i = names.indexOf(c); if (i >= 0) return i; } return -1; };
    const stmt = find(HEADER_COLS.stmt);
    if (stmt >= 0) {
      return {
        kind: "csv", header: true, delim: d, cols: names.length, stmt, event: find(HEADER_COLS.event), ts: find(HEADER_COLS.ts),
        m: HEADER_COLS.m.map(find),
        label: (d === "\t" ? "TAB" : d === "," ? "CSV" : "CSV (;)") + " · " + names.length + " columns",
      };
    }
  }
  return { kind: "tsv", header: false, label: "ParamLens TSV v1" };
}

/* Splits a Blob/File into records honoring quotes (CSV), without loading it whole.
 * Works on UTF-8/ANSI and UTF-16 code units. onRec(text, offset, lengthInBytes). */
async function forEachRecord(blob, encInfo, delim, onRec, onProgress, isCanceled) {
  const dec = new TextDecoder(encInfo.enc);
  const reader = blob.slice(encInfo.skip).stream().getReader();
  const u = encInfo.unit, le = encInfo.le;
  const at = u === 1 ? (b, j) => b[j] : le ? (b, j) => b[j] | (b[j + 1] << 8) : (b, j) => (b[j] << 8) | b[j + 1];
  const D = delim.charCodeAt(0);
  let carry = new Uint8Array(0), scanned = 0, st = 0, base = encInfo.skip, lastYield = Date.now();
  const emit = (buf, s, e) => {
    if (e - s >= u && at(buf, e - u) === 13) e -= u;
    onRec(dec.decode(buf.subarray(s, e)), base + s, e - s);
  };
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    let buf = value;
    if (carry.length) {
      buf = new Uint8Array(carry.length + value.length);
      buf.set(carry, 0);
      buf.set(value, carry.length);
    }
    let s = 0, j = scanned;
    for (; j + u <= buf.length; j += u) {
      const c = at(buf, j);
      if (st === 2) { if (c === 34) st = 3; continue; }
      if (st === 3 && c === 34) { st = 2; continue; }
      if (c === 10) { emit(buf, s, j); s = j + u; st = 0; continue; }
      if (c === D) { st = 0; continue; }
      st = st === 0 && c === 34 ? 2 : 1;
    }
    base += s;
    carry = buf.subarray(s);
    scanned = j - s;
    if (isCanceled && isCanceled()) { try { reader.cancel(); } catch (e) { /* ignore */ } return false; }
    if (Date.now() - lastYield > 60) {
      if (onProgress) onProgress(base - encInfo.skip, blob.size - encInfo.skip);
      await tick();
      lastYield = Date.now();
    }
  }
  if (carry.length) emit(carry, 0, carry.length - (carry.length % u));
  if (onProgress) onProgress(blob.size - encInfo.skip, blob.size - encInfo.skip);
  return true;
}

/* Reads the exported file and feeds an Engine.
 * Accepts "ParamLens TSV v1" (docs/export-format.md) or a CSV/TXT with a header row whose columns
 * are matched by name (needs a "statement" column).
 * opts: { normalizeLiterals, unitMs, eventName (default rpc_completed), like, onProgress(info, engine), isCanceled() }
 * Returns { engine, info, canceled }. */
async function analyzeFile(blob, opts) {
  opts = opts || {};
  const eng = new Engine(opts);
  const head = new Uint8Array(await blob.slice(0, 65536).arrayBuffer());
  const encInfo = detectEncoding(head);
  const headText = new TextDecoder(encInfo.enc).decode(head.subarray(encInfo.skip));
  const nl = headText.indexOf("\n");
  const fmt = detectFormat((nl >= 0 ? headText.slice(0, nl) : headText).replace(/\r$/, ""));
  encInfo.fmt = fmt;
  const evName = (opts.eventName || "rpc_completed").trim().toLowerCase();
  const like = opts.like && opts.like.trim() ? likeToRegExp(opts.like.trim()) : null;
  const info = {
    encoding: ENC_LABEL[encInfo.enc] || encInfo.enc, encId: encInfo.enc, format: fmt.label, size: blob.size,
    lines: 0, header: fmt.header, ignored: {}, filtered: 0, badFormat: 0, truncated: 0,
    tsMin: "", tsMax: "", bytesRead: 0,
  };

  // common handling of one event, whatever the file format
  const handle = (ev, ts, metrics, raw, stmt, off, len) => {
    ev = ev.trim();
    if (ev !== "" && ev.toLowerCase() !== evName) { info.ignored[ev] = (info.ignored[ev] || 0) + 1; return; }
    if (like && !like.test(stmt)) { info.filtered++; return; }
    if (raw.length === SSMS_CELL_LIMIT) {
      info.truncated++;
      eng.ingestFailure(FAIL_CUT, stmt);
      return;
    }
    ts = ts.trim();
    if (ts && ts !== "NULL") {
      if (!info.tsMin || ts < info.tsMin) info.tsMin = ts;
      if (!info.tsMax || ts > info.tsMax) info.tsMax = ts;
    }
    const m = [0, 0, 0, 0, 0, 0], has = [false, false, false, false, false, false];
    for (let k = 0; k < NM; k++) {
      const v = metrics[k] == null ? null : parseMetric(metrics[k]);
      if (v !== null) { m[k] = v; has[k] = true; }
    }
    eng.ingest(stmt, m, has, off, len, ts);
  };
  const blank = line => line === "" || (line.length < 3 && trimSpace(line) === "");
  let first = true;

  const onLine = (line, off, len) => {
    if (blank(line)) return;
    if (first) { first = false; if (fmt.header) return; }
    info.lines++;
    const f = splitExportLine(line);
    if (!f) {
      info.badFormat++;
      eng.ingestFailure(FAIL_FORMAT + (line.indexOf(",") >= 0 && line.indexOf("\t") < 0 ? FAIL_CSV : "") + ")", line);
      return;
    }
    let raw = f[EXPORT_COLS - 1];
    if (raw === "NULL") raw = "";
    handle(f[0], f[1], f.slice(2, 2 + NM), raw, unescapeStmt(raw), off, len);
  };

  const onRec = (text, off, len) => {
    if (blank(text)) return;
    if (first) { first = false; return; } // header row
    info.lines++;
    const f = parseDelimited(text, fmt.delim);
    if (f.length !== fmt.cols) {
      info.badFormat++;
      eng.ingestFailure(FAIL_COLS.replace("{0}", fmt.cols).replace("{1}", f.length), text);
      return;
    }
    let raw = f[fmt.stmt];
    if (raw === "NULL") raw = "";
    handle(fmt.event >= 0 ? f[fmt.event] : "", fmt.ts >= 0 ? f[fmt.ts] : "", fmt.m.map(i => (i >= 0 ? f[i] : null)), raw, raw, off, len);
  };

  const progress = done => { info.bytesRead = done; if (opts.onProgress) opts.onProgress(info, eng); };
  const ok = fmt.kind === "csv"
    ? await forEachRecord(blob, encInfo, fmt.delim, onRec, progress, opts.isCanceled)
    : await forEachLine(blob, encInfo, onLine, progress, opts.isCanceled);
  eng.finish();
  info.encInfo = encInfo;
  return { engine: eng, info, canceled: !ok };
}

// re-reads the statement of one execution straight from the file
async function readStatementAt(blob, encInfo, off, len) {
  return decodeStatement(new Uint8Array(await blob.slice(off, off + len).arrayBuffer()), encInfo);
}

// re-reads many statements, one read per window of nearby lines (refs: [{off, len}])
async function readStatementsAt(blob, encInfo, refs) {
  const WINDOW = 8 << 20;
  const out = new Array(refs.length);
  let i = 0;
  while (i < refs.length) {
    const start = refs[i].off;
    let end = start + refs[i].len, j = i;
    while (j + 1 < refs.length && refs[j + 1].off >= start && refs[j + 1].off + refs[j + 1].len - start <= WINDOW) {
      j++;
      end = Math.max(end, refs[j].off + refs[j].len);
    }
    const b = new Uint8Array(await blob.slice(start, end).arrayBuffer());
    for (let k = i; k <= j; k++) out[k] = decodeStatement(b.subarray(refs[k].off - start, refs[k].off - start + refs[k].len), encInfo);
    i = j + 1;
  }
  return out;
}

function decodeStatement(b, encInfo) {
  const line = new TextDecoder(encInfo.enc).decode(b);
  const fmt = encInfo.fmt || { kind: "tsv" };
  let raw;
  if (fmt.kind === "csv") {
    raw = parseDelimited(line, fmt.delim)[fmt.stmt] || "";
    return trimSpace(raw === "NULL" ? "" : raw);
  }
  const f = splitExportLine(line);
  if (!f) return "";
  raw = f[EXPORT_COLS - 1];
  return trimSpace(unescapeStmt(raw === "NULL" ? "" : raw));
}

return {
  Engine, analyzeFile, readStatementAt, readStatementsAt, testScript, parseRPC, normalizeSQL, detectPatterns, tokenize,
  sqlEqual, unescapeStmt, splitExportLine, parseDelimited, detectFormat, detectEncoding, likeToRegExp, EXPORT_COLS, parseTimestamp,
  _internal: { utf8Len, utf8Prefix, cmpStr, clipValue, classOf, normDate, parseNum },
};
});
