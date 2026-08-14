package provider_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/winebarrel/terraform-provider-pgconfig/internal/provider"
)

func TestRoleSettingResource_Configure(t *testing.T) {
	t.Run("nil ProviderData is a no-op", func(t *testing.T) {
		r := provider.NewRoleSettingResource().(resource.ResourceWithConfigure)
		resp := &resource.ConfigureResponse{}
		r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: nil}, resp)

		if resp.Diagnostics.HasError() {
			t.Errorf("unexpected error: %v", resp.Diagnostics)
		}
	})

	t.Run("wrong ProviderData type is an error", func(t *testing.T) {
		r := provider.NewRoleSettingResource().(resource.ResourceWithConfigure)
		resp := &resource.ConfigureResponse{}
		r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "not-a-client"}, resp)

		if !resp.Diagnostics.HasError() {
			t.Error("expected an error for wrong ProviderData type")
		}
	})
}

func TestDatabaseSettingResource_Configure(t *testing.T) {
	t.Run("nil ProviderData is a no-op", func(t *testing.T) {
		r := provider.NewDatabaseSettingResource().(resource.ResourceWithConfigure)
		resp := &resource.ConfigureResponse{}
		r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: nil}, resp)

		if resp.Diagnostics.HasError() {
			t.Errorf("unexpected error: %v", resp.Diagnostics)
		}
	})

	t.Run("wrong ProviderData type is an error", func(t *testing.T) {
		r := provider.NewDatabaseSettingResource().(resource.ResourceWithConfigure)
		resp := &resource.ConfigureResponse{}
		r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: 42}, resp)

		if !resp.Diagnostics.HasError() {
			t.Error("expected an error for wrong ProviderData type")
		}
	})
}

func TestRoleSettingResource_ImportState_invalidID(t *testing.T) {
	ids := []string{
		"",
		"role-only",
		"role/database",
		"/database/name",
		"role/database/",
	}

	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			r := provider.NewRoleSettingResource().(resource.ResourceWithImportState)
			resp := &resource.ImportStateResponse{}
			r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, resp)

			if !resp.Diagnostics.HasError() {
				t.Errorf("expected error for invalid import ID %q", id)
			}
		})
	}
}

func TestDatabaseSettingResource_ImportState_invalidID(t *testing.T) {
	ids := []string{
		"",
		"database-only",
		"/name",
		"database/",
	}

	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			r := provider.NewDatabaseSettingResource().(resource.ResourceWithImportState)
			resp := &resource.ImportStateResponse{}
			r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, resp)

			if !resp.Diagnostics.HasError() {
				t.Errorf("expected error for invalid import ID %q", id)
			}
		})
	}
}
