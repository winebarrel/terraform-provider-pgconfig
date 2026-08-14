resource "pgconfig_role_setting" "app_role_pgaudit_log" {
  role  = "app_role"
  name  = "pgaudit.log"
  value = "all"
}

# ALTER ROLE ... IN DATABASE ... SET
resource "pgconfig_role_setting" "app_role_pgaudit_log_app_db" {
  role     = "app_role"
  database = "app_db"
  name     = "pgaudit.log"
  value    = "all"
}
