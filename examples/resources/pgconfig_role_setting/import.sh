# <role>/<database>/<name> (database may be empty for a cluster-wide setting)
terraform import pgconfig_role_setting.app_role_pgaudit_log app_role//pgaudit.log
terraform import pgconfig_role_setting.app_role_pgaudit_log_app_db app_role/app_db/pgaudit.log
