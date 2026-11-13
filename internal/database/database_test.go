package database

import "testing"

func TestDetect(t *testing.T) {
	env := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	tg, ok := Detect("mcr.microsoft.com/mssql/server:2022-latest", "172.18.0.2", env(map[string]string{"MSSQL_SA_PASSWORD": "pw"}))
	if !ok || tg.Engine != SQLServer || tg.User != "sa" || tg.Password != "pw" || tg.Port != 1433 || tg.NeedsLogin {
		t.Errorf("sql server = %+v", tg)
	}
	tg, ok = Detect("postgres:16-alpine", "172.18.0.3", env(map[string]string{"POSTGRES_PASSWORD": "pw", "POSTGRES_USER": "shop"}))
	if !ok || tg.Engine != Postgres || tg.User != "shop" || tg.Default != "shop" {
		t.Errorf("postgres = %+v", tg)
	}
	tg, ok = Detect("mariadb:11", "172.18.0.4", env(map[string]string{}))
	if !ok || tg.Engine != MySQL || !tg.NeedsLogin {
		t.Errorf("mariadb without a password should ask for a login: %+v", tg)
	}
	if _, ok := Detect("caddy:2.10-alpine", "", env(nil)); ok {
		t.Error("caddy is not a database")
	}
}

func TestReadOnly(t *testing.T) {
	for q, want := range map[string]bool{
		"SELECT * FROM Users":                         true,
		"  -- users\nselect top 10 * from Users;":     true,
		"WITH x AS (SELECT 1 AS a) SELECT * FROM x":   true,
		"SHOW TABLES":                                 true,
		"SELECT * INTO Copy FROM Users":               false,
		"DELETE FROM Users WHERE Id = 3":              false,
		"UPDATE Users SET Name = 'x'":                 false,
		"SELECT 1; DROP TABLE Users":                  false,
		"/* comment */ TRUNCATE TABLE Logs":           false,
		"EXEC sp_who":                                 false,
		"WITH d AS (SELECT 1 AS a) DELETE FROM Users": false,
		"": false,
	} {
		if got := ReadOnly(q); got != want {
			t.Errorf("ReadOnly(%q) = %v, want %v", q, got, want)
		}
	}
}

func TestDisplay(t *testing.T) {
	guid := []byte{0x67, 0x45, 0x23, 0x01, 0xab, 0x89, 0xef, 0xcd, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	if got := display(guid, "UNIQUEIDENTIFIER"); got != "01234567-89AB-CDEF-0123-456789ABCDEF" {
		t.Errorf("guid = %v", got)
	}
	if got := display([]byte{0xff, 0x00}, "VARBINARY"); got != "0xff00" {
		t.Errorf("binary = %v", got)
	}
	if got := display(nil, ""); got != nil {
		t.Errorf("null = %v", got)
	}
	if got := display(int64(42), ""); got != "42" {
		t.Errorf("int = %v", got)
	}
}
