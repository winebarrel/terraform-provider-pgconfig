package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestEnvOr(t *testing.T) {
	t.Setenv("PGCONFIG_TEST_ENVOR", "from-env")

	if got := envOr("PGCONFIG_TEST_ENVOR", "default"); got != "from-env" {
		t.Errorf("envOr(set) = %q, want %q", got, "from-env")
	}

	if got := envOr("PGCONFIG_TEST_ENVOR_UNSET", "default"); got != "default" {
		t.Errorf("envOr(unset) = %q, want %q", got, "default")
	}
}

func TestEnvInt64Or(t *testing.T) {
	t.Run("valid int", func(t *testing.T) {
		t.Setenv("PGCONFIG_TEST_ENVINT", "42")

		if got := envInt64Or("PGCONFIG_TEST_ENVINT", 7); got != 42 {
			t.Errorf("envInt64Or(valid) = %d, want 42", got)
		}
	})

	t.Run("unparsable falls back to default", func(t *testing.T) {
		t.Setenv("PGCONFIG_TEST_ENVINT_BAD", "not-a-number")

		if got := envInt64Or("PGCONFIG_TEST_ENVINT_BAD", 7); got != 7 {
			t.Errorf("envInt64Or(unparsable) = %d, want 7", got)
		}
	})

	t.Run("unset falls back to default", func(t *testing.T) {
		if got := envInt64Or("PGCONFIG_TEST_ENVINT_UNSET", 7); got != 7 {
			t.Errorf("envInt64Or(unset) = %d, want 7", got)
		}
	})
}

func TestStringOrEnv(t *testing.T) {
	t.Run("explicit value wins over env", func(t *testing.T) {
		t.Setenv("PGCONFIG_TEST_STRINGOR", "from-env")

		if got := stringOrEnv(types.StringValue("explicit"), "PGCONFIG_TEST_STRINGOR", "default"); got != "explicit" {
			t.Errorf("stringOrEnv(explicit) = %q, want %q", got, "explicit")
		}
	})

	t.Run("null falls back to env", func(t *testing.T) {
		t.Setenv("PGCONFIG_TEST_STRINGOR", "from-env")

		if got := stringOrEnv(types.StringNull(), "PGCONFIG_TEST_STRINGOR", "default"); got != "from-env" {
			t.Errorf("stringOrEnv(null) = %q, want %q", got, "from-env")
		}
	})

	t.Run("unknown falls back to env", func(t *testing.T) {
		t.Setenv("PGCONFIG_TEST_STRINGOR", "from-env")

		if got := stringOrEnv(types.StringUnknown(), "PGCONFIG_TEST_STRINGOR", "default"); got != "from-env" {
			t.Errorf("stringOrEnv(unknown) = %q, want %q", got, "from-env")
		}
	})
}

func TestInt64OrEnv(t *testing.T) {
	t.Run("explicit value wins over env", func(t *testing.T) {
		t.Setenv("PGCONFIG_TEST_INT64OR", "99")

		if got := int64OrEnv(types.Int64Value(5), "PGCONFIG_TEST_INT64OR", 7); got != 5 {
			t.Errorf("int64OrEnv(explicit) = %d, want 5", got)
		}
	})

	t.Run("null falls back to env", func(t *testing.T) {
		t.Setenv("PGCONFIG_TEST_INT64OR", "99")

		if got := int64OrEnv(types.Int64Null(), "PGCONFIG_TEST_INT64OR", 7); got != 99 {
			t.Errorf("int64OrEnv(null) = %d, want 99", got)
		}
	})

	t.Run("unknown falls back to env", func(t *testing.T) {
		t.Setenv("PGCONFIG_TEST_INT64OR", "99")

		if got := int64OrEnv(types.Int64Unknown(), "PGCONFIG_TEST_INT64OR", 7); got != 99 {
			t.Errorf("int64OrEnv(unknown) = %d, want 99", got)
		}
	})
}

func TestValueInt64Default(t *testing.T) {
	if got := valueInt64Default(types.Int64Value(9), 1); got != 9 {
		t.Errorf("valueInt64Default(set) = %d, want 9", got)
	}

	if got := valueInt64Default(types.Int64Null(), 1); got != 1 {
		t.Errorf("valueInt64Default(null) = %d, want 1", got)
	}

	if got := valueInt64Default(types.Int64Unknown(), 1); got != 1 {
		t.Errorf("valueInt64Default(unknown) = %d, want 1", got)
	}
}
