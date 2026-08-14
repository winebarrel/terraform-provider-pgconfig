package provider_test

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}

// testAccPreCheck skips the test unless TF_ACC is set. Test functions call
// this before touching the database (via testAccDB/createTestRole/
// createTestDatabase), so plain `go test` (no TF_ACC, no PostgreSQL server
// running) skips cleanly instead of failing on a connection error.
// resource.Test() has its own TF_ACC gate, but only takes effect once
// resource.Test() itself runs, which is after our own fixture setup.
func testAccPreCheck(t *testing.T) {
	t.Helper()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set, skipping acceptance test")
	}

	if os.Getenv("PGHOST") == "" {
		t.Fatal("PGHOST must be set for acceptance tests")
	}
}

// testAccDB opens a direct connection to the acceptance test database, used
// to set up fixtures (roles/databases) and to assert on PostgreSQL catalog
// state that the provider itself doesn't expose.
func testAccDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		envOrDefault("PGHOST", "localhost"),
		envOrDefault("PGPORT", "5432"),
		envOrDefault("PGUSER", "postgres"),
		envOrDefault("PGPASSWORD", "postgres"),
		envOrDefault("PGDATABASE", "postgres"),
		envOrDefault("PGSSLMODE", "disable"),
	)

	db, err := sql.Open("pgx", dsn)

	if err != nil {
		t.Fatalf("failed to open acceptance test database: %s", err)
	}

	t.Cleanup(func() { db.Close() })

	return db
}

func testAccExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("failed to exec %q: %s", query, err)
	}
}

// testAccRoleSetting returns the raw "name=value" entry for `name` in the
// pg_db_role_setting row for (role, database), or "" if there is no such
// entry (either the row doesn't exist, or it exists but lacks `name`).
func testAccRoleSetting(t *testing.T, db *sql.DB, role, database, name string) string {
	t.Helper()

	const query = `
		SELECT entry
		FROM pg_db_role_setting s
		JOIN pg_roles r ON r.oid = s.setrole
		CROSS JOIN LATERAL unnest(s.setconfig) AS entry
		WHERE r.rolname = $1
		  AND s.setdatabase = CASE
		        WHEN $2 = '' THEN 0
		        ELSE (SELECT oid FROM pg_database WHERE datname = $2)
		      END
		  AND split_part(entry, '=', 1) = $3
	`

	return queryMatchingSetting(t, db, query, role, database, name)
}

// testAccDatabaseSetting returns the raw "name=value" entry for `name` in the
// pg_db_role_setting row for `database` (setrole = 0), or "" if absent.
func testAccDatabaseSetting(t *testing.T, db *sql.DB, database, name string) string {
	t.Helper()

	const query = `
		SELECT entry
		FROM pg_db_role_setting s
		JOIN pg_database d ON d.oid = s.setdatabase
		CROSS JOIN LATERAL unnest(s.setconfig) AS entry
		WHERE d.datname = $1 AND s.setrole = 0
		  AND split_part(entry, '=', 1) = $2
	`

	return queryMatchingSetting(t, db, query, database, name)
}

func queryMatchingSetting(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()

	var entry string
	err := db.QueryRow(query, args...).Scan(&entry)

	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}

	if err != nil {
		t.Fatalf("failed to query pg_db_role_setting: %s", err)
	}

	return entry
}

// testAccRoleSettingRowExists reports whether a pg_db_role_setting row
// exists at all for (role, database), as opposed to testAccRoleSetting,
// which only reports whether a particular entry is present within that
// row. RESET removes the row entirely once its last entry is gone, rather
// than leaving an empty array behind; this distinguishes that case.
func testAccRoleSettingRowExists(t *testing.T, db *sql.DB, role, database string) bool {
	t.Helper()

	const query = `
		SELECT 1
		FROM pg_db_role_setting s
		JOIN pg_roles r ON r.oid = s.setrole
		WHERE r.rolname = $1
		  AND s.setdatabase = CASE
		        WHEN $2 = '' THEN 0
		        ELSE (SELECT oid FROM pg_database WHERE datname = $2)
		      END
	`

	return queryRowExists(t, db, query, role, database)
}

// testAccDatabaseSettingRowExists is testAccRoleSettingRowExists's
// database_setting counterpart.
func testAccDatabaseSettingRowExists(t *testing.T, db *sql.DB, database string) bool {
	t.Helper()

	const query = `
		SELECT 1
		FROM pg_db_role_setting s
		JOIN pg_database d ON d.oid = s.setdatabase
		WHERE d.datname = $1 AND s.setrole = 0
	`

	return queryRowExists(t, db, query, database)
}

func queryRowExists(t *testing.T, db *sql.DB, query string, args ...any) bool {
	t.Helper()

	var one int
	err := db.QueryRow(query, args...).Scan(&one)

	if errors.Is(err, sql.ErrNoRows) {
		return false
	}

	if err != nil {
		t.Fatalf("failed to query pg_db_role_setting: %s", err)
	}

	return true
}
