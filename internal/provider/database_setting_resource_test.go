package provider_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccDatabaseSetting_basic(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const database = "pgconfig_test_db_basic"
	createTestDatabase(t, db, database)

	const resourceName = "pgconfig_database_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := testAccDatabaseSetting(t, db, database, "statement_timeout"); got != "" {
				return fmt.Errorf("statement_timeout should have been reset, but pg_db_role_setting has %q", got)
			}

			// statement_timeout was this database's only setting, so
			// resetting it must remove the pg_db_role_setting row entirely,
			// not just clear the entry within it.
			if testAccDatabaseSettingRowExists(t, db, database) {
				return fmt.Errorf("pg_db_role_setting row for %s should have been removed after its last setting was reset", database)
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_database_setting" "test" {
						database = %[1]q
						name     = "statement_timeout"
						value    = "5000"
					}
				`, database),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "database", database),
					resource.TestCheckResourceAttr(resourceName, "name", "statement_timeout"),
					resource.TestCheckResourceAttr(resourceName, "value", "5000"),
					resource.TestCheckResourceAttr(resourceName, "quote", "true"),
					testAccCheckDatabaseSetting(t, db, database, "statement_timeout", "statement_timeout=5000"),
				),
			},
			// Update: value change should re-issue ALTER DATABASE ... SET.
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_database_setting" "test" {
						database = %[1]q
						name     = "statement_timeout"
						value    = "6000"
					}
				`, database),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "value", "6000"),
					testAccCheckDatabaseSetting(t, db, database, "statement_timeout", "statement_timeout=6000"),
				),
			},
			// Import.
			{
				ResourceName:                         resourceName,
				ImportState:                          true,
				ImportStateId:                        database + "/statement_timeout",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
	})
}

// TestAccDatabaseSetting_multipleKeys ensures two pgconfig_database_setting
// resources on the same database don't clobber each other's entry in the
// shared pg_db_role_setting.setconfig array. This is
// TestAccRoleSetting_multipleKeys's database_setting counterpart.
func TestAccDatabaseSetting_multipleKeys(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const database = "pgconfig_test_db_multi"
	createTestDatabase(t, db, database)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_database_setting" "a" {
						database = %[1]q
						name     = "statement_timeout"
						value    = "5000"
					}

					resource "pgconfig_database_setting" "b" {
						database = %[1]q
						name     = "idle_in_transaction_session_timeout"
						value    = "7000"
					}
				`, database),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDatabaseSetting(t, db, database, "statement_timeout", "statement_timeout=5000"),
					testAccCheckDatabaseSetting(t, db, database, "idle_in_transaction_session_timeout", "idle_in_transaction_session_timeout=7000"),
				),
			},
			// Destroying resource "a" only must leave "b" intact.
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_database_setting" "b" {
						database = %[1]q
						name     = "idle_in_transaction_session_timeout"
						value    = "7000"
					}
				`, database),
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						if got := testAccDatabaseSetting(t, db, database, "statement_timeout"); got != "" {
							return fmt.Errorf("statement_timeout should have been reset, but pg_db_role_setting has %q", got)
						}
						return nil
					},
					testAccCheckDatabaseSetting(t, db, database, "idle_in_transaction_session_timeout", "idle_in_transaction_session_timeout=7000"),
				),
			},
		},
	})
}

// TestAccDatabaseSetting_pgaudit manages a database-wide pgaudit.log
// setting, e.g. for object-level audit logging.
func TestAccDatabaseSetting_pgaudit(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const database = "pgconfig_test_db_pgaudit"
	createTestDatabase(t, db, database)

	const resourceName = "pgconfig_database_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_database_setting" "test" {
						database = %[1]q
						name     = "pgaudit.log"
						value    = "all,-write,-read,-misc"
					}
				`, database),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "value", "all,-write,-read,-misc"),
					testAccCheckDatabaseSetting(t, db, database, "pgaudit.log", "pgaudit.log=all,-write,-read,-misc"),
				),
			},
		},
	})
}

// TestAccDatabaseSetting_driftDetection verifies that resetting a setting
// outside of Terraform is detected as drift on the next plan.
func TestAccDatabaseSetting_driftDetection(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const database = "pgconfig_test_db_drift"
	createTestDatabase(t, db, database)

	config := fmt.Sprintf(`
		resource "pgconfig_database_setting" "test" {
			database = %[1]q
			name     = "statement_timeout"
			value    = "5000"
		}
	`, database)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				PreConfig: func() {
					testAccExec(t, db, fmt.Sprintf("ALTER DATABASE %s RESET statement_timeout", quoteTestIdentifier(database)))
				},
				Config:             config,
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}

// TestAccDatabaseSetting_replaceOnDatabaseChange verifies that database's
// RequiresReplace plan modifier actually forces a destroy+recreate (rather
// than an in-place update), and that the old database's setting is reset
// as part of that.
func TestAccDatabaseSetting_replaceOnDatabaseChange(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const databaseA = "pgconfig_test_db_replace_a"
	const databaseB = "pgconfig_test_db_replace_b"
	createTestDatabase(t, db, databaseA)
	createTestDatabase(t, db, databaseB)

	const resourceName = "pgconfig_database_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_database_setting" "test" {
						database = %[1]q
						name     = "statement_timeout"
						value    = "5000"
					}
				`, databaseA),
				Check: testAccCheckDatabaseSetting(t, db, databaseA, "statement_timeout", "statement_timeout=5000"),
			},
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_database_setting" "test" {
						database = %[1]q
						name     = "statement_timeout"
						value    = "5000"
					}
				`, databaseB),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(resourceName, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDatabaseSetting(t, db, databaseB, "statement_timeout", "statement_timeout=5000"),
					func(*terraform.State) error {
						if got := testAccDatabaseSetting(t, db, databaseA, "statement_timeout"); got != "" {
							return fmt.Errorf("statement_timeout on %s should have been reset after replace, but pg_db_role_setting has %q", databaseA, got)
						}
						return nil
					},
				),
			},
		},
	})
}

func testAccCheckDatabaseSetting(t *testing.T, db *sql.DB, database, name, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := testAccDatabaseSetting(t, db, database, name); got != want {
			return fmt.Errorf("pg_db_role_setting entry for %s: got %q, want %q", name, got, want)
		}
		return nil
	}
}
