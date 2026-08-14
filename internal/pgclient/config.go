// Package pgclient builds PostgreSQL connections for the pgconfig provider.
package pgclient

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ClientCertificateConfig holds the client certificate used for SSL
// connections (the `clientcert` provider block).
type ClientCertificateConfig struct {
	CertificatePath string
	KeyPath         string
	SSLInline       bool
}

// Config holds the connection parameters for the pgconfig provider. It
// mirrors cyrilgdn/terraform-provider-postgresql's provider schema so the
// two providers can be configured the same way.
type Config struct {
	Scheme                        string
	Host                          string
	Port                          int64
	Database                      string
	Username                      string
	Password                      string
	SSLMode                       string
	SSLRootCertPath               string
	SSLClientCert                 *ClientCertificateConfig
	ConnectTimeoutSec             int64
	MaxConnRetries                int64
	ConnectionRetryTimeoutSeconds int64
	MaxConns                      int64
	ConnMaxLifetimeSeconds        int64
}

// connInfo builds a libpq "keyword = 'value'" connection string. This format
// (rather than a postgres:// URL) avoids having to URL-escape usernames and
// passwords with special characters.
func (c *Config) connInfo() string {
	params := []string{
		"host=" + quoteConnInfoValue(c.Host),
		"port=" + quoteConnInfoValue(strconv.FormatInt(c.Port, 10)),
		"dbname=" + quoteConnInfoValue(c.Database),
		"user=" + quoteConnInfoValue(c.Username),
		"password=" + quoteConnInfoValue(c.Password),
		"application_name=" + quoteConnInfoValue("terraform-provider-pgconfig"),
		"connect_timeout=" + quoteConnInfoValue(strconv.FormatInt(c.ConnectTimeoutSec, 10)),
	}

	if c.SSLMode != "" {
		params = append(params, "sslmode="+quoteConnInfoValue(c.SSLMode))
	}

	if c.SSLRootCertPath != "" {
		params = append(params, "sslrootcert="+quoteConnInfoValue(c.SSLRootCertPath))
	}

	if c.SSLClientCert != nil {
		params = append(params,
			"sslcert="+quoteConnInfoValue(c.SSLClientCert.CertificatePath),
			"sslkey="+quoteConnInfoValue(c.SSLClientCert.KeyPath),
		)

		if c.SSLClientCert.SSLInline {
			params = append(params, "sslinline="+quoteConnInfoValue("true"))
		}
	}

	return strings.Join(params, " ")
}

func quoteConnInfoValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	return "'" + v + "'"
}

// Client lazily opens and caches a single shared *sql.DB for a Config. All
// pgconfig resources share one connection regardless of which role/database
// they operate on: pg_db_role_setting is a shared (cluster-wide) catalog, so
// ALTER ROLE/DATABASE for any target works over a connection to the
// provider's configured `database`.
type Client struct {
	config Config

	once sync.Once
	db   *sql.DB
	err  error
}

// NewClient returns a Client for the given Config.
func NewClient(config Config) *Client {
	return &Client{config: config}
}

// DB returns the shared *sql.DB, connecting (with retry) on first use.
func (c *Client) DB(ctx context.Context) (*sql.DB, error) {
	c.once.Do(func() {
		c.db, c.err = c.connect(ctx)
	})

	return c.db, c.err
}

// connect opens the connection, retrying transient failures up to
// MaxConnRetries times or until ConnectionRetryTimeoutSeconds elapses,
// whichever comes first.
func (c *Client) connect(ctx context.Context) (*sql.DB, error) {
	dsn := c.config.connInfo()
	deadline := time.Now().Add(time.Duration(c.config.ConnectionRetryTimeoutSeconds) * time.Second)

	var db *sql.DB
	var err error
	var retryCount int64

	for {
		db, err = sql.Open("pgx", dsn)

		if err == nil {
			err = db.PingContext(ctx)
		}

		if err == nil {
			break
		}

		if db != nil {
			db.Close()
			db = nil
		}

		if retryCount >= c.config.MaxConnRetries || time.Now().After(deadline) {
			return nil, fmt.Errorf("failed to connect to PostgreSQL server %s:%d: %w", c.config.Host, c.config.Port, err)
		}

		retryCount++
		time.Sleep(time.Second)
	}

	db.SetMaxIdleConns(0)
	db.SetMaxOpenConns(int(c.config.MaxConns))
	db.SetConnMaxLifetime(time.Duration(c.config.ConnMaxLifetimeSeconds) * time.Second)

	return db, nil
}
