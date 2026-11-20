// Package source reads rpc_completed events from SQL Server (a table or a .xel file read by the
// server) and generates the synthetic demo data.
package source

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"sync"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/italombs/ParamLens/internal/engine"
)

type ConnReq struct {
	Mode       string `json:"mode"` // fields | string
	Server     string `json:"server"`
	Auth       string `json:"auth"` // windows | sql
	User       string `json:"user"`
	Password   string `json:"password"`
	Encrypt    string `json:"encrypt"` // false | true | strict
	Trust      bool   `json:"trust"`
	ConnString string `json:"connString"`
}

func (c ConnReq) build() (string, error) {
	if c.Mode == "string" {
		if strings.TrimSpace(c.ConnString) == "" {
			return "", errors.New("enter the connection string")
		}
		return c.ConnString, nil
	}
	srv := strings.TrimSpace(c.Server)
	if srv == "" {
		return "", errors.New("enter the server")
	}
	srv = strings.TrimPrefix(srv, "tcp:")
	host, port, inst := srv, "", ""
	if k := strings.LastIndex(host, ","); k > 0 {
		port = strings.TrimSpace(host[k+1:])
		host = strings.TrimSpace(host[:k])
	}
	if k := strings.Index(host, `\`); k > 0 {
		inst = host[k+1:]
		host = host[:k]
	}
	if host == "." || strings.EqualFold(host, "(local)") {
		host = "localhost"
	}
	u := &url.URL{Scheme: "sqlserver", Host: host}
	if port != "" {
		u.Host = host + ":" + port
	} else if inst != "" {
		u.Path = inst
	}
	if c.Auth == "sql" {
		u.User = url.UserPassword(c.User, c.Password)
	}
	enc := c.Encrypt
	if enc == "" {
		enc = "false"
	}
	q := url.Values{}
	q.Set("encrypt", enc)
	if c.Trust {
		q.Set("TrustServerCertificate", "true")
	}
	q.Set("app name", "ParamLens")
	q.Set("dial timeout", "15")
	q.Set("connection timeout", "0")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

var (
	poolMu sync.Mutex
	pools  = map[string]*sql.DB{}
)

func openDB(c ConnReq) (*sql.DB, error) {
	cs, err := c.build()
	if err != nil {
		return nil, err
	}
	poolMu.Lock()
	defer poolMu.Unlock()
	if db, ok := pools[cs]; ok {
		return db, nil
	}
	db, err := sql.Open("sqlserver", cs)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	pools[cs] = db
	return db, nil
}

func qn(s string) string {
	return "[" + strings.ReplaceAll(s, "]", "]]") + "]"
}

type ServerInfo struct {
	Version   string   `json:"version"`
	Server    string   `json:"server"`
	Login     string   `json:"login"`
	Databases []string `json:"databases"`
	Current   string   `json:"current"`
}

func TestConn(ctx context.Context, c ConnReq) (*ServerInfo, error) {
	db, err := openDB(c)
	if err != nil {
		return nil, err
	}
	si := &ServerInfo{}
	row := db.QueryRowContext(ctx, `SELECT CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)) + N' ' + CAST(SERVERPROPERTY('Edition') AS nvarchar(128)), @@SERVERNAME, SUSER_SNAME(), DB_NAME()`)
	var srvName sql.NullString
	if err := row.Scan(&si.Version, &srvName, &si.Login, &si.Current); err != nil {
		return nil, err
	}
	si.Server = srvName.String
	rows, err := db.QueryContext(ctx, `SELECT name FROM sys.databases WHERE state = 0 AND HAS_DBACCESS(name) = 1 ORDER BY CASE WHEN database_id <= 4 THEN 1 ELSE 0 END, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			si.Databases = append(si.Databases, n)
		}
	}
	return si, rows.Err()
}

type TableInfo struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Rows   int64  `json:"rows"`
	Likely bool   `json:"likely"`
}

