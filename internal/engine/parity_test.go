package engine_test

// Gera os fixtures do teste de paridade Go × JS (tools/parity.js):
//
//	PARAMLENS_PARITY_DIR=$PWD/dist/parity go test -run TestParityFixtures ./internal/engine
//	node scripts/parity.js dist/parity
//
// O arquivo export.tsv segue o formato de exportação v1 (docs/exportacao.md), em
// UTF-16LE com CRLF, exatamente como o bcp -w grava. Os golden-*.json são a saída
// do motor Go para as mesmas linhas.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/italombs/ParamLens/internal/engine"
	"github.com/italombs/ParamLens/internal/source"
)

func escapeExport(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "\r", `\r`, "\n", `\n`, "\t", `\t`)
	return r.Replace(s)
}

// casos de borda além do modo demo
func parityExtraRows() []engine.Row {
	long := strings.Repeat("a", 198) + "çãõ" + strings.Repeat("x", 50)
	longFail := "SELECT '" + strings.Repeat("b", 2991) + "ção São João 😀" + strings.Repeat("c", 100) + "'"
	stmts := []string{
		// casos do parse_test.go
		`exec sp_executesql N'SELECT * FROM {X} [EN] WHERE ([EN].[A] IN (3, 1,2)) AND (@x = 0 OR [EN].[B] = @x) AND [EN].[C] = ''it''''s'' AND (@d = @nd OR [EN].[D] >= @d) AND [EN].[E] NOT IN (N''a'',N''b'')',N'@x bigint,@s nvarchar(50),@d datetime,@nd datetime,@m decimal(18,2)',@x=0,@s=N'a,b=c ''q''',@d='1900-01-01 00:00:00',@nd='1900-01-01 00:00:00.000',@m=-1.5`,
		`exec sp_executesql N'SELECT * FROM {X} [EN] WHERE ([EN].[A] IN (3, 1,2)) AND (@x = 0 OR [EN].[B] = @x) AND [EN].[C] = ''it''''s'' AND (@d = @nd OR [EN].[D] >= @d) AND [EN].[E] NOT IN (N''a'',N''b'')',N'@x bigint,@s nvarchar(50),@d datetime,@nd datetime,@m decimal(18,2)',@x=7,@s=NULL,@d='2026-01-01T00:00:00.000',@nd='1900-01-01',@m=2`,
		"declare @p1 int\nset @p1=NULL\nexec sp_prepexec @p1 output,N'@P0 int,@P1 nvarchar(10)',N'select 1 where a=@P0 and b=@P1',@P0=5,@P1=NULL\nselect @p1",
		"exec [dbo].[usp_x] @a=1, @b=N'x,y', 'pos', @c=default",
		"exec sp_executesql N'select @a+@b',N'@a int,@b int',1,2",
		// unicode, valores longos (corte em 200 bytes sem partir caractere)
		`exec sp_executesql N'SELECT * FROM T WHERE Nome = @n AND (@c = N'''' OR Cidade = @c)',N'@n nvarchar(400),@c nvarchar(100)',@n=N'São João ção 😀',@c=N''`,
		`exec sp_executesql N'SELECT * FROM T WHERE Nome = @n AND (@c = N'''' OR Cidade = @c)',N'@n nvarchar(400),@c nvarchar(100)',@n=N'` + long + `',@c=N'Recife'`,
		`exec sp_executesql N'SELECT * FROM T WHERE Nome = @n AND (@c = N'''' OR Cidade = @c)',N'@n nvarchar(400),@c nvarchar(5)',@n=N'x',@c=N'ÀÉÎõü'`,
		// TAB, CR/LF e barra invertida dentro do SQL (testa o escape do arquivo)
		"exec sp_executesql N'SELECT ''C:\\temp\\new\\x'' AS p,\t@x AS y\r\nWHERE (@x IS NULL OR a = @x) AND ISNULL(@y, 0) = 1 AND COALESCE(@z, b) = b',N'@x int,@y int,@z int',@x=NULL,@y=3,@z=4",
		"exec sp_executesql N'SELECT ''C:\\temp\\new\\x'' AS p,\t@x AS y\r\nWHERE (@x IS NULL OR a = @x) AND ISNULL(@y, 0) = 1 AND COALESCE(@z, b) = b',N'@x int,@y int,@z int',@x=9,@y=NULL,@z=NULL",
		"/* a /* b */ c */ exec sp_executesql N'select 1 -- x\n where a = @a',N'@a int',@a=2",
		// não interpretáveis
		"SELECT 1",
		longFail,
		"exec",
		// procedure com posicionais, sinal, default, output, exec @rc =
		"exec dbo.proc1 1, -2, N'x', default, @o = @v output",
		"exec @rc = dbo.proc1 @a=1",
		"EXECUTE [s].[p]] q] @x = 0x0AFF, @y = $1.50, @z = 1e3",
		// listas IN com negativos, duplicados e valores numericamente iguais
		"exec sp_executesql N'select * from t where a in (-1, 3, 2, 2, ''2'', 2.0) and b not in (N''b'', N''a'') and c in (1) and d in (1)',N'@z int',@z=1",
		"exec sp_executesql N'select * from t where a in (5) and b not in (N''z'') and c in (1, 2) and d in (3)',N'@z int',@z=1",
		// ß, identificadores com acento, comparação literal à esquerda, <>, IS NOT NULL
		"exec sp_executesql N'select straße, ação from t where (0 = @a OR x = @a) and (@b <> -1 OR y = @b) and (@c IS NOT NULL OR z = 1) and (@d = @nd OR w >= @d)',N'@a int,@b int,@c int,@d datetime,@nd datetime',@a=0,@b=-1,@c=NULL,@d='2026-01-01',@nd='1900-01-01'",
		"exec sp_executesql N'select straße, ação from t where (0 = @a OR x = @a) and (@b <> -1 OR y = @b) and (@c IS NOT NULL OR z = 1) and (@d = @nd OR w >= @d)',N'@a int,@b int,@c int,@d datetime,@nd datetime',@a=5,@b=3,@c=1,@d='1900-01-01T00:00:00',@nd='1900-01-01'",
		// strings que parecem números (nan, 1e5, $)
		"exec sp_executesql N'select * from t where (@s = 100000 OR x = @s) and (@t = 0 OR y = @t)',N'@s nvarchar(10),@t nvarchar(10)',@s=N'1e5',@t=N'nan'",
		"exec sp_executesql N'select * from t where (@s = 100000 OR x = @s) and (@t = 0 OR y = @t)',N'@s nvarchar(10),@t nvarchar(10)',@s=N' $3 ',@t=N'0'",
		// hard-coded values after non-ASCII text (byte × UTF-16 positions), BETWEEN, LIKE, CASE, types
		"exec sp_executesql N'select ''São João 😀'' as n from t where t.Nome = N''Conceição'' and t.S <> 6 and t.D between -5 and 2.5 and t.A in (N''ç'', N''b'') and t.L like ''a%'' and case when t.K = 1 then ''x'' end = N''x'' and 7 = t.Q and t.B = 3000000000 and t.X = 0x0A and @z = 0',N'@z int',@z=1",
		"exec sp_executesql N'select ''São João 😀'' as n from t where t.Nome = N''Ação'' and t.S <> 6 and t.D between -1 and 3 and t.A in (N''a,b'') and t.L like ''b%'' and case when t.K = 2 then ''x'' end = N''y'' and 8 = t.Q and t.B = 5 and t.X = 0x0B and @z = 0',N'@z int',@z=2",
		// parâmetro sem valor e argumento extra
		"exec sp_executesql N'select * from t where a = @a and b = @b',N'@a int,@b int',@a=1,@c=3",
	}
	var rows []engine.Row
	for i, s := range stmts {
		for rep := 0; rep < 1+i%3; rep++ {
			r := engine.Row{Stmt: s}
			r.M = engine.Metrics{int64(1000 * (i + 1)), int64(300*(i+1) + rep), int64(10*(i+1) + rep), int64(rep), 0, int64(i)}
			for k := range r.Has {
				r.Has[k] = true
			}
			if i == 4 && rep == 0 {
				r.Has = [engine.NMetrics]bool{} // métricas ausentes (NULL no SSMS)
				r.M = engine.Metrics{}
			}
			rows = append(rows, r)
		}
	}
	return rows
}

