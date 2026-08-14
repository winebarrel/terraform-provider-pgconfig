package provider_test

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// These tests exercise provider.Configure's own validation/setup branches
// directly through Terraform plan/apply, without touching a real database:
// they either fail inside Configure itself (before any pgclient.Client is
// created), or fail on the first connection attempt against a closed local
// port, which returns immediately. They run as regular (non-acceptance)
// unit tests via IsUnitTest: true, so they don't need TF_ACC or Docker.

func TestProviderConfigure_missingHost(t *testing.T) {
	t.Setenv("PGHOST", "")

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
					provider "pgconfig" {
						username = "test"
					}

					resource "pgconfig_role_setting" "test" {
						role  = "x"
						name  = "y"
						value = "z"
					}
				`,
				ExpectError: regexp.MustCompile(`Missing PostgreSQL Host`),
			},
		},
	})
}

func TestProviderConfigure_missingUsername(t *testing.T) {
	t.Setenv("PGUSER", "")

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
					provider "pgconfig" {
						host = "localhost"
					}

					resource "pgconfig_role_setting" "test" {
						role  = "x"
						name  = "y"
						value = "z"
					}
				`,
				ExpectError: regexp.MustCompile(`Missing PostgreSQL Username`),
			},
		},
	})
}

// TestProviderConfigure_awsRDSIAMAuthError exercises the aws_rds_iam_auth
// branch's error path: a nonexistent AWS profile makes token generation
// fail inside Configure itself, before any PostgreSQL connection attempt.
func TestProviderConfigure_awsRDSIAMAuthError(t *testing.T) {
	dir := t.TempDir()

	for k, v := range map[string]string{
		"AWS_ACCESS_KEY_ID":           "",
		"AWS_SECRET_ACCESS_KEY":       "",
		"AWS_PROFILE":                 "",
		"AWS_SHARED_CREDENTIALS_FILE": filepath.Join(dir, "nonexistent-credentials"),
		"AWS_CONFIG_FILE":             filepath.Join(dir, "nonexistent-config"),
		"AWS_EC2_METADATA_DISABLED":   "true",
	} {
		t.Setenv(k, v)
	}

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
					provider "pgconfig" {
						host                = "localhost"
						username            = "test"
						aws_rds_iam_auth    = true
						aws_rds_iam_profile = "no-such-profile-xyz"
					}

					resource "pgconfig_role_setting" "test" {
						role  = "x"
						name  = "y"
						value = "z"
					}
				`,
				ExpectError: regexp.MustCompile(`Failed to build RDS IAM auth token`),
			},
		},
	})
}

// TestProviderConfigure_clientCert exercises the clientcert-parsing branch
// in Configure. It points at an unreachable port so the resource's Create
// step fails fast on the connection attempt (no real server needed),
// exercising RoleSettingResource.Create's "failed to connect" branch too.
func TestProviderConfigure_clientCert(t *testing.T) {
	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
					provider "pgconfig" {
						host              = "127.0.0.1"
						port              = 1
						username          = "test"
						sslmode           = "require"
						connect_timeout   = 1
						max_conn_retries  = 0

						clientcert = {
							cert = "/nonexistent/client.crt"
							key  = "/nonexistent/client.key"
						}
					}

					resource "pgconfig_role_setting" "test" {
						role  = "x"
						name  = "y"
						value = "z"
					}
				`,
				ExpectError: regexp.MustCompile(`.`),
			},
		},
	})
}

// TestProviderConfigure_connectFailure_databaseSetting is the
// pgconfig_database_setting counterpart of TestProviderConfigure_clientCert:
// it exercises DatabaseSettingResource.Create's "failed to connect" branch
// the same way, against an unreachable local port.
func TestProviderConfigure_connectFailure_databaseSetting(t *testing.T) {
	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
					provider "pgconfig" {
						host              = "127.0.0.1"
						port              = 1
						username          = "test"
						connect_timeout   = 1
						max_conn_retries  = 0
					}

					resource "pgconfig_database_setting" "test" {
						database = "x"
						name     = "y"
						value    = "z"
					}
				`,
				ExpectError: regexp.MustCompile(`.`),
			},
		},
	})
}
