package provider_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/jackc/pgx/v5"
)

func quoteTestIdentifier(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

func createTestRole(t *testing.T, db *sql.DB, role string) {
	t.Helper()

	testAccExec(t, db, fmt.Sprintf("DROP ROLE IF EXISTS %s", quoteTestIdentifier(role)))
	testAccExec(t, db, fmt.Sprintf("CREATE ROLE %s", quoteTestIdentifier(role)))

	t.Cleanup(func() {
		db.Exec(fmt.Sprintf("DROP ROLE IF EXISTS %s", quoteTestIdentifier(role)))
	})
}

func createTestDatabase(t *testing.T, db *sql.DB, database string) {
	t.Helper()

	testAccExec(t, db, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteTestIdentifier(database)))
	testAccExec(t, db, fmt.Sprintf("CREATE DATABASE %s", quoteTestIdentifier(database)))

	t.Cleanup(func() {
		db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteTestIdentifier(database)))
	})
}

func TestAccRoleSetting_basic(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const role = "pgconfig_test_role_basic"
	createTestRole(t, db, role)

	const resourceName = "pgconfig_role_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := testAccRoleSetting(t, db, role, "", "statement_timeout"); got != "" {
				return fmt.Errorf("statement_timeout should have been reset, but pg_db_role_setting has %q", got)
			}

			// statement_timeout was this role's only setting, so resetting it
			// must remove the pg_db_role_setting row entirely, not just clear
			// the entry within it.
			if testAccRoleSettingRowExists(t, db, role, "") {
				return fmt.Errorf("pg_db_role_setting row for %s should have been removed after its last setting was reset", role)
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "statement_timeout"
						value = "5000"
					}
				`, role),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "role", role),
					resource.TestCheckNoResourceAttr(resourceName, "database"),
					resource.TestCheckResourceAttr(resourceName, "name", "statement_timeout"),
					resource.TestCheckResourceAttr(resourceName, "value", "5000"),
					resource.TestCheckResourceAttr(resourceName, "quote", "true"),
					testAccCheckRoleSetting(t, db, role, "", "statement_timeout", "statement_timeout=5000"),
				),
			},
			// Update: value change should re-issue ALTER ROLE ... SET.
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "statement_timeout"
						value = "6000"
					}
				`, role),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "value", "6000"),
					testAccCheckRoleSetting(t, db, role, "", "statement_timeout", "statement_timeout=6000"),
				),
			},
			// Import.
			{
				ResourceName:                         resourceName,
				ImportState:                          true,
				ImportStateId:                        role + "//statement_timeout",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
	})
}

func TestAccRoleSetting_inDatabase(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const role = "pgconfig_test_role_indb"
	const database = "pgconfig_test_db_indb"
	createTestRole(t, db, role)
	createTestDatabase(t, db, database)

	const resourceName = "pgconfig_role_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := testAccRoleSetting(t, db, role, database, "statement_timeout"); got != "" {
				return fmt.Errorf("statement_timeout should have been reset, but pg_db_role_setting has %q", got)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role     = %[1]q
						database = %[2]q
						name     = "statement_timeout"
						value    = "5000"
					}
				`, role, database),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "role", role),
					resource.TestCheckResourceAttr(resourceName, "database", database),
					testAccCheckRoleSetting(t, db, role, database, "statement_timeout", "statement_timeout=5000"),
					// The cluster-wide (database = "") setting must remain untouched.
					func(*terraform.State) error {
						if got := testAccRoleSetting(t, db, role, "", "statement_timeout"); got != "" {
							return fmt.Errorf("cluster-wide statement_timeout should be empty, got %q", got)
						}
						return nil
					},
				),
			},
			{
				ResourceName:                         resourceName,
				ImportState:                          true,
				ImportStateId:                        role + "/" + database + "/statement_timeout",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
	})
}

// TestAccRoleSetting_multipleKeys ensures two pgconfig_role_setting
// resources on the same role/database don't clobber each other's entry in
// the shared pg_db_role_setting.setconfig array.
func TestAccRoleSetting_multipleKeys(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const role = "pgconfig_test_role_multi"
	createTestRole(t, db, role)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "a" {
						role  = %[1]q
						name  = "statement_timeout"
						value = "5000"
					}

					resource "pgconfig_role_setting" "b" {
						role  = %[1]q
						name  = "idle_in_transaction_session_timeout"
						value = "7000"
					}
				`, role),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckRoleSetting(t, db, role, "", "statement_timeout", "statement_timeout=5000"),
					testAccCheckRoleSetting(t, db, role, "", "idle_in_transaction_session_timeout", "idle_in_transaction_session_timeout=7000"),
				),
			},
			// Destroying resource "a" only must leave "b" intact.
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "b" {
						role  = %[1]q
						name  = "idle_in_transaction_session_timeout"
						value = "7000"
					}
				`, role),
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						if got := testAccRoleSetting(t, db, role, "", "statement_timeout"); got != "" {
							return fmt.Errorf("statement_timeout should have been reset, but pg_db_role_setting has %q", got)
						}
						return nil
					},
					testAccCheckRoleSetting(t, db, role, "", "idle_in_transaction_session_timeout", "idle_in_transaction_session_timeout=7000"),
				),
			},
		},
	})
}

// TestAccRoleSetting_pgaudit exercises the direct motivation for this
// provider: managing a pgaudit.log role setting via Terraform.
func TestAccRoleSetting_pgaudit(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const role = "pgconfig_test_role_pgaudit"
	createTestRole(t, db, role)

	const resourceName = "pgconfig_role_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "pgaudit.log"
						value = "all"
					}
				`, role),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "value", "all"),
					testAccCheckRoleSetting(t, db, role, "", "pgaudit.log", "pgaudit.log=all"),
				),
			},
		},
	})
}

