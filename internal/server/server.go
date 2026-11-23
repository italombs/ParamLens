// Package server is the local HTTP server of the .exe: it serves the interface, runs the
// analysis jobs (Go engine over SQL Server sources) and exports the static report.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/italombs/ParamLens/internal/engine"
	"github.com/italombs/ParamLens/internal/source"
	"github.com/italombs/ParamLens/web"
)

// Options configures Listen.
type Options struct {
	Version string
	Port    int    // 0 = automatic
	Bind    string // listen address; 127.0.0.1 unless inside a container
}

// Server is the listening local server.
type Server struct {
	URL     string // interface address, with the session token
	version string
	ln      net.Listener
	srv     *http.Server
}

type Job struct {
	eng    *engine.Engine
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

// Listen opens the local port and prepares the routes; Serve starts answering.
func Listen(o Options) (*Server, error) {
	if o.Bind == "" {
		o.Bind = "127.0.0.1"
	}
	b := make([]byte, 16)
	rand.Read(b)
	token = hex.EncodeToString(b)

	ln, err := net.Listen("tcp", net.JoinHostPort(o.Bind, strconv.Itoa(o.Port)))
	if err != nil {
		return nil, fmt.Errorf("could not open the local port: %w", err)
	}
	addr := ln.Addr().String()
	_, lport, _ := net.SplitHostPort(addr)

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
	api("/api/params", hParams)
	api("/api/dist", hDist)
	api("/api/insert", hInsert)
	api("/api/prefs", hPrefs)
	api("/api/export", hExport)
	api("/api/quit", hQuit)

	return &Server{
		URL:     "http://127.0.0.1:" + lport + "/?t=" + token,
		version: o.Version,
		ln:      ln,
		srv:     &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second},
	}, nil
}

// Banner prints the console header (console and browser modes).
func (s *Server) Banner() {
	fmt.Println("==============================================")
	fmt.Println(" ParamLens " + s.version + " — parameter profiling for rpc_completed")
	fmt.Println("==============================================")
	fmt.Println(" Interface: " + s.URL)
	fmt.Println(" Only accepts connections from this machine (127.0.0.1).")
	fmt.Println(" Close this window to quit.")
	fmt.Println()
}

// Serve answers requests until the process ends; Ctrl+C cleans up and quits.
func (s *Server) Serve() error {
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		Cleanup()
		os.Exit(0)
	}()
	if err := s.srv.Serve(s.ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Cleanup cancels the running analysis and removes its temporary file.
func Cleanup() {
	jobMu.Lock()
	defer jobMu.Unlock()
	if job != nil {
		if job.cancel != nil {
			job.cancel()
		}
		job.eng.Close()
	}
}

// OpenBrowser opens the interface in the default browser.
func OpenBrowser(u string) {
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
		fmt.Println(" Open in your browser:", u)
	}
}

// guard protege a API: Host precisa ser o endereço local (anti DNS-rebinding)
// e o token da sessão precisa vir no header (ou na querystring, para downloads).
func guard(addr string, h http.HandlerFunc) http.HandlerFunc {
	_, port, _ := net.SplitHostPort(addr)
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if host != "127.0.0.1:"+port && host != "localhost:"+port {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		t := r.Header.Get("X-Token")
		if t == "" {
			t = r.URL.Query().Get("t")
		}
		if t != token {
			http.Error(w, "invalid token — reopen the link shown in the console", http.StatusForbidden)
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
	w.Write(web.Page())
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
	var c source.ConnReq
	if err := readJSON(r, &c); err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := reqCtx(r, 30*time.Second)
	defer cancel()
	si, err := source.TestConn(ctx, c)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, si)
}

func hTables(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Conn source.ConnReq `json:"conn"`
		DB   string         `json:"db"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := reqCtx(r, 60*time.Second)
	defer cancel()
	t, err := source.ListTables(ctx, req.Conn, req.DB)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, t)
}

func hColumns(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Conn   source.ConnReq `json:"conn"`
		DB     string         `json:"db"`
		Schema string         `json:"schema"`
		Table  string         `json:"table"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := reqCtx(r, 30*time.Second)
	defer cancel()
	c, err := source.ListColumns(ctx, req.Conn, req.DB, req.Schema, req.Table)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, c)
}

type analyzeReq struct {
	Conn              source.ConnReq   `json:"conn"`
	Source            source.SourceReq `json:"source"`
	NormalizeLiterals bool             `json:"normalizeLiterals"`
	UnitMs            bool             `json:"unitMs"`
}

