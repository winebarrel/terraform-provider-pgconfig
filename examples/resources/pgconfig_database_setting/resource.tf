resource "pgconfig_database_setting" "pgaudit_log" {
  database = "app_db"
  name     = "pgaudit.log"
  value    = "all,-write,-read,-misc"
}