// TestAccRoleSetting_unknownPlaceholderGUC exercises spec 4.5's documented
// (not a bug) behavior: PostgreSQL accepts ALTER ROLE ... SET for any
// dot-namespaced name as a "custom variable class" placeholder, even when
// no extension registers it. docker-compose always loads pgaudit for this
// test suite, so a name under "pgaudit." wouldn't actually exercise the
// unloaded case; this uses a namespace that's guaranteed not to be loaded.
func TestAccRoleSetting_unknownPlaceholderGUC(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const role = "pgconfig_test_role_placeholder"
	createTestRole(t, db, role)

	const resourceName = "pgconfig_role_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "definitely_not_a_loaded_extension.some_setting"
						value = "anything"
					}
				`, role),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "value", "anything"),
					testAccCheckRoleSetting(t, db, role, "", "definitely_not_a_loaded_extension.some_setting",
						"definitely_not_a_loaded_extension.some_setting=anything"),
				),
			},
		},
	})
}

// TestAccRoleSetting_coexistsWithExternalSetting verifies that
// pgconfig_role_setting doesn't clobber a setting on the same role that
// was applied by some other tool (e.g. cyrilgdn/terraform-provider-
// postgresql's postgresql_role "search_path" support) outside of pgconfig,
// and that destroying the pgconfig-managed key leaves it untouched. This
// simulates the external tool via a direct SQL statement rather than
// depending on cyrilgdn's provider.
func TestAccRoleSetting_coexistsWithExternalSetting(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const role = "pgconfig_test_role_coexist"
	createTestRole(t, db, role)

	testAccExec(t, db, fmt.Sprintf("ALTER ROLE %s SET search_path = public", quoteTestIdentifier(role)))

	const resourceName = "pgconfig_role_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "pgaudit.log"
						value = "all"
					}
				`, role),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "value", "all"),
					testAccCheckRoleSetting(t, db, role, "", "pgaudit.log", "pgaudit.log=all"),
					testAccCheckRoleSetting(t, db, role, "", "search_path", "search_path=public"),
				),
			},
			// Destroying the pgconfig-managed key must leave the
			// externally-managed search_path intact.
			{
				Config: `# no pgconfig_role_setting resources`,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						if got := testAccRoleSetting(t, db, role, "", "pgaudit.log"); got != "" {
							return fmt.Errorf("pgaudit.log should have been reset, but pg_db_role_setting has %q", got)
						}
						return nil
					},
					testAccCheckRoleSetting(t, db, role, "", "search_path", "search_path=public"),
				),
			},
		},
	})
}

