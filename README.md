# terraform-provider-pgconfig

[![CI](https://github.com/winebarrel/terraform-provider-pgconfig/actions/workflows/ci.yml/badge.svg)](https://github.com/winebarrel/terraform-provider-pgconfig/actions/workflows/ci.yml)
[![terraform docs](https://img.shields.io/badge/terraform-docs-%35835CC?logo=terraform)](https://registry.terraform.io/providers/winebarrel/pgconfig/latest/docs)
[![codecov](https://codecov.io/gh/winebarrel/terraform-provider-pgconfig/graph/badge.svg)](https://codecov.io/gh/winebarrel/terraform-provider-pgconfig)
[![AI Generated](https://img.shields.io/badge/AI%20Generated-Claude-orange?logo=anthropic)](https://claude.com/claude-code)

Terraform provider for managing PostgreSQL role/database configuration parameters
(`ALTER ROLE ... SET` / `ALTER DATABASE ... SET`), one parameter per resource.

The direct motivation is enabling [pgaudit](https://github.com/pgaudit/pgaudit) per role:

```sql
ALTER ROLE app_role SET pgaudit.log = 'all';
```

which isn't supported by [cyrilgdn/terraform-provider-postgresql](https://github.com/cyrilgdn/terraform-provider-postgresql)
([#210](https://github.com/cyrilgdn/terraform-provider-postgresql/issues/210),
[#390](https://github.com/cyrilgdn/terraform-provider-postgresql/issues/390),
[#634](https://github.com/cyrilgdn/terraform-provider-postgresql/issues/634)).
This provider isn't pgaudit-specific though — it works for any GUC, e.g. `statement_timeout`.

Each `pgconfig_role_setting` / `pgconfig_database_setting` resource manages a single
parameter key, so it can coexist with `cyrilgdn/postgresql` (or any other tool) managing
the same role/database, and drift (e.g. a manual `ALTER ROLE ... RESET`) is detected on
the next plan.

## Usage

```tf
terraform {
  required_providers {
    pgconfig = {
      source  = "winebarrel/pgconfig"
      version = ">= 0.1"
    }
  }
}

provider "pgconfig" {
  host     = "localhost"
  port     = 5432
  database = "postgres"
  username = "postgres"
  password = "postgres"
  sslmode  = "disable"
}

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

resource "pgconfig_database_setting" "pgaudit_log" {
  database = "app_db"
  name     = "pgaudit.log"
  value    = "all,-write,-read,-misc"
}
```

Set `quote = false` for values that must be embedded unquoted, such as `search_path`:

```tf
resource "pgconfig_role_setting" "search_path" {
  role  = "app_role"
  name  = "search_path"
  value = "\"$user\", public"
  quote = false
}
```

See [docs/](docs/) for the full provider/resource schema, and
[terraform-provider-lambdazip](https://github.com/winebarrel/terraform-provider-lambdazip)
for the general provider layout this repo follows.

## Run locally for development

```sh
cp examples/provider/provider.tf pgconfig.tf
make
make tf-plan
make tf-apply
```

## Testing

Acceptance tests run against a real PostgreSQL server (with the `pgaudit` extension
loaded, since the primary use case for this provider is managing `pgaudit.log`):

```sh
make docker-up
make testacc
make docker-down
```