func hPreview(w http.ResponseWriter, r *http.Request) {
	var req analyzeReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	q, _, err := source.BuildQuery(req.Source)
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
		q, _, err := source.BuildQuery(req.Source)
		if err != nil {
			writeErr(w, err)
			return
		}
		query = q
	}
	eng, err := engine.NewEngine(engine.Options{NormalizeLiterals: req.NormalizeLiterals, UnitMs: req.UnitMs})
	if err != nil {
		writeErr(w, err)
		return
	}
	jobMu.Lock()
	if job != nil {
		if job.state == "running" {
			jobMu.Unlock()
			eng.Close()
			writeErr(w, fmt.Errorf("an analysis is already running"))
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
		j.label = "sample data"
	}
	job = j
	jobMu.Unlock()

	go func() {
		var err error
		if req.Source.Type == "demo" {
			err = source.RunDemo(ctx, req.Source.DemoRows, eng.Ingest)
		} else {
			err = source.RunSQL(ctx, req.Conn, req.Source, eng.Ingest)
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
		"elapsedMs": time.Since(e.Started()).Milliseconds(),
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

func curEngine() (*engine.Engine, string, string) {
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
		writeErr(w, fmt.Errorf("no finished analysis"))
		return
	}
	ov := e.Overview()
	writeJSON(w, map[string]any{"overview": ov, "label": label, "state": state})
}

func hFamily(w http.ResponseWriter, r *http.Request) {
	e, _, _ := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("no finished analysis"))
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	d, ok := e.Detail(id)
	if !ok {
		writeErr(w, fmt.Errorf("query not found"))
		return
	}
	writeJSON(w, d)
}

func hCombos(w http.ResponseWriter, r *http.Request) {
	e, _, _ := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("no finished analysis"))
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
		writeErr(w, fmt.Errorf("query not found"))
		return
	}
	writeJSON(w, c)
}

func hSample(w http.ResponseWriter, r *http.Request) {
	e, _, _ := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("no finished analysis"))
		return
	}
	ix, _ := strconv.Atoi(r.URL.Query().Get("exec"))
	s, err := e.Statement(ix)
	if err != nil {
		writeErr(w, fmt.Errorf("execution not found"))
		return
	}
	writeJSON(w, map[string]string{"statement": s, "script": engine.TestScript(s)})
}

func hExport(w http.ResponseWriter, r *http.Request) {
	e, label, _ := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("no finished analysis"))
		return
	}
	ov := e.Overview()
	details := map[int]*engine.FamilyDetail{}
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
	out := web.WithStaticData(data)
	name := "paramlens-" + time.Now().Format("20060102-1504") + ".html"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write(out)
}

func hQuit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"ok": "1"})
	go func() {
		time.Sleep(200 * time.Millisecond)
		Cleanup()
		os.Exit(0)
	}()
}

// hParams: exact parameter values of one query (one row per execution or per combination).
func hParams(w http.ResponseWriter, r *http.Request) {
	e, _, _ := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("no finished analysis"))
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	pt, err := e.ParamTable(id, r.URL.Query().Get("mode"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, pt)
}

// hDist: metric percentiles of every query (used to compare two captures).
func hDist(w http.ResponseWriter, r *http.Request) {
	e, _, _ := curEngine()
	if e == nil {
		writeErr(w, fmt.Errorf("no finished analysis"))
		return
	}
	writeJSON(w, e.Distributions())
}

// hInsert writes a parameter table into a SQL Server table. The rows come from the page, so
// this also works for captures analyzed by the JS engine (file mode).
func hInsert(w http.ResponseWriter, r *http.Request) {
	var req source.InsertReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := reqCtx(r, 30*time.Minute)
	defer cancel()
	n, err := source.InsertParams(ctx, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]int{"rows": n})
}

// hPrefs keeps interface preferences (the language) in the user profile: the port, and so the
// origin of the page and its localStorage, changes on every run.
func hPrefs(w http.ResponseWriter, r *http.Request) {
	dir, err := os.UserConfigDir()
	if err != nil {
		writeErr(w, err)
		return
	}
	file := filepath.Join(dir, "ParamLens", "prefs.json")
	var p struct {
		Lang string `json:"lang"`
	}
	if r.Method == http.MethodPost {
		if err := readJSON(r, &p); err != nil {
			writeErr(w, err)
			return
		}
		if p.Lang != "en" && p.Lang != "pt-BR" {
			writeErr(w, fmt.Errorf("unknown language"))
			return
		}
		data, _ := json.Marshal(p)
		if os.MkdirAll(filepath.Dir(file), 0o755) == nil {
			os.WriteFile(file, data, 0o644) // best effort: a preference
		}
		writeJSON(w, p)
		return
	}
	if data, err := os.ReadFile(file); err == nil {
		json.Unmarshal(data, &p)
	}
	writeJSON(w, p)
}
