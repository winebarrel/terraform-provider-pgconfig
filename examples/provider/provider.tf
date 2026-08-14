terraform {
  required_providers {
    pgconfig = {
      source = "winebarrel/pgconfig"
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
