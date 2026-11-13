// Package database browses the database running in a container: its
// databases, tables and rows, and runs queries. It recognizes SQL Server,
// PostgreSQL and MySQL/MariaDB by the container's image and signs in with the
// credentials the container was started with, so nothing has to be set up.
package database

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb"
)

type Engine string

const (
	SQLServer Engine = "sqlserver"
	Postgres  Engine = "postgres"
	MySQL     Engine = "mysql"
)

// Target is how to reach one container's database.
type Target struct {
	Engine   Engine `json:"engine"`
	Host     string `json:"-"`
	Port     int    `json:"-"`
	User     string `json:"user"`
	Password string `json:"-"`
	Default  string `json:"default"` // the database to open first
	// NeedsLogin is set when the container's settings hold no password; the page
	// then asks for one
	NeedsLogin bool `json:"needsLogin"`
}

// Detect recognizes a database by the container's image and finds its login in
// the container's environment. env looks a variable up.
func Detect(image, ip string, env func(string) (string, bool)) (Target, bool) {
	img := strings.ToLower(image)
	get := func(names ...string) string {
		for _, n := range names {
			if v, ok := env(n); ok && v != "" {
				return v
			}
		}
		return ""
	}
	var t Target
	switch {
	case strings.Contains(img, "mssql") || strings.Contains(img, "sqlserver") || strings.Contains(img, "azure-sql-edge"):
		t = Target{Engine: SQLServer, Port: 1433, User: "sa", Password: get("MSSQL_SA_PASSWORD", "SA_PASSWORD"), Default: "master"}
	case strings.Contains(img, "postgres") || strings.Contains(img, "postgis") || strings.Contains(img, "timescale"):
		user := get("POSTGRES_USER")
		if user == "" {
			user = "postgres"
		}
		db := get("POSTGRES_DB")
		if db == "" {
			db = user
		}
		t = Target{Engine: Postgres, Port: 5432, User: user, Password: get("POSTGRES_PASSWORD"), Default: db}
	case strings.Contains(img, "mysql") || strings.Contains(img, "mariadb") || strings.Contains(img, "percona"):
		t = Target{Engine: MySQL, Port: 3306, User: "root", Password: get("MYSQL_ROOT_PASSWORD", "MARIADB_ROOT_PASSWORD"), Default: get("MYSQL_DATABASE", "MARIADB_DATABASE")}
		if t.Password == "" && get("MYSQL_USER", "MARIADB_USER") != "" {
			t.User, t.Password = get("MYSQL_USER", "MARIADB_USER"), get("MYSQL_PASSWORD", "MARIADB_PASSWORD")
		}
	default:
		return Target{}, false
	}
	t.Host = ip
	t.NeedsLogin = t.Password == ""
	return t, true
}

