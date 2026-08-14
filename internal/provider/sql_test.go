package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// testAccDBForSQLTest opens a connection to the acceptance test database
// (the same one internal/provider/helper_test.go uses), for the one test
// here that needs a real server to exercise a genuine SQL execution error.
// It's a small, deliberate duplication of that helper: package provider's
// internal (white-box) tests and package provider_test's external tests
// are compiled as two separate packages, so symbols can't be shared between
// them.
func testAccDBForSQLTest(t *testing.T) *sql.DB {
	t.Helper()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set, skipping acceptance test")
	}

	envOrDefault := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}

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

// TestExecWithAdvisoryLock_stmtError exercises the branch where the
// advisory lock is acquired successfully but the target statement itself
// fails (e.g. a malformed SET), as opposed to TestExecWithAdvisoryLock_
// beginTxError's connection-level failure.
func TestExecWithAdvisoryLock_stmtError(t *testing.T) {
	db := testAccDBForSQLTest(t)

	err := execWithAdvisoryLock(context.Background(), db, "test-stmt-error-lock", "SELECT this is not valid SQL")

	if err == nil {
		t.Fatal("expected an error for an invalid statement, got nil")
	}
}

func TestQuoteLiteral(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"plain", "all", "'all'"},
		{"single quote", "O'Brien", "'O''Brien'"},
		{"backslash", `C:\pgaudit`, `E'C:\\pgaudit'`},
		{"backslash and quote", `it's C:\pgaudit`, `E'it''s C:\\pgaudit'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quoteLiteral(tt.value); got != tt.want {
				t.Errorf("quoteLiteral(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestValueExpr(t *testing.T) {
	if got := valueExpr("all", true); got != "'all'" {
		t.Errorf("valueExpr(quote=true) = %q, want %q", got, "'all'")
	}

	if got := valueExpr(`"$user", public`, false); got != `"$user", public` {
		t.Errorf("valueExpr(quote=false) = %q, want unquoted passthrough", got)
	}
}

func TestQuoteIdentifier(t *testing.T) {
	if got := quoteIdentifier(`weird"role`); got != `"weird""role"` {
		t.Errorf("quoteIdentifier = %q, want %q", got, `"weird""role"`)
	}
}

func TestBuildRoleSetSQL(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		database string
		want     string
	}{
		{"cluster-wide", "app_role", "", `ALTER ROLE "app_role" SET statement_timeout = '5000'`},
		{"in database", "app_role", "app_db", `ALTER ROLE "app_role" IN DATABASE "app_db" SET statement_timeout = '5000'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRoleSetSQL(tt.role, tt.database, "statement_timeout", "5000", true)
			if got != tt.want {
				t.Errorf("buildRoleSetSQL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildRoleResetSQL(t *testing.T) {
	tests := []struct {
		name     string
		database string
		want     string
	}{
		{"cluster-wide", "", `ALTER ROLE "app_role" RESET statement_timeout`},
		{"in database", "app_db", `ALTER ROLE "app_role" IN DATABASE "app_db" RESET statement_timeout`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRoleResetSQL("app_role", tt.database, "statement_timeout")
			if got != tt.want {
				t.Errorf("buildRoleResetSQL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildDatabaseSetSQL(t *testing.T) {
	got := buildDatabaseSetSQL("app_db", "pgaudit.log", "all", true)
	want := `ALTER DATABASE "app_db" SET pgaudit.log = 'all'`

	if got != want {
		t.Errorf("buildDatabaseSetSQL() = %q, want %q", got, want)
	}
}

func TestBuildDatabaseResetSQL(t *testing.T) {
	got := buildDatabaseResetSQL("app_db", "pgaudit.log")
	want := `ALTER DATABASE "app_db" RESET pgaudit.log`

	if got != want {
		t.Errorf("buildDatabaseResetSQL() = %q, want %q", got, want)
	}
}

func TestRoleLockKey(t *testing.T) {
	if got := roleLockKey("app_role", "app_db"); got != "pgconfig_role_setting\x1fapp_role\x1fapp_db" {
		t.Errorf("roleLockKey() = %q", got)
	}
}

func TestDatabaseLockKey(t *testing.T) {
	if got := databaseLockKey("app_db"); got != "pgconfig_database_setting\x1fapp_db" {
		t.Errorf("databaseLockKey() = %q", got)
	}
}

func TestSqlErrorHint(t *testing.T) {
	t.Run("non-pg error is passed through unchanged", func(t *testing.T) {
		err := errors.New("boom")

		if got := sqlErrorHint(err); got != "boom" {
			t.Errorf("sqlErrorHint() = %q, want %q", got, "boom")
		}
	})

	t.Run("insufficient_privilege (class 42) gets a hint", func(t *testing.T) {
		err := &pgconn.PgError{Code: "42501", Message: "permission denied"}
		got := sqlErrorHint(err)

		if got == err.Error() {
			t.Errorf("sqlErrorHint() did not append a hint for class-42 error: %q", got)
		}
	})

	t.Run("non-class-42 pg error is passed through unchanged", func(t *testing.T) {
		err := &pgconn.PgError{Code: "23505", Message: "duplicate key value"}

		if got := sqlErrorHint(err); got != err.Error() {
			t.Errorf("sqlErrorHint() = %q, want unchanged %q", got, err.Error())
		}
	})
}

// TestExecWithAdvisoryLock_beginTxError exercises the BeginTx error branch
// without needing a reachable PostgreSQL server: closing the *sql.DB first
// makes BeginTx fail deterministically and immediately.
func TestExecWithAdvisoryLock_beginTxError(t *testing.T) {
	db, err := sql.Open("pgx", "host=127.0.0.1 port=1 dbname=x user=x password=x")

	if err != nil {
		t.Fatalf("sql.Open: %s", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %s", err)
	}

	err = execWithAdvisoryLock(context.Background(), db, "lock-key", "SELECT 1")

	if err == nil {
		t.Fatal("expected an error from execWithAdvisoryLock on a closed *sql.DB, got nil")
	}
}

// TestFindSettingEntry_queryError exercises the non-ErrNoRows error branch
// the same way, without needing a reachable PostgreSQL server.
func TestFindSettingEntry_queryError(t *testing.T) {
	db, err := sql.Open("pgx", "host=127.0.0.1 port=1 dbname=x user=x password=x")

	if err != nil {
		t.Fatalf("sql.Open: %s", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %s", err)
	}

	_, ok, err := findSettingEntry(context.Background(), db, "SELECT 1")

	if err == nil {
		t.Fatal("expected an error from findSettingEntry on a closed *sql.DB, got nil")
	}

	if ok {
		t.Error("expected ok=false on error")
	}
}