func ListTables(ctx context.Context, c ConnReq, dbName string) ([]TableInfo, error) {
	db, err := openDB(c)
	if err != nil {
		return nil, err
	}
	d := qn(dbName)
	q := `SELECT s.name, t.name, ISNULL(SUM(CASE WHEN p.index_id IN (0,1) THEN p.rows END), 0),
       CASE WHEN EXISTS (SELECT 1 FROM ` + d + `.sys.columns c WHERE c.object_id = t.object_id AND c.name IN (N'statement', N'batch_text')) THEN 1 ELSE 0 END
FROM ` + d + `.sys.tables t
JOIN ` + d + `.sys.schemas s ON s.schema_id = t.schema_id
LEFT JOIN ` + d + `.sys.partitions p ON p.object_id = t.object_id
GROUP BY s.name, t.name, t.object_id
ORDER BY 4 DESC, s.name, t.name`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TableInfo
	for rows.Next() {
		var t TableInfo
		var likely int
		if err := rows.Scan(&t.Schema, &t.Name, &t.Rows, &likely); err != nil {
			return nil, err
		}
		t.Likely = likely == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func ListColumns(ctx context.Context, c ConnReq, dbName, schema, table string) ([]ColumnInfo, error) {
	db, err := openDB(c)
	if err != nil {
		return nil, err
	}
	d := qn(dbName)
	q := `SELECT c.name, ty.name FROM ` + d + `.sys.columns c
JOIN ` + d + `.sys.types ty ON ty.user_type_id = c.user_type_id
JOIN ` + d + `.sys.tables t ON t.object_id = c.object_id
JOIN ` + d + `.sys.schemas s ON s.schema_id = t.schema_id
WHERE s.name = @s AND t.name = @t ORDER BY c.column_id`
	rows, err := db.QueryContext(ctx, q, sql.Named("s", schema), sql.Named("t", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ColumnInfo
	for rows.Next() {
		var ci ColumnInfo
		if err := rows.Scan(&ci.Name, &ci.Type); err != nil {
			return nil, err
		}
		out = append(out, ci)
	}
	return out, rows.Err()
}

type SourceReq struct {
	Type       string            `json:"type"` // table | xel | demo
	DB         string            `json:"db"`
	Schema     string            `json:"schema"`
	Table      string            `json:"table"`
	Cols       map[string]string `json:"cols"` // stmt, name, duration, cpu, reads, physical, writes, rows
	EventValue string            `json:"eventValue"`
	Like       string            `json:"like"`
	Where      string            `json:"where"`
	Top        int               `json:"top"`
	XelPath    string            `json:"xelPath"`
	DemoRows   int               `json:"demoRows"`
}

// BuildQuery monta o SELECT da fonte. Devolve SQL + argumentos nomeados.
func BuildQuery(s SourceReq) (string, []any, error) {
	var args []any
	top := ""
	if s.Top > 0 {
		top = "TOP (@top) "
		args = append(args, sql.Named("top", s.Top))
	}
	switch s.Type {
	case "table":
		stmt := s.Cols["stmt"]
		if s.DB == "" || s.Table == "" || stmt == "" {
			return "", nil, errors.New("choose the database, the table and the statement column")
		}
		sel := []string{"CAST(" + qn(stmt) + " AS nvarchar(max))"}
		for _, m := range engine.MetricKeys {
			if c := s.Cols[m]; c != "" {
				sel = append(sel, "TRY_CAST("+qn(c)+" AS bigint)")
			} else {
				sel = append(sel, "CAST(NULL AS bigint)")
			}
		}
		q := "SELECT " + top + strings.Join(sel, ", ") + "\nFROM " + qn(s.DB) + "." + qn(s.Schema) + "." + qn(s.Table) + "\nWHERE " + qn(stmt) + " IS NOT NULL"
		if c := s.Cols["name"]; c != "" && strings.TrimSpace(s.EventValue) != "" {
			q += "\n  AND " + qn(c) + " = @ev"
			args = append(args, sql.Named("ev", strings.TrimSpace(s.EventValue)))
		}
		if strings.TrimSpace(s.Like) != "" {
			q += "\n  AND " + qn(stmt) + " LIKE @like"
			args = append(args, sql.Named("like", s.Like))
		}
		if strings.TrimSpace(s.Where) != "" {
			q += "\n  AND (" + s.Where + ")"
		}
		q += "\nOPTION (MAXDOP 1)"
		return q, args, nil
	case "xel":
		if strings.TrimSpace(s.XelPath) == "" {
			return "", nil, errors.New("enter the .xel path (on the server)")
		}
		ev := strings.TrimSpace(s.EventValue)
		if ev == "" {
			ev = "rpc_completed"
		}
		args = append(args, sql.Named("path", strings.TrimSpace(s.XelPath)), sql.Named("ev", ev))
		val := func(name, typ string) string {
			return `x.e.value('(event/data[@name="` + name + `"]/value)[1]', '` + typ + `')`
		}
		q := `SELECT ` + top + `v.statement, v.duration, v.cpu_time, v.logical_reads, v.physical_reads, v.writes, v.row_count
FROM (
  SELECT ` + val("statement", "nvarchar(max)") + ` AS statement,
         ` + val("duration", "bigint") + ` AS duration,
         ` + val("cpu_time", "bigint") + ` AS cpu_time,
         ` + val("logical_reads", "bigint") + ` AS logical_reads,
         ` + val("physical_reads", "bigint") + ` AS physical_reads,
         ` + val("writes", "bigint") + ` AS writes,
         ` + val("row_count", "bigint") + ` AS row_count
  FROM (SELECT CAST(event_data AS xml) AS e
        FROM sys.fn_xe_file_target_read_file(@path, NULL, NULL, NULL)
        WHERE object_name = @ev) AS x
) AS v
WHERE v.statement IS NOT NULL`
		if strings.TrimSpace(s.Like) != "" {
			q += "\n  AND v.statement LIKE @like"
			args = append(args, sql.Named("like", s.Like))
		}
		if strings.TrimSpace(s.Where) != "" {
			q += "\n  AND (" + s.Where + ")"
		}
		return q, args, nil
	}
	return "", nil, fmt.Errorf("unknown source: %s", s.Type)
}

func RunSQL(ctx context.Context, c ConnReq, s SourceReq, emit func(engine.Row)) error {
	db, err := openDB(c)
	if err != nil {
		return err
	}
	q, args, err := BuildQuery(s)
	if err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var stmt sql.NullString
	var m [engine.NMetrics]sql.NullInt64
	dst := []any{&stmt}
	for k := range m {
		dst = append(dst, &m[k])
	}
	for rows.Next() {
		if err := rows.Scan(dst...); err != nil {
			return err
		}
		r := engine.Row{Stmt: stmt.String}
		for k := range m {
			if m[k].Valid {
				r.M[k] = m[k].Int64
				r.Has[k] = true
			}
		}
		emit(r)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return rows.Err()
}

// ---------------- modo demo (dados sintéticos estilo OutSystems) ----------------

func RunDemo(ctx context.Context, n int, emit func(engine.Row)) error {
	if n <= 0 {
		n = 20000
	}
	rnd := rand.New(rand.NewSource(42))
	inbox := `SELECT count(1) FROM {MESSAGEIN} [ENMESSAGEIN] INNER JOIN {MESSAGETYPE} [ENMESSAGETYPE] ON ([ENMESSAGEIN].[MESSAGETYPEID] = [ENMESSAGETYPE].[ID])
LEFT JOIN {MESSAGEINREAD} [ENMESSAGEINREAD] ON ([ENMESSAGEINREAD].[MESSAGEINID] = [ENMESSAGEIN].[ID] AND [ENMESSAGEINREAD].[ORGANIZATIONUSERID] = @qploOrganizationUserId)
LEFT JOIN {PATIENT} [ENPATIENT] ON ([ENPATIENT].[ID] = [ENMESSAGEIN].[PATIENTID])
LEFT JOIN {TASK} [ENTASK] ON ([ENTASK].[MESSAGEINID] = [ENMESSAGEIN].[ID])
WHERE ([ENMESSAGEIN].[ORGANIZATIONID] = @qploOrganizationId)
 AND ([ENMESSAGEIN].[MESSAGETYPEID] IN (%s))
 AND (@qploDropdownAssigneeId = 0 OR [ENTASK].[ASSIGNEEID] = @qploDropdownAssigneeId)
 AND (@qpboIsMyGroup = 0 OR [ENTASK].[ASSIGNEEID] IN (%s))
 AND (@qpdaStartDate = @qpdaNullDate OR [ENMESSAGEIN].[CREATEDON] >= @qpdaStartDate)
 AND (@qpdaEndDate = @qpdaNullDate OR [ENMESSAGEIN].[CREATEDON] < @qpdaEndDate)
 AND (@qpboUnreadOnly = 0 OR [ENMESSAGEINREAD].[ID] IS NULL)
 AND (@qpstGPOfRegistration_AGB = N'' OR [ENPATIENT].[GPAGB] = @qpstGPOfRegistration_AGB)
 AND [ENMESSAGEIN].[MESSAGESTATEID] <> @qpinMessageStateIdAutomaticProcessing`
	decl := `@qploOrganizationUserId bigint,@qploOrganizationId bigint,@qploDropdownAssigneeId bigint,@qpboIsMyGroup bit,@qpdaStartDate datetime,@qpdaNullDate datetime,@qpdaEndDate datetime,@qpboUnreadOnly bit,@qpstGPOfRegistration_AGB nvarchar(%d),@qpinMessageStateIdAutomaticProcessing int`
	episodes := `SELECT TOP (@qpinMaxRecords) [ENDOC].[ID], [ENDOC].[TITLE] FROM {DOCUMENT} [ENDOC]
WHERE [ENDOC].[OWNERTYPEID] = @qpinOwnerTypeIdPatient AND [ENDOC].[PATIENTID] = @qploEncounter_PatientId
 AND (@qpboAllEpisodes = 1 OR [ENDOC].[EPISODEID] = @qploEpisodeID)
 AND [ENDOC].[IMPORTANCE] IN (@qpinImportance_Regular, @qpinImportance_High)
ORDER BY [ENDOC].[CREATEDON] DESC`
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var r engine.Row
		switch x := rnd.Intn(100); {
		case x < 70:
			types := []string{"1,2,3", "1,2,3,7", "1,2,3", "4", "1,2,3,7,9,12"}[rnd.Intn(5)]
			var groups []string
			for g := 0; g < 1+rnd.Intn(6); g++ {
				groups = append(groups, strconv.Itoa(100+rnd.Intn(40)))
			}
			assignee := 0
			if rnd.Intn(100) < 9 {
				assignee = 5000 + rnd.Intn(300)
			}
			myGroup := 0
			if rnd.Intn(100) < 30 {
				myGroup = 1
			}
			start, end := "1900-01-01 00:00:00", "1900-01-01 00:00:00"
			if rnd.Intn(100) < 15 {
				start = fmt.Sprintf("2026-%02d-%02d 00:00:00", 1+rnd.Intn(10), 1+rnd.Intn(28))
				end = "2026-11-19 00:00:00"
			}
			unread := 0
			if rnd.Intn(100) < 40 {
				unread = 1
			}
			agb := "N''"
			if rnd.Intn(100) < 3 {
				agb = fmt.Sprintf("N'%08d'", rnd.Intn(99999999))
			}
			sqlTxt := fmt.Sprintf(inbox, types, strings.Join(groups, ","))
			declLen := 4000
			if rnd.Intn(100) < 10 {
				declLen = 8
			}
			stmt := fmt.Sprintf("exec sp_executesql N'%s',N'%s',@qploOrganizationUserId=%d,@qploOrganizationId=%d,@qploDropdownAssigneeId=%d,@qpboIsMyGroup=%d,@qpdaStartDate='%s',@qpdaNullDate='1900-01-01 00:00:00',@qpdaEndDate='%s',@qpboUnreadOnly=%d,@qpstGPOfRegistration_AGB=%s,@qpinMessageStateIdAutomaticProcessing=6",
				strings.ReplaceAll(sqlTxt, "'", "''"), fmt.Sprintf(decl, declLen), 1000+rnd.Intn(800), []int{12, 12, 12, 15, 31}[rnd.Intn(5)], assignee, myGroup, start, end, unread, agb)
			reads := int64(800 + rnd.Intn(400))
			if assignee != 0 {
				reads = int64(120000 + rnd.Intn(60000))
			}
			if start != "1900-01-01 00:00:00" {
				reads += int64(20000 + rnd.Intn(5000))
			}
			r = engine.Row{Stmt: stmt}
			r.M = engine.Metrics{reads * 9, reads * 6, reads, int64(rnd.Intn(20)), 0, 1}
		case x < 92:
			all := 1
			ep := "NULL"
			if rnd.Intn(100) < 25 {
				all = 0
				ep = fmt.Sprintf("N'EP%06d'", rnd.Intn(999999))
			}
			stmt := fmt.Sprintf("exec sp_executesql N'%s',N'@qpinMaxRecords int,@qpinOwnerTypeIdPatient int,@qploEncounter_PatientId bigint,@qpboAllEpisodes bit,@qploEpisodeID nvarchar(4000),@qpinImportance_Regular int,@qpinImportance_High int',@qpinMaxRecords=50,@qpinOwnerTypeIdPatient=1,@qploEncounter_PatientId=%d,@qpboAllEpisodes=%d,@qploEpisodeID=%s,@qpinImportance_Regular=1,@qpinImportance_High=2",
				strings.ReplaceAll(episodes, "'", "''"), 100000+rnd.Intn(50000), all, ep)
			reads := int64(40 + rnd.Intn(30))
			r = engine.Row{Stmt: stmt}
			r.M = engine.Metrics{reads * 30, reads * 10, reads, 0, 0, int64(rnd.Intn(50))}
		case x < 99:
			stmt := fmt.Sprintf("exec [dbo].[usp_GetPatientTimeline] @PatientId=%d,@FromDate='2026-%02d-01',@IncludeArchived=%d", 100000+rnd.Intn(50000), 1+rnd.Intn(11), rnd.Intn(2))
			r = engine.Row{Stmt: stmt}
			r.M = engine.Metrics{int64(5000 + rnd.Intn(5000)), int64(2000 + rnd.Intn(2000)), int64(300 + rnd.Intn(300)), 0, 0, 10}
		default:
			r = engine.Row{Stmt: "exec sp_executesql @stmt"}
		}
		for k := range r.Has {
			r.Has[k] = true
		}
		emit(r)
	}
	return nil
}