// open connects to one database of the target.
func (t Target) open(db string) (*sql.DB, error) {
	if t.Host == "" {
		return nil, errors.New("the container has no network address; is it running?")
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	var driver, dsn string
	switch t.Engine {
	case SQLServer:
		q := url.Values{"encrypt": {"true"}, "TrustServerCertificate": {"true"}, "app name": {"LinCos"}, "dial timeout": {"5"}}
		if db != "" {
			q.Set("database", db)
		}
		u := url.URL{Scheme: "sqlserver", User: url.UserPassword(t.User, t.Password), Host: addr, RawQuery: q.Encode()}
		driver, dsn = "sqlserver", u.String()
	case Postgres:
		if db == "" {
			db = t.Default
		}
		u := url.URL{Scheme: "postgres", User: url.UserPassword(t.User, t.Password), Host: addr, Path: "/" + db,
			RawQuery: url.Values{"sslmode": {"disable"}, "connect_timeout": {"5"}, "application_name": {"LinCos"}}.Encode()}
		driver, dsn = "postgres", u.String()
	case MySQL:
		c := mysql.NewConfig()
		c.User, c.Passwd, c.Net, c.Addr, c.DBName, c.Timeout = t.User, t.Password, "tcp", addr, db, 5*time.Second
		driver, dsn = "mysql", c.FormatDSN()
	default:
		return nil, fmt.Errorf("unknown database %q", t.Engine)
	}
	d, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	return d, nil
}

// Databases lists the databases, the ones the user made first.
func (t Target) Databases(ctx context.Context) ([]string, error) {
	q := map[Engine]string{
		SQLServer: "SELECT name FROM sys.databases WHERE state = 0 ORDER BY CASE WHEN database_id <= 4 THEN 1 ELSE 0 END, name",
		Postgres:  "SELECT datname FROM pg_database WHERE NOT datistemplate ORDER BY CASE WHEN datname = 'postgres' THEN 1 ELSE 0 END, datname",
		MySQL:     "SELECT schema_name FROM information_schema.schemata ORDER BY CASE WHEN schema_name IN ('mysql','information_schema','performance_schema','sys') THEN 1 ELSE 0 END, schema_name",
	}[t.Engine]
	res, err := t.run(ctx, "", q, 10000, nil)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range res.Rows {
		if s, ok := r[0].(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// Table is one table and about how many rows it holds (from the database's
// statistics, so it costs nothing to ask).
type Table struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Rows   int64  `json:"rows"`
}

func (t Target) Tables(ctx context.Context, db string) ([]Table, error) {
	var q string
	var args []any
	switch t.Engine {
	case SQLServer:
		q = `SELECT s.name, t.name, CAST(SUM(p.rows) AS bigint) FROM sys.tables t
			JOIN sys.schemas s ON s.schema_id = t.schema_id
			JOIN sys.partitions p ON p.object_id = t.object_id AND p.index_id IN (0, 1)
			GROUP BY s.name, t.name ORDER BY s.name, t.name`
	case Postgres:
		q = `SELECT schemaname, relname, n_live_tup FROM pg_stat_user_tables ORDER BY schemaname, relname`
	case MySQL:
		q = `SELECT table_schema, table_name, COALESCE(table_rows, 0) FROM information_schema.tables
			WHERE table_schema = ? AND table_type = 'BASE TABLE' ORDER BY table_name`
		args = []any{db}
	}
	res, err := t.run(ctx, db, q, 100000, args)
	if err != nil {
		return nil, err
	}
	out := make([]Table, 0, len(res.Rows))
	for _, r := range res.Rows {
		tb := Table{}
		tb.Schema, _ = r[0].(string)
		tb.Name, _ = r[1].(string)
		if s, ok := r[2].(string); ok {
			tb.Rows, _ = strconv.ParseInt(s, 10, 64)
		}
		out = append(out, tb)
	}
	return out, nil
}

// Result is what a query returned. Values are strings, or nil for NULL.
type Result struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Affected  int64    `json:"affected"`  // rows changed, for statements that change data
	Truncated bool     `json:"truncated"` // more rows than the limit
	Ms        float64  `json:"ms"`
	Changed   bool     `json:"changed"` // the statement could change data
}

// Rows returns one page of a table.
func (t Target) Rows(ctx context.Context, db, schema, table string, offset, limit int) (Result, error) {
	var q string
	switch t.Engine {
	case SQLServer:
		q = fmt.Sprintf("SELECT * FROM %s.%s ORDER BY (SELECT NULL) OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", bracket(schema), bracket(table), offset, limit)
	case Postgres:
		q = fmt.Sprintf(`SELECT * FROM %s.%s LIMIT %d OFFSET %d`, dquote(schema), dquote(table), limit, offset)
	case MySQL:
		q = fmt.Sprintf("SELECT * FROM %s.%s LIMIT %d OFFSET %d", backtick(schema), backtick(table), limit, offset)
	}
	return t.run(ctx, db, q, limit, nil)
}

// Query runs SQL typed on the page. SQL that may change data (anything but a
// read) is run as a statement and reports the rows it changed.
func (t Target) Query(ctx context.Context, db, query string, limit int) (Result, error) {
	if ReadOnly(query) {
		return t.run(ctx, db, query, limit, nil)
	}
	d, err := t.open(db)
	if err != nil {
		return Result{}, err
	}
	defer d.Close()
	start := time.Now()
	r, err := d.ExecContext(ctx, query)
	if err != nil {
		return Result{}, err
	}
	n, _ := r.RowsAffected()
	return Result{Affected: n, Changed: true, Ms: ms(start)}, nil
}

func (t Target) run(ctx context.Context, db, query string, limit int, args []any) (Result, error) {
	d, err := t.open(db)
	if err != nil {
		return Result{}, err
	}
	defer d.Close()
	start := time.Now()
	rows, err := d.QueryContext(ctx, query, args...)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	res := Result{Rows: [][]any{}}
	types, _ := rows.ColumnTypes()
	res.Columns, _ = rows.Columns()
	vals := make([]any, len(res.Columns))
	ptrs := make([]any, len(vals))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if len(res.Rows) >= limit {
			res.Truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return res, err
		}
		row := make([]any, len(vals))
		for i, v := range vals {
			typ := ""
			if i < len(types) {
				typ = types[i].DatabaseTypeName()
			}
			row[i] = display(v, typ)
		}
		res.Rows = append(res.Rows, row)
	}
	res.Ms = ms(start)
	return res, rows.Err()
}

// MaxCell is the longest value sent to the page; longer ones are cut.
const MaxCell = 4000

// display turns a value from the driver into text for the page.
func display(v any, dbType string) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		if dbType == "UNIQUEIDENTIFIER" && len(x) == 16 {
			return sqlServerGUID(x)
		}
		if utf8.Valid(x) {
			return cut(string(x))
		}
		if len(x) > 64 {
			return "0x" + hex.EncodeToString(x[:64]) + fmt.Sprintf("… (%d bytes)", len(x))
		}
		return "0x" + hex.EncodeToString(x)
	case string:
		return cut(x)
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 {
			return x.Format("2006-01-02")
		}
		return x.Format("2006-01-02 15:04:05.000")
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(x)
	}
}

