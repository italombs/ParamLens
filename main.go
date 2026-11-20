package main

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/index.html
var indexHTML []byte

var version = "1.0.0"

type Job struct {
	eng    *Engine
	state  string // running | done | error | canceled
	err    string
	cancel context.CancelFunc
	query  string
	label  string
}

var (
	jobMu sync.Mutex
	job   *Job
	token string
)

func main() {
	port := flag.Int("port", 0, "porta local (0 = automática)")
	noBrowser := flag.Bool("no-browser", false, "não abrir o navegador")
	bind := flag.String("bind", "127.0.0.1", "endereço de escuta (0.0.0.0 só dentro de container, publicando a porta em 127.0.0.1)")
	flag.Parse()

	b := make([]byte, 16)
	rand.Read(b)
	token = hex.EncodeToString(b)

	ln, err := net.Listen("tcp", net.JoinHostPort(*bind, strconv.Itoa(*port)))
	if err != nil {
		fmt.Println("Não foi possível abrir a porta local:", err)
		waitExit()
		return
	}
	addr := ln.Addr().String()
	_, lport, _ := net.SplitHostPort(addr)
	u := "http://127.0.0.1:" + lport + "/?t=" + token

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	api := func(path string, h http.HandlerFunc) { mux.HandleFunc(path, guard(addr, h)) }
	api("/api/test", hTest)
	api("/api/tables", hTables)
	api("/api/columns", hColumns)
	api("/api/preview", hPreview)
	api("/api/analyze", hAnalyze)
	api("/api/status", hStatus)
	api("/api/cancel", hCancel)
	api("/api/overview", hOverview)
	api("/api/family", hFamily)
	api("/api/combos", hCombos)
	api("/api/sample", hSample)
	api("/api/export", hExport)
	api("/api/quit", hQuit)

	fmt.Println("==============================================")
	fmt.Println(" ParamLens " + version + " — perfil de parâmetros do rpc_completed")
	fmt.Println("==============================================")
	fmt.Println(" Interface: " + u)
	fmt.Println(" Só aceita conexões desta máquina (127.0.0.1).")
	fmt.Println(" Feche esta janela para encerrar.")
	fmt.Println()

	if !*noBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(u)
		}()
	}
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		cleanup()
		os.Exit(0)
	}()
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Println("Erro:", err)
		waitExit()
	}
}

func waitExit() {
	fmt.Println("Pressione Enter para sair.")
	fmt.Scanln()
}

func cleanup() {
	jobMu.Lock()
	defer jobMu.Unlock()
	if job != nil {
		if job.cancel != nil {
			job.cancel()
		}
		job.eng.Close()
	}
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err != nil {
		fmt.Println(" Abra no navegador:", u)
	}
}

// guard protege a API: Host precisa ser o endereço local (anti DNS-rebinding)
// e o token da sessão precisa vir no header (ou na querystring, para downloads).
func guard(addr string, h http.HandlerFunc) http.HandlerFunc {
	_, port, _ := net.SplitHostPort(addr)
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if host != "127.0.0.1:"+port && host != "localhost:"+port {
			http.Error(w, "host inválido", http.StatusForbidden)
			return
		}
		t := r.Header.Get("X-Token")
		if t == "" {
			t = r.URL.Query().Get("t")
		}
		if t != token {
			http.Error(w, "token inválido — reabra pelo link mostrado no console", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(indexHTML)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func reqCtx(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func hTest(w http.ResponseWriter, r *http.Request) {
	var c ConnReq
	if err := readJSON(r, &c); err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := reqCtx(r, 30*time.Second)
	defer cancel()
	si, err := testConn(ctx, c)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, si)
}

func hTables(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Conn ConnReq `json:"conn"`
		DB   string  `json:"db"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := reqCtx(r, 60*time.Second)
	defer cancel()
	t, err := listTables(ctx, req.Conn, req.DB)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, t)
}

func hColumns(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Conn   ConnReq `json:"conn"`
		DB     string  `json:"db"`
		Schema string  `json:"schema"`
		Table  string  `json:"table"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := reqCtx(r, 30*time.Second)
	defer cancel()
	c, err := listColumns(ctx, req.Conn, req.DB, req.Schema, req.Table)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, c)
}

type analyzeReq struct {
	Conn              ConnReq   `json:"conn"`
	Source            SourceReq `json:"source"`
	NormalizeLiterals bool      `json:"normalizeLiterals"`
	UnitMs            bool      `json:"unitMs"`
}

func hPreview(w http.ResponseWriter, r *http.Request) {
	var req analyzeReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	q, _, err := BuildQuery(req.Source)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"query": q})
}

