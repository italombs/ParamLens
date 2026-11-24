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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	Cols       map[string]string `json:"cols"` // stmt, name, ts, duration, cpu, reads, physical, writes, rows
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
		if c := s.Cols["ts"]; c != "" {
			sel = append(sel, "CONVERT(nvarchar(40), "+qn(c)+", 121)")
		} else {
			sel = append(sel, "CAST(NULL AS nvarchar(40))")
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
		q := `SELECT ` + top + `v.statement, v.duration, v.cpu_time, v.logical_reads, v.physical_reads, v.writes, v.row_count, v.ts
FROM (
  SELECT ` + val("statement", "nvarchar(max)") + ` AS statement,
         ` + val("duration", "bigint") + ` AS duration,
         ` + val("cpu_time", "bigint") + ` AS cpu_time,
         ` + val("logical_reads", "bigint") + ` AS logical_reads,
         ` + val("physical_reads", "bigint") + ` AS physical_reads,
         ` + val("writes", "bigint") + ` AS writes,
         ` + val("row_count", "bigint") + ` AS row_count,
         x.e.value('(event/@timestamp)[1]', 'nvarchar(40)') AS ts
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
	var ts sql.NullString
	dst := []any{&stmt}
	for k := range m {
		dst = append(dst, &m[k])
	}
	dst = append(dst, &ts)
	for rows.Next() {
		if err := rows.Scan(dst...); err != nil {
			return err
		}
		r := engine.Row{Stmt: stmt.String, TS: ts.String}
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

// ---------------- demo mode (synthetic, fictitious OutSystems-style data) ----------------

func RunDemo(ctx context.Context, n int, emit func(engine.Row)) error {
	if n <= 0 {
		n = 20000
	}
	rnd := rand.New(rand.NewSource(42))
	ordersSQL := `SELECT count(1) FROM {SALESORDER} [ENSALESORDER] INNER JOIN {ORDERTYPE} [ENORDERTYPE] ON ([ENSALESORDER].[ORDERTYPEID] = [ENORDERTYPE].[ID])
LEFT JOIN {ORDERVIEW} [ENORDERVIEW] ON ([ENORDERVIEW].[ORDERID] = [ENSALESORDER].[ID] AND [ENORDERVIEW].[STOREUSERID] = @qploStoreUserId)
LEFT JOIN {CUSTOMER} [ENCUSTOMER] ON ([ENCUSTOMER].[ID] = [ENSALESORDER].[CUSTOMERID])
LEFT JOIN {SHIPMENT} [ENSHIPMENT] ON ([ENSHIPMENT].[ORDERID] = [ENSALESORDER].[ID])
WHERE ([ENSALESORDER].[STOREID] = @qploStoreId)
 AND ([ENSALESORDER].[ORDERTYPEID] IN (%s))
 AND (@qploDropdownCarrierId = 0 OR [ENSHIPMENT].[CARRIERID] = @qploDropdownCarrierId)
 AND (@qpboIsMyRegion = 0 OR [ENSHIPMENT].[CARRIERID] IN (%s))
 AND (@qpdaStartDate = @qpdaNullDate OR [ENSALESORDER].[CREATEDON] >= @qpdaStartDate)
 AND (@qpdaEndDate = @qpdaNullDate OR [ENSALESORDER].[CREATEDON] < @qpdaEndDate)
 AND (@qpboUnreadOnly = 0 OR [ENORDERVIEW].[ID] IS NULL)
 AND (@qpstCouponCode = N'' OR [ENCUSTOMER].[COUPONCODE] = @qpstCouponCode)
 AND [ENSALESORDER].[ORDERSTATEID] <> @qpinOrderStateIdArchived`
	decl := `@qploStoreUserId bigint,@qploStoreId bigint,@qploDropdownCarrierId bigint,@qpboIsMyRegion bit,@qpdaStartDate datetime,@qpdaNullDate datetime,@qpdaEndDate datetime,@qpboUnreadOnly bit,@qpstCouponCode nvarchar(%d),@qpinOrderStateIdArchived int`
	invoicesSQL := `SELECT TOP (@qpinMaxRecords) [ENINVOICE].[ID], [ENINVOICE].[TITLE] FROM {INVOICE} [ENINVOICE]
WHERE [ENINVOICE].[OWNERTYPEID] = @qpinOwnerTypeIdCustomer AND [ENINVOICE].[CUSTOMERID] = @qploCustomerId
 AND (@qpboAllProjects = 1 OR [ENINVOICE].[PROJECTID] = @qploProjectId)
 AND [ENINVOICE].[PRIORITY] IN (@qpinPriority_Normal, @qpinPriority_High)
ORDER BY [ENINVOICE].[CREATEDON] DESC`
	times := demoTimes(n)
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
			carrier := 0
			if rnd.Intn(100) < 9 {
				carrier = 5000 + rnd.Intn(300)
			}
			myRegion := 0
			if rnd.Intn(100) < 30 {
				myRegion = 1
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
			coupon := "N''"
			if rnd.Intn(100) < 3 {
				coupon = fmt.Sprintf("N'%08d'", rnd.Intn(99999999))
			}
			sqlTxt := fmt.Sprintf(ordersSQL, types, strings.Join(groups, ","))
			declLen := 4000
			if rnd.Intn(100) < 10 {
				declLen = 8
			}
			stmt := fmt.Sprintf("exec sp_executesql N'%s',N'%s',@qploStoreUserId=%d,@qploStoreId=%d,@qploDropdownCarrierId=%d,@qpboIsMyRegion=%d,@qpdaStartDate='%s',@qpdaNullDate='1900-01-01 00:00:00',@qpdaEndDate='%s',@qpboUnreadOnly=%d,@qpstCouponCode=%s,@qpinOrderStateIdArchived=6",
				strings.ReplaceAll(sqlTxt, "'", "''"), fmt.Sprintf(decl, declLen), 1000+rnd.Intn(800), []int{12, 12, 12, 15, 31}[rnd.Intn(5)], carrier, myRegion, start, end, unread, coupon)
			reads := int64(800 + rnd.Intn(400))
			if carrier != 0 {
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
				ep = fmt.Sprintf("N'PR%06d'", rnd.Intn(999999))
			}
			stmt := fmt.Sprintf("exec sp_executesql N'%s',N'@qpinMaxRecords int,@qpinOwnerTypeIdCustomer int,@qploCustomerId bigint,@qpboAllProjects bit,@qploProjectId nvarchar(4000),@qpinPriority_Normal int,@qpinPriority_High int',@qpinMaxRecords=50,@qpinOwnerTypeIdCustomer=1,@qploCustomerId=%d,@qpboAllProjects=%d,@qploProjectId=%s,@qpinPriority_Normal=1,@qpinPriority_High=2",
				strings.ReplaceAll(invoicesSQL, "'", "''"), 100000+rnd.Intn(50000), all, ep)
			reads := int64(40 + rnd.Intn(30))
			r = engine.Row{Stmt: stmt}
			r.M = engine.Metrics{reads * 30, reads * 10, reads, 0, 0, int64(rnd.Intn(50))}
		case x < 99:
			stmt := fmt.Sprintf("exec [dbo].[usp_GetCustomerHistory] @CustomerId=%d,@FromDate='2026-%02d-01',@IncludeArchived=%d", 100000+rnd.Intn(50000), 1+rnd.Intn(11), rnd.Intn(2))
			r = engine.Row{Stmt: stmt}
			r.M = engine.Metrics{int64(5000 + rnd.Intn(5000)), int64(2000 + rnd.Intn(2000)), int64(300 + rnd.Intn(300)), 0, 0, 10}
		default:
			r = engine.Row{Stmt: "exec sp_executesql @stmt"}
		}
		for k := range r.Has {
			r.Has[k] = true
		}
		r.TS = times[i]
		emit(r)
	}
	return nil
}

// demoTimes spreads n timestamps over three working days, busier in the morning and afternoon, in
// increasing order (a separate generator, so the rest of the sample data does not change).
func demoTimes(n int) []string {
	rnd := rand.New(rand.NewSource(7))
	// per UTC hour: working hours of UTC-3 (09:00-18:00 there)
	weights := []int{3, 2, 2, 1, 1, 1, 1, 1, 1, 2, 4, 9, 14, 15, 12, 6, 8, 13, 14, 11, 7, 4, 3, 2}
	total := 0
	for _, w := range weights {
		total += w
	}
	t0 := time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC) // midnight in UTC-3
	ts := make([]time.Time, n)
	for i := range ts {
		h, x := 0, rnd.Intn(total)
		for x >= weights[h] {
			x -= weights[h]
			h++
		}
		h = (h + 21) % 24 // hours after t0
		ts[i] = t0.Add(time.Duration(rnd.Intn(3))*24*time.Hour + time.Duration(h)*time.Hour + time.Duration(rnd.Int63n(int64(time.Hour/time.Millisecond)))*time.Millisecond)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	out := make([]string, n)
	for i, t := range ts {
		out[i] = t.Format("2006-01-02T15:04:05.000")
	}
	return out
}