func cut(s string) string {
	if len(s) <= MaxCell {
		return s
	}
	return s[:MaxCell] + "…"
}

// sqlServerGUID formats SQL Server's uniqueidentifier, whose first three parts
// are stored little-endian.
func sqlServerGUID(b []byte) string {
	g := []byte{b[3], b[2], b[1], b[0], b[5], b[4], b[7], b[6]}
	g = append(g, b[8:]...)
	h := hex.EncodeToString(g)
	return strings.ToUpper(h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:])
}

// ReadOnly says whether SQL only reads. Each statement must start with a
// reading word; anything else counts as a change and needs confirming.
func ReadOnly(query string) bool {
	q := stripComments(query)
	any := false
	for _, stmt := range strings.Split(q, ";") {
		f := strings.Fields(stmt)
		if len(f) == 0 {
			continue
		}
		any = true
		switch strings.ToUpper(f[0]) {
		case "SELECT", "WITH", "SHOW", "EXPLAIN", "DESCRIBE", "DESC", "VALUES", "TABLE":
			up := strings.ToUpper(stmt)
			// SELECT ... INTO makes a table; a WITH can end in a change
			if strings.Contains(up, " INTO ") || containsWord(up, "INSERT", "UPDATE", "DELETE", "MERGE", "DROP", "ALTER", "CREATE", "TRUNCATE", "EXEC", "EXECUTE") {
				return false
			}
		default:
			return false
		}
	}
	return any
}

func containsWord(s string, words ...string) bool {
	f := strings.FieldsFunc(s, func(r rune) bool { return !(r == '_' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') })
	for _, w := range f {
		for _, x := range words {
			if w == x {
				return true
			}
		}
	}
	return false
}

func stripComments(q string) string {
	var b strings.Builder
	for i := 0; i < len(q); i++ {
		switch {
		case strings.HasPrefix(q[i:], "--"):
			for i < len(q) && q[i] != '\n' {
				i++
			}
		case strings.HasPrefix(q[i:], "/*"):
			end := strings.Index(q[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 3
		default:
			b.WriteByte(q[i])
		}
	}
	return b.String()
}

func bracket(s string) string  { return "[" + strings.ReplaceAll(s, "]", "]]") + "]" }
func dquote(s string) string   { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func backtick(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
func ms(t time.Time) float64   { return float64(time.Since(t).Microseconds()) / 1000 }