func hAnalyze(w http.ResponseWriter, r *http.Request) {
	var req analyzeReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	query := ""
	if req.Source.Type != "demo" {
		q, _, err := BuildQuery(req.Source)
		if err != nil {
			writeErr(w, err)
			return
		}
		query = q
	}
	eng, err := NewEngine(Options{NormalizeLiterals: req.NormalizeLiterals, UnitMs: req.UnitMs})
	if err != nil {
		writeErr(w, err)
		return
	}
	jobMu.Lock()
	if job != nil {
		if job.state == "running" {
			jobMu.Unlock()
			eng.Close()
			writeErr(w, fmt.Errorf("já existe uma análise em andamento"))
			return
		}
		job.eng.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{eng: eng, state: "running", cancel: cancel, query: query}
	switch req.Source.Type {
	case "table":
		j.label = req.Source.DB + "." + req.Source.Schema + "." + req.Source.Table
	case "xel":
		j.label = req.Source.XelPath
	default:
		j.label = "dados de exemplo"
	}
	job = j
	jobMu.Unlock()

	go func() {
		var err error
		if req.Source.Type == "demo" {
			err = runDemo(ctx, req.Source.DemoRows, eng.Ingest)
		} else {
			err = runSQLSource(ctx, req.Conn, req.Source, eng.Ingest)
		}
		eng.Finish()
		jobMu.Lock()
		defer jobMu.Unlock()
		switch {
		case ctx.Err() != nil && err != nil:
			j.state = "canceled"
		case err != nil:
			j.state = "error"
			j.err = err.Error()
		default:
			j.state = "done"
		}
	}()
	writeJSON(w, map[string]string{"state": "running", "query": query})
}

func hStatus(w http.ResponseWriter, r *http.Request) {
	jobMu.Lock()
	defer jobMu.Unlock()
	if job == nil {
		writeJSON(w, map[string]any{"state": "idle"})
		return
	}
	e := job.eng
	writeJSON(w, map[string]any{
		"state":     job.state,
		"error":     job.err,
		"read":      e.Read.Load(),
		"parsed":    e.Parsed.Load(),
		"failed":    e.Failed.Load(),
		"elapsedMs": time.Since(e.started).Milliseconds(),
		"label":     job.label,
		"query":     job.query,
	})
}

func hCancel(w http.ResponseWriter, r *http.Request) {
	jobMu.Lock()
	defer jobMu.Unlock()
	if job != nil && job.cancel != nil {
		job.cancel()
	}
	writeJSON(w, map[string]string{"ok": "1"})
}

func curEngine() (*Engine, string, string) {
	jobMu.Lock()
	defer jobMu.Unlock()
	if job == nil {
		return nil, "", ""
	}
	if job.state == "running" {
		return nil, "", ""
	}
	return job.eng, job.label, job.state
}

func hOverview(w http.ResponseWriter, r *http.Request) {
	e, label, state := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("nenhuma análise concluída"))
		return
	}
	ov := e.Overview()
	writeJSON(w, map[string]any{"overview": ov, "label": label, "state": state})
}

func hFamily(w http.ResponseWriter, r *http.Request) {
	e, _, _ := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("nenhuma análise concluída"))
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	d, ok := e.Detail(id)
	if !ok {
		writeErr(w, fmt.Errorf("família não encontrada"))
		return
	}
	writeJSON(w, d)
}

func hCombos(w http.ResponseWriter, r *http.Request) {
	e, _, _ := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("nenhuma análise concluída"))
		return
	}
	var req struct {
		ID      int   `json:"id"`
		Indexes []int `json:"indexes"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	c, ok := e.Combos(req.ID, req.Indexes)
	if !ok {
		writeErr(w, fmt.Errorf("família não encontrada"))
		return
	}
	writeJSON(w, c)
}

func hSample(w http.ResponseWriter, r *http.Request) {
	e, _, _ := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("nenhuma análise concluída"))
		return
	}
	ix, _ := strconv.Atoi(r.URL.Query().Get("exec"))
	s, err := e.Statement(ix)
	if err != nil {
		writeErr(w, fmt.Errorf("execução não encontrada"))
		return
	}
	writeJSON(w, map[string]string{"statement": s, "script": TestScript(s)})
}

func hExport(w http.ResponseWriter, r *http.Request) {
	e, label, _ := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("nenhuma análise concluída"))
		return
	}
	ov := e.Overview()
	details := map[int]*FamilyDetail{}
	for _, f := range ov.Families {
		if d, ok := e.Detail(f.ID); ok {
			details[f.ID] = d
		}
	}
	data, err := json.Marshal(map[string]any{
		"overview": ov, "details": details, "label": label,
		"generated": time.Now().Format("2006-01-02 15:04"),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	marker := []byte("<!--STATIC-DATA-->")
	inj := append([]byte(`<script id="static-data" type="application/json">`), data...)
	inj = append(inj, []byte(`</script>`)...)
	out := bytes.Replace(indexHTML, marker, inj, 1)
	name := "paramlens-" + time.Now().Format("20060102-1504") + ".html"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write(out)
}

func hQuit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"ok": "1"})
	go func() {
		time.Sleep(200 * time.Millisecond)
		cleanup()
		os.Exit(0)
	}()
}

var _ = strings.TrimSpace
