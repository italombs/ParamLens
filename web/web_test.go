package web

import (
	"bytes"
	"testing"
)

// The JS engine is inlined into index.html: it must not close the <script> or open an HTML
// comment, and each marker must appear exactly once.
func TestAssembledPage(t *testing.T) {
	if bytes.Contains(bytes.ToLower(engineJS), []byte("</script")) || bytes.Contains(engineJS, []byte("<!--")) {
		t.Fatal("web/engine.js contains </script or <!--")
	}
	for _, m := range []string{EngineMarker, StaticDataMarker} {
		if n := bytes.Count(indexSrc, []byte(m)); n != 1 {
			t.Fatalf("marker %s appears %d times", m, n)
		}
	}
	if !bytes.Contains(Page(), []byte(`<script id="pl-engine">`)) || bytes.Contains(Page(), []byte(EngineMarker)) {
		t.Fatal("JS engine was not injected")
	}
	rep := WithStaticData([]byte(`{"x":1}`))
	if !bytes.Contains(rep, []byte(`<script id="static-data" type="application/json">{"x":1}</script>`)) {
		t.Fatal("static data was not injected")
	}
}
