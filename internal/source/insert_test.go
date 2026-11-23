package source

import (
	"bytes"
	"testing"
)

func TestValidType(t *testing.T) {
	ok := []string{"int", "nvarchar(50)", "nvarchar(max)", "decimal(18,2)", "decimal(18, 2)", "datetime2(7)", "varbinary(10)", "double precision", "  bigint "}
	bad := []string{"", "int); DROP TABLE t; --", "dbo.mytype", "nvarchar(50) NOT NULL", "nvarchar(99999)", "int -- x", "[int]"}
	for _, s := range ok {
		if !ValidType(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range bad {
		if ValidType(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
}

func TestCellValue(t *testing.T) {
	if v, _ := cellValue(nil); v != nil {
		t.Errorf("nil -> %v", v)
	}
	if v, _ := cellValue("abc"); v != "abc" {
		t.Errorf("string -> %v", v)
	}
	if v, _ := cellValue(12.5); v != "12.5" {
		t.Errorf("number -> %v", v)
	}
	if v, _ := cellValue(map[string]any{"b": "0A0b"}); !bytes.Equal(v.([]byte), []byte{0x0a, 0x0b}) {
		t.Errorf("binary -> %v", v)
	}
	if _, err := cellValue(map[string]any{"b": "zz"}); err == nil {
		t.Error("invalid hex accepted")
	}
}
