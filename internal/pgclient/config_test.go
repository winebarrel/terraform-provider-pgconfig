package pgclient

import (
	"context"
	"strings"
	"testing"
)

func TestConfig_connInfo(t *testing.T) {
	t.Run("base fields only", func(t *testing.T) {
		cfg := Config{
			Host:              "db.example.internal",
			Port:              5432,
			Database:          "postgres",
			Username:          "app",
			Password:          "s3cret",
			ConnectTimeoutSec: 10,
		}

		got := cfg.connInfo()

		for _, want := range []string{
			"host='db.example.internal'",
			"port='5432'",
			"dbname='postgres'",
			"user='app'",
			"password='s3cret'",
			"connect_timeout='10'",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("connInfo() = %q, missing %q", got, want)
			}
		}

		for _, notWant := range []string{"sslmode=", "sslrootcert=", "sslcert=", "sslkey=", "sslinline="} {
			if strings.Contains(got, notWant) {
				t.Errorf("connInfo() = %q, unexpectedly contains %q", got, notWant)
			}
		}
	})

	t.Run("sslmode and sslrootcert", func(t *testing.T) {
		cfg := Config{
			Host:            "db",
			SSLMode:         "verify-full",
			SSLRootCertPath: "/etc/ssl/root.pem",
		}

		got := cfg.connInfo()

		for _, want := range []string{"sslmode='verify-full'", "sslrootcert='/etc/ssl/root.pem'"} {
			if !strings.Contains(got, want) {
				t.Errorf("connInfo() = %q, missing %q", got, want)
			}
		}
	})

	t.Run("client cert without inline", func(t *testing.T) {
		cfg := Config{
			Host: "db",
			SSLClientCert: &ClientCertificateConfig{
				CertificatePath: "/etc/ssl/client.crt",
				KeyPath:         "/etc/ssl/client.key",
			},
		}

		got := cfg.connInfo()

		for _, want := range []string{"sslcert='/etc/ssl/client.crt'", "sslkey='/etc/ssl/client.key'"} {
			if !strings.Contains(got, want) {
				t.Errorf("connInfo() = %q, missing %q", got, want)
			}
		}

		if strings.Contains(got, "sslinline=") {
			t.Errorf("connInfo() = %q, unexpectedly contains sslinline= when SSLInline is false", got)
		}
	})

	t.Run("client cert with inline", func(t *testing.T) {
		cfg := Config{
			Host: "db",
			SSLClientCert: &ClientCertificateConfig{
				CertificatePath: "-----BEGIN CERTIFICATE-----",
				KeyPath:         "-----BEGIN PRIVATE KEY-----",
				SSLInline:       true,
			},
		}

		got := cfg.connInfo()

		if !strings.Contains(got, "sslinline='true'") {
			t.Errorf("connInfo() = %q, missing sslinline='true'", got)
		}
	})

	t.Run("special characters are escaped", func(t *testing.T) {
		cfg := Config{
			Host:     "db",
			Password: `p'a\ss`,
		}

		got := cfg.connInfo()

		if !strings.Contains(got, `password='p\'a\\ss'`) {
			t.Errorf("connInfo() = %q, expected escaped password", got)
		}
	})
}

// TestClient_connect_failure exercises connect()'s retry-exhausted error
// path against an unroutable local address, without needing a reachable
// PostgreSQL server. MaxConnRetries=0 and a short timeout keep it fast.
func TestClient_connect_failure(t *testing.T) {
	cfg := Config{
		Host:                          "127.0.0.1",
		Port:                          1,
		Database:                      "x",
		Username:                      "x",
		Password:                      "x",
		ConnectTimeoutSec:             1,
		MaxConnRetries:                0,
		ConnectionRetryTimeoutSeconds: 1,
	}

	client := NewClient(cfg)
	_, err := client.DB(context.Background())

	if err == nil {
		t.Fatal("expected a connection error, got nil")
	}

	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("error %q does not mention the target address", err.Error())
	}

	// A second call must reuse the cached (failed) result via sync.Once
	// rather than attempting to reconnect.
	_, err2 := client.DB(context.Background())

	if err2 != err {
		t.Errorf("second DB() call returned a different error: %v vs %v", err2, err)
	}
}
