package source

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteDemoTSV(t *testing.T) {
	var b bytes.Buffer
	if err := WriteDemoTSV(&b, 500); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	if len(lines) != 501 || !strings.HasPrefix(lines[0], "event\ttimestamp\t") {
		t.Fatalf("expected a header and 500 events, got %d lines", len(lines))
	}
	for i, l := range lines[1:] {
		f := strings.Split(l, "\t")
		if len(f) != 9 || f[0] != "rpc_completed" || !strings.HasPrefix(f[1], "2026-11-20T1") {
			t.Fatalf("line %d: %q", i+2, l)
		}
		if strings.ContainsAny(f[8], "\r\n") || !strings.HasPrefix(f[8], "exec ") {
			t.Fatalf("line %d: statement not escaped: %q", i+2, f[8])
		}
	}
}