// TestAccRoleSetting_quoteFalse exercises quote = false, used for values
// like search_path that expect an unquoted list.
func TestAccRoleSetting_quoteFalse(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const role = "pgconfig_test_role_quotefalse"
	createTestRole(t, db, role)

	const resourceName = "pgconfig_role_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "search_path"
						value = "\"$user\", public"
						quote = false
					}
				`, role),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "quote", "false"),
				),
			},
		},
	})
}

// TestAccRoleSetting_driftDetection verifies that resetting a setting
// outside of Terraform is detected as drift on the next plan.
func TestAccRoleSetting_driftDetection(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const role = "pgconfig_test_role_drift"
	createTestRole(t, db, role)

	config := fmt.Sprintf(`
		resource "pgconfig_role_setting" "test" {
			role  = %[1]q
			name  = "statement_timeout"
			value = "5000"
		}
	`, role)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				PreConfig: func() {
					testAccExec(t, db, fmt.Sprintf("ALTER ROLE %s RESET statement_timeout", quoteTestIdentifier(role)))
				},
				Config:             config,
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}

// TestAccRoleSetting_replaceOnRoleChange verifies that role's
// RequiresReplace plan modifier actually forces a destroy+recreate (rather
// than an in-place update), and that the old role's setting is reset as
// part of that.
func TestAccRoleSetting_replaceOnRoleChange(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const roleA = "pgconfig_test_role_replace_a"
	const roleB = "pgconfig_test_role_replace_b"
	createTestRole(t, db, roleA)
	createTestRole(t, db, roleB)

	const resourceName = "pgconfig_role_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "statement_timeout"
						value = "5000"
					}
				`, roleA),
				Check: testAccCheckRoleSetting(t, db, roleA, "", "statement_timeout", "statement_timeout=5000"),
			},
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "statement_timeout"
						value = "5000"
					}
				`, roleB),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(resourceName, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					testAccCheckRoleSetting(t, db, roleB, "", "statement_timeout", "statement_timeout=5000"),
					func(*terraform.State) error {
						if got := testAccRoleSetting(t, db, roleA, "", "statement_timeout"); got != "" {
							return fmt.Errorf("statement_timeout on %s should have been reset after replace, but pg_db_role_setting has %q", roleA, got)
						}
						return nil
					},
				),
			},
		},
	})
}

// TestAccRoleSetting_replaceOnDatabaseChange is the same check for
// database, going from unset (cluster-wide) to set (IN DATABASE).
func TestAccRoleSetting_replaceOnDatabaseChange(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const role = "pgconfig_test_role_replace_db"
	const database = "pgconfig_test_db_replace"
	createTestRole(t, db, role)
	createTestDatabase(t, db, database)

	const resourceName = "pgconfig_role_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "statement_timeout"
						value = "5000"
					}
				`, role),
				Check: testAccCheckRoleSetting(t, db, role, "", "statement_timeout", "statement_timeout=5000"),
			},
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role     = %[1]q
						database = %[2]q
						name     = "statement_timeout"
						value    = "5000"
					}
				`, role, database),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(resourceName, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					testAccCheckRoleSetting(t, db, role, database, "statement_timeout", "statement_timeout=5000"),
					func(*terraform.State) error {
						if got := testAccRoleSetting(t, db, role, "", "statement_timeout"); got != "" {
							return fmt.Errorf("cluster-wide statement_timeout on %s should have been reset after replace, but pg_db_role_setting has %q", role, got)
						}
						return nil
					},
				),
			},
		},
	})
}

// TestAccRoleSetting_quoteToggle exercises an in-place Update (not a
// replace: quote isn't a RequiresReplace attribute) that flips quote from
// its default true to false on an existing resource.
func TestAccRoleSetting_quoteToggle(t *testing.T) {
	testAccPreCheck(t)
	db := testAccDB(t)

	const role = "pgconfig_test_role_quotetoggle"
	createTestRole(t, db, role)

	const resourceName = "pgconfig_role_setting.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "search_path"
						value = "public"
					}
				`, role),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "quote", "true"),
					testAccCheckRoleSetting(t, db, role, "", "search_path", "search_path=public"),
				),
			},
			{
				Config: fmt.Sprintf(`
					resource "pgconfig_role_setting" "test" {
						role  = %[1]q
						name  = "search_path"
						value = "public"
						quote = false
					}
				`, role),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(resourceName, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "quote", "false"),
					testAccCheckRoleSetting(t, db, role, "", "search_path", "search_path=public"),
				),
			},
		},
	})
}

func testAccCheckRoleSetting(t *testing.T, db *sql.DB, role, database, name, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := testAccRoleSetting(t, db, role, database, name); got != want {
			return fmt.Errorf("pg_db_role_setting entry for %s: got %q, want %q", name, got, want)
		}
		return nil
	}
}
