package pgclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateAWSEnv points the AWS SDK's config/credentials resolution at
// per-test static values (and nonexistent shared config/credentials files),
// so GetRDSAuthToken's tests are hermetic: they don't depend on, and can't
// be broken or slowed down by, real credentials/network access on the
// machine running them. BuildAuthToken itself never makes a network call
// (it's a local SigV4 presign), so once credentials resolve, these tests
// run fully offline.
func isolateAWSEnv(t *testing.T) {
	t.Helper()

	dir := t.TempDir()

	for k, v := range map[string]string{
		"AWS_ACCESS_KEY_ID":                      "AKIAEXAMPLE",
		"AWS_SECRET_ACCESS_KEY":                  "secretexample",
		"AWS_SESSION_TOKEN":                      "",
		"AWS_PROFILE":                            "",
		"AWS_SHARED_CREDENTIALS_FILE":            filepath.Join(dir, "nonexistent-credentials"),
		"AWS_CONFIG_FILE":                        filepath.Join(dir, "nonexistent-config"),
		"AWS_EC2_METADATA_DISABLED":              "true",
		"AWS_REGION":                             "",
		"AWS_DEFAULT_REGION":                     "",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI": "",
		"AWS_WEB_IDENTITY_TOKEN_FILE":            "",
	} {
		t.Setenv(k, v)
	}
}

func TestGetRDSAuthToken_regionBranch(t *testing.T) {
	isolateAWSEnv(t)

	token, err := GetRDSAuthToken(context.Background(), "us-east-1", "", "", "testuser", "db.example.com", 5432)

	if err != nil {
		t.Fatalf("GetRDSAuthToken() error = %s", err)
	}

	if !strings.HasPrefix(token, "db.example.com:5432?") {
		t.Errorf("token %q does not start with the expected endpoint", token)
	}

	if !strings.Contains(token, "DBUser=testuser") {
		t.Errorf("token %q does not contain DBUser=testuser", token)
	}
}

func TestGetRDSAuthToken_defaultBranch(t *testing.T) {
	isolateAWSEnv(t)
	t.Setenv("AWS_REGION", "us-west-2")

	token, err := GetRDSAuthToken(context.Background(), "", "", "", "testuser", "db.example.com", 5432)

	if err != nil {
		t.Fatalf("GetRDSAuthToken() error = %s", err)
	}

	if !strings.Contains(token, "DBUser=testuser") {
		t.Errorf("token %q does not contain DBUser=testuser", token)
	}
}

func TestGetRDSAuthToken_profileBranch(t *testing.T) {
	isolateAWSEnv(t)

	dir := t.TempDir()
	credentialsPath := filepath.Join(dir, "credentials")
	configPath := filepath.Join(dir, "config")

	if err := os.WriteFile(credentialsPath, []byte(
		"[testprofile]\naws_access_key_id = AKIAEXAMPLE\naws_secret_access_key = secretexample\n",
	), 0o600); err != nil {
		t.Fatalf("failed to write credentials file: %s", err)
	}

	if err := os.WriteFile(configPath, []byte(
		"[profile testprofile]\nregion = us-west-2\n",
	), 0o600); err != nil {
		t.Fatalf("failed to write config file: %s", err)
	}

	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsPath)
	t.Setenv("AWS_CONFIG_FILE", configPath)

	token, err := GetRDSAuthToken(context.Background(), "", "testprofile", "", "testuser", "db.example.com", 5432)

	if err != nil {
		t.Fatalf("GetRDSAuthToken() error = %s", err)
	}

	if !strings.Contains(token, "DBUser=testuser") {
		t.Errorf("token %q does not contain DBUser=testuser", token)
	}
}

func TestGetRDSAuthToken_invalidProfile(t *testing.T) {
	isolateAWSEnv(t)

	_, err := GetRDSAuthToken(context.Background(), "", "no-such-profile", "", "testuser", "db.example.com", 5432)

	if err == nil {
		t.Fatal("expected an error for a nonexistent profile, got nil")
	}

	if !strings.Contains(err.Error(), "could not load AWS default config") {
		t.Errorf("error %q does not have the expected wrapping message", err.Error())
	}
}
