package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// paramNameRegexp matches a bare GUC name (statement_timeout) or a
// dot-namespaced custom GUC (pgaudit.log). It intentionally rejects
// anything else, since name is embedded directly into SQL text and cannot
// be passed as a bind parameter or quoted as an identifier (the dot must be
// preserved as-is).
var paramNameRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

func quoteIdentifier(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// quoteLiteral builds a standard SQL string literal. It uses the E'...'
// escape syntax and doubles backslashes whenever the value contains one, so
// the result is safe regardless of the server's standard_conforming_strings
// setting.
func quoteLiteral(value string) string {
	if strings.Contains(value, `\`) {
		return "E'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(value) + "'"
	}

	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func valueExpr(value string, quote bool) string {
	if quote {
		return quoteLiteral(value)
	}

	return value
}

func buildRoleSetSQL(role, database, name, value string, quote bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ALTER ROLE %s ", quoteIdentifier(role))

	if database != "" {
		fmt.Fprintf(&b, "IN DATABASE %s ", quoteIdentifier(database))
	}

	fmt.Fprintf(&b, "SET %s = %s", name, valueExpr(value, quote))

	return b.String()
}

func buildRoleResetSQL(role, database, name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ALTER ROLE %s ", quoteIdentifier(role))

	if database != "" {
		fmt.Fprintf(&b, "IN DATABASE %s ", quoteIdentifier(database))
	}

	fmt.Fprintf(&b, "RESET %s", name)

	return b.String()
}

func buildDatabaseSetSQL(database, name, value string, quote bool) string {
	return fmt.Sprintf("ALTER DATABASE %s SET %s = %s", quoteIdentifier(database), name, valueExpr(value, quote))
}

func buildDatabaseResetSQL(database, name string) string {
	return fmt.Sprintf("ALTER DATABASE %s RESET %s", quoteIdentifier(database), name)
}

// execWithAdvisoryLock runs stmt inside a transaction serialized by a
// session-level advisory lock keyed on lockKey. pg_db_role_setting rows are
// created lazily (there is no row until the first SET for a given
// role/database), so two concurrent first-time ALTER ROLE/DATABASE ... SET
// statements for the same target race on inserting that row and one loses
// with a duplicate key error. Terraform applies independent resources (e.g.
// two pgconfig_role_setting resources for the same role but different
// parameter names) concurrently by default, so the provider must serialize
// this itself rather than relying on configuration-level ordering.
func execWithAdvisoryLock(ctx context.Context, db *sql.DB, lockKey, stmt string) error {
	tx, err := db.BeginTx(ctx, nil)

	if err != nil {
		return err
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext($1)::bigint)", lockKey); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, stmt); err != nil {
		return err
	}

	return tx.Commit()
}

func roleLockKey(role, database string) string {
	return "pgconfig_role_setting\x1f" + role + "\x1f" + database
}

func databaseLockKey(database string) string {
	return "pgconfig_database_setting\x1f" + database
}

// findSettingEntry looks up `name` in a pg_db_role_setting row (identified
// by the WHERE clause in `query`, whose args are `args...`). It unnests
// setconfig and filters by parameter name in SQL, rather than scanning the
// whole text[] value into Go, so no Postgres-array-aware scan type is
// needed. It returns the raw "name=value" entry and whether it was found.
func findSettingEntry(ctx context.Context, db *sql.DB, query string, args ...any) (string, bool, error) {
	var entry string
	err := db.QueryRowContext(ctx, query, args...).Scan(&entry)

	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, err
	}

	return entry, true, nil
}

// sqlErrorHint appends a hint about the privileges required to alter role
// or database configuration parameters when the error looks like a
// permission error (SQLSTATE class 42, insufficient_privilege).
func sqlErrorHint(err error) string {
	msg := err.Error()

	var pgErr *pgconn.PgError

	if errors.As(err, &pgErr) && len(pgErr.Code) >= 2 && pgErr.Code[:2] == "42" {
		return msg + "\n\nAltering role/database configuration parameters typically requires superuser " +
			"(or rds_superuser on RDS), or PostgreSQL 15+'s GRANT SET ON PARAMETER."
	}

	return msg
}
