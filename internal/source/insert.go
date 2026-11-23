package source

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// InsertCol is a column of the destination table. Type is a SQL Server type name such as
// int, nvarchar(50), decimal(18,2) or nvarchar(max).
type InsertCol struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// InsertReq writes a parameter table (one row per execution or per combination) into a table.
// Each cell is null (NULL), a string (sent as nvarchar, converted by SQL Server to the column
// type) or {"b": "<hex>"} for binary values.
type InsertReq struct {
	Conn    ConnReq     `json:"conn"`
	DB      string      `json:"db"`
	Schema  string      `json:"schema"`
	Table   string      `json:"table"`
	Replace bool        `json:"replace"` // drop and recreate the table if it exists
	Columns []InsertCol `json:"columns"`
	Rows    [][]any     `json:"rows"`
}

var reType = regexp.MustCompile(`^(?i)[a-z][a-z0-9_]*( [a-z][a-z0-9_]*)?( ?\( ?(max|[0-9]{1,4})( ?, ?[0-9]{1,2})? ?\))?$`)

// ValidType reports whether t is a plain type name that can be written into a CREATE TABLE.
func ValidType(t string) bool { return reType.MatchString(strings.TrimSpace(t)) }

func cellValue(c any) (any, error) {
	switch v := c.(type) {
	case nil:
		return nil, nil
	case string:
		return v, nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		if v {
			return "1", nil
		}
		return "0", nil
	case map[string]any:
		if s, ok := v["b"].(string); ok {
			return hex.DecodeString(s)
		}
	}
	return nil, fmt.Errorf("unsupported value %v", c)
}

// InsertParams creates the table if needed and inserts the rows in batches inside one transaction.
func InsertParams(ctx context.Context, r InsertReq) (int, error) {
	if strings.TrimSpace(r.DB) == "" || strings.TrimSpace(r.Table) == "" {
		return 0, errors.New("choose the database and the table")
	}
	if len(r.Columns) == 0 {
		return 0, errors.New("no columns to insert")
	}
	schema := strings.TrimSpace(r.Schema)
	if schema == "" {
		schema = "dbo"
	}
	full := qn(r.DB) + "." + qn(schema) + "." + qn(strings.TrimSpace(r.Table))
	defs := make([]string, len(r.Columns))
	names := make([]string, len(r.Columns))
	for i, c := range r.Columns {
		if !ValidType(c.Type) {
			return 0, fmt.Errorf("invalid type for column %s: %s", c.Name, c.Type)
		}
		names[i] = qn(c.Name)
		defs[i] = names[i] + " " + strings.TrimSpace(c.Type) + " NULL"
	}
	for i, row := range r.Rows {
		if len(row) != len(r.Columns) {
			return 0, fmt.Errorf("row %d has %d values, expected %d", i+1, len(row), len(r.Columns))
		}
	}
	db, err := openDB(r.Conn)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// literal dates such as '2026-01-12' must not depend on the login language (dmy in pt-BR)
	if _, err := tx.ExecContext(ctx, "SET DATEFORMAT ymd"); err != nil {
		return 0, err
	}
	obj := "N'" + strings.ReplaceAll(full, "'", "''") + "'"
	ddl := ""
	if r.Replace {
		ddl = "IF OBJECT_ID(" + obj + ", N'U') IS NOT NULL DROP TABLE " + full + ";\n"
	}
	ddl += "IF OBJECT_ID(" + obj + ", N'U') IS NULL CREATE TABLE " + full + " (\n  " + strings.Join(defs, ",\n  ") + "\n);"
	if _, err := tx.ExecContext(ctx, ddl); err != nil {
		return 0, err
	}
	per := 2000 / len(r.Columns)
	if per > 1000 {
		per = 1000
	}
	if per < 1 {
		return 0, errors.New("too many columns")
	}
	head := "INSERT INTO " + full + " (" + strings.Join(names, ", ") + ") VALUES "
	done := 0
	for start := 0; start < len(r.Rows); start += per {
		end := start + per
		if end > len(r.Rows) {
			end = len(r.Rows)
		}
		var sb strings.Builder
		sb.WriteString(head)
		args := make([]any, 0, (end-start)*len(r.Columns))
		for i := start; i < end; i++ {
			if i > start {
				sb.WriteString(",")
			}
			sb.WriteString("(")
			for k, c := range r.Rows[i] {
				if k > 0 {
					sb.WriteString(",")
				}
				v, err := cellValue(c)
				if err != nil {
					return 0, fmt.Errorf("row %d: %w", i+1, err)
				}
				if v == nil {
					// a NULL parameter is typed nvarchar, which varbinary columns do not accept
					sb.WriteString("NULL")
					continue
				}
				args = append(args, v)
				sb.WriteString("@p" + strconv.Itoa(len(args)))
			}
			sb.WriteString(")")
		}
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return done, fmt.Errorf("rows %d–%d: %w", start+1, end, err)
		}
		done = end
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return done, nil
}
