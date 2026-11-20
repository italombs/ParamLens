// Package web holds the user interface: a single HTML page (index.html) and the JavaScript
// engine (engine.js), embedded in the executable.
//
// The assembled page is used three ways: served by the .exe, written as the standalone
// ParamLens.html (-write-html) and, with the analysis JSON injected, as the exported report.
package web

import (
	"bytes"
	_ "embed"
)

//go:embed index.html
var indexSrc []byte

//go:embed engine.js
var engineJS []byte

// EngineMarker is replaced by the inline JS engine; StaticDataMarker by the report data.
const (
	EngineMarker     = "<!--ENGINE-->"
	StaticDataMarker = "<!--STATIC-DATA-->"
)

var page = assemble(indexSrc, engineJS)

// Page returns the interface with the JS engine inlined.
func Page() []byte { return page }

func assemble(index, engine []byte) []byte {
	inj := append([]byte("<script id=\"pl-engine\">\n"), engine...)
	inj = append(inj, []byte("</script>")...)
	return bytes.Replace(index, []byte(EngineMarker), inj, 1)
}

// WithStaticData returns the page with the analysis JSON embedded (the exported report).
func WithStaticData(data []byte) []byte {
	inj := append([]byte(`<script id="static-data" type="application/json">`), data...)
	inj = append(inj, []byte(`</script>`)...)
	return bytes.Replace(page, []byte(StaticDataMarker), inj, 1)
}