func TestParityFixtures(t *testing.T) {
	dir := os.Getenv("PARAMLENS_PARITY_DIR")
	if dir == "" {
		t.Skip("defina PARAMLENS_PARITY_DIR para gerar os fixtures do teste de paridade Go × JS")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// casos de borda primeiro: só os 30 primeiros exemplos de falha vão para o overview
	rows := parityExtraRows()
	n := 6000
	if s := os.Getenv("PARAMLENS_PARITY_ROWS"); s != "" {
		n, _ = strconv.Atoi(s)
	}
	if err := source.RunDemo(context.Background(), n, func(r engine.Row) { rows = append(rows, r) }); err != nil {
		t.Fatal(err)
	}

	// export.tsv: cabeçalho, eventos de outro tipo intercalados, NULL vindo do SSMS
	var sb strings.Builder
	sb.WriteString("event\ttimestamp\tduration\tcpu_time\tlogical_reads\tphysical_reads\twrites\trow_count\tstatement\r\n")
	ignored := 0
	for i, r := range rows {
		if i%97 == 0 {
			sb.WriteString("sp_statement_completed\t2026-11-20T10:00:00.000\t1\t1\t1\t0\t0\t0\tSELECT x FROM t WHERE a = @p\r\n")
			ignored++
		}
		sb.WriteString("rpc_completed\t2026-11-20T10:" + strconv.Itoa(10+i%50) + ":00.000")
		for k := 0; k < engine.NMetrics; k++ {
			sb.WriteByte('\t')
			if r.Has[k] {
				sb.WriteString(strconv.FormatInt(r.M[k], 10))
			} else if k%2 == 0 {
				sb.WriteString("NULL")
			}
		}
		sb.WriteByte('\t')
		sb.WriteString(escapeExport(r.Stmt))
		sb.WriteString("\r\n")
	}
	u := utf16.Encode([]rune(sb.String()))
	buf := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(buf[2*i:], c)
	}
	if err := os.WriteFile(filepath.Join(dir, "export.tsv"), buf, 0o644); err != nil {
		t.Fatal(err)
	}

	// export.csv: the same events as a SELECT * of an XE table saved as CSV by SSMS
	// (header, extra columns in another order, RFC 4180 quoting, raw line breaks, UTF-8 BOM)
	var cb bytes.Buffer
	cb.WriteString("\xEF\xBB\xBF")
	cw := csv.NewWriter(&cb) // UseCRLF would also rewrite line breaks inside the statements
	cw.Write([]string{"name", "timestamp", "timestamp (UTC)", "object_id", "duration", "cpu_time", "physical_reads", "logical_reads", "writes", "row_count", "statement", "sql_text", "data_stream", "id"})
	for i, r := range rows {
		if i%97 == 0 {
			cw.Write([]string{"sp_statement_completed", "", "2026-11-20 10:00:00.0000000 +00:00", "1", "1", "1", "0", "1", "0", "0", "SELECT x, y FROM t WHERE a = @p", "", "\x00", "0"})
		}
		met := make([]string, engine.NMetrics)
		for k := 0; k < engine.NMetrics; k++ {
			if r.Has[k] {
				met[k] = strconv.FormatInt(r.M[k], 10)
			} else {
				met[k] = "NULL"
			}
		}
		ts := "2026-11-20 10:" + strconv.Itoa(10+i%50) + ":00.0000000 +00:00"
		cw.Write([]string{"rpc_completed", ts, ts, "NULL", met[engine.MDur], met[engine.MCpu], met[engine.MPhys], met[engine.MReads], met[engine.MWrites], met[engine.MRows], r.Stmt, "", "\x00", strconv.Itoa(i)})
	}
	cw.Flush()
	if err := os.WriteFile(filepath.Join(dir, "export.csv"), cb.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	type variant struct {
		Name              string `json:"name"`
		NormalizeLiterals bool   `json:"normalizeLiterals"`
		UnitMs            bool   `json:"unitMs"`
	}
	variants := []variant{{"padrao", false, false}, {"literais-ms", true, true}}
	for _, v := range variants {
		eng, err := engine.NewEngine(engine.Options{NormalizeLiterals: v.NormalizeLiterals, UnitMs: v.UnitMs})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			eng.Ingest(r)
		}
		eng.Finish()
		ov := eng.Overview()
		details := map[int]*engine.FamilyDetail{}
		samples := map[int]map[string]string{}
		paramTables := map[int]map[string]*engine.ParamTable{}
		for _, f := range ov.Families {
			d, _ := eng.Detail(f.ID)
			details[f.ID] = d
			s, err := eng.Statement(int(f.Agg.Sample))
			if err != nil {
				t.Fatal(err)
			}
			samples[int(f.Agg.Sample)] = map[string]string{"statement": s, "script": engine.TestScript(s)}
			// combinação com todos os parâmetros (exercita o modo "classe")
			var all []int
			for i := range d.Params {
				all = append(all, i)
			}
			c, _ := eng.Combos(f.ID, all)
			d2 := *d
			d2.Combos = c
			details[f.ID] = &d2
			paramTables[f.ID] = map[string]*engine.ParamTable{}
			for _, mode := range []string{"execs", "combos"} {
				pt, err := eng.ParamTable(f.ID, mode)
				if err != nil {
					t.Fatal(err)
				}
				paramTables[f.ID][mode] = pt
			}
		}
		data, err := json.Marshal(map[string]any{
			"overview": ov, "details": details, "samples": samples, "ignored": ignored,
			"paramTables": paramTables, "distributions": eng.Distributions(),
		})
		eng.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "golden-"+v.Name+".json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vj, _ := json.Marshal(variants)
	if err := os.WriteFile(filepath.Join(dir, "variants.json"), vj, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("fixtures em %s: %d linhas", dir, len(rows))
}
