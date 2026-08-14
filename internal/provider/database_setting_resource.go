package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/winebarrel/terraform-provider-pgconfig/internal/pgclient"
)

var (
	_ resource.Resource                = &DatabaseSettingResource{}
	_ resource.ResourceWithConfigure   = &DatabaseSettingResource{}
	_ resource.ResourceWithImportState = &DatabaseSettingResource{}
)

func NewDatabaseSettingResource() resource.Resource {
	return &DatabaseSettingResource{}
}

type DatabaseSettingResource struct {
	client *pgclient.Client
}

type DatabaseSettingResourceModel struct {
	Database types.String `tfsdk:"database"`
	Name     types.String `tfsdk:"name"`
	Value    types.String `tfsdk:"value"`
	Quote    types.Bool   `tfsdk:"quote"`
}

func (r *DatabaseSettingResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database_setting"
}

func (r *DatabaseSettingResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a single PostgreSQL database configuration parameter (`ALTER DATABASE ... SET`), " +
			"so it can coexist with other resources/providers managing other parameters on the same database.",
		Attributes: map[string]schema.Attribute{
			"database": schema.StringAttribute{
				Required:    true,
				Description: "The database to set the parameter on.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The configuration parameter name, e.g. `pgaudit.log` or `statement_timeout`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(paramNameRegexp, "must be a valid PostgreSQL configuration parameter name, e.g. `statement_timeout` or `pgaudit.log`"),
				},
			},
			"value": schema.StringAttribute{
				Required:    true,
				Description: "The parameter value.",
			},
			"quote": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				Description: "Whether to quote `value` as a SQL string literal. Set to `false` to embed `value` " +
					"verbatim, e.g. for parameters like `search_path` that expect an unquoted list. " +
					"When `false`, `value` is not escaped and is the caller's responsibility to sanitize.",
			},
		},
	}
}

func (r *DatabaseSettingResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*pgclient.Client)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *pgclient.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *DatabaseSettingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DatabaseSettingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	db, err := r.client.DB(ctx)

	if err != nil {
		resp.Diagnostics.AddError("Failed to connect to PostgreSQL", err.Error())
		return
	}

	sqlStr := buildDatabaseSetSQL(plan.Database.ValueString(), plan.Name.ValueString(), plan.Value.ValueString(), plan.Quote.ValueBool())
	lockKey := databaseLockKey(plan.Database.ValueString())

	if err := execWithAdvisoryLock(ctx, db, lockKey, sqlStr); err != nil {
		resp.Diagnostics.AddError("Failed to set database parameter", sqlErrorHint(err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DatabaseSettingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DatabaseSettingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	db, err := r.client.DB(ctx)

	if err != nil {
		resp.Diagnostics.AddError("Failed to connect to PostgreSQL", err.Error())
		return
	}

	database := state.Database.ValueString()
	name := state.Name.ValueString()

	const query = `
		SELECT entry
		FROM pg_db_role_setting s
		JOIN pg_database d ON d.oid = s.setdatabase
		CROSS JOIN LATERAL unnest(s.setconfig) AS entry
		WHERE d.datname = $1 AND s.setrole = 0
		  AND split_part(entry, '=', 1) = $2
	`

	entry, ok, err := findSettingEntry(ctx, db, query, database, name)

	if err != nil {
		resp.Diagnostics.AddError("Failed to read database parameter", sqlErrorHint(err))
		return
	}

	if !ok {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Value = types.StringValue(strings.TrimPrefix(entry, name+"="))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *DatabaseSettingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan DatabaseSettingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	db, err := r.client.DB(ctx)

	if err != nil {
		resp.Diagnostics.AddError("Failed to connect to PostgreSQL", err.Error())
		return
	}

	sqlStr := buildDatabaseSetSQL(plan.Database.ValueString(), plan.Name.ValueString(), plan.Value.ValueString(), plan.Quote.ValueBool())
	lockKey := databaseLockKey(plan.Database.ValueString())

	if err := execWithAdvisoryLock(ctx, db, lockKey, sqlStr); err != nil {
		resp.Diagnostics.AddError("Failed to set database parameter", sqlErrorHint(err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DatabaseSettingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DatabaseSettingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	db, err := r.client.DB(ctx)

	if err != nil {
		resp.Diagnostics.AddError("Failed to connect to PostgreSQL", err.Error())
		return
	}

	sqlStr := buildDatabaseResetSQL(state.Database.ValueString(), state.Name.ValueString())
	lockKey := databaseLockKey(state.Database.ValueString())

	if err := execWithAdvisoryLock(ctx, db, lockKey, sqlStr); err != nil {
		resp.Diagnostics.AddError("Failed to reset database parameter", sqlErrorHint(err))
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *DatabaseSettingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)

	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			`Expected import ID in the format "<database>/<name>", got: `+req.ID,
		)
		return
	}

	database, name := parts[0], parts[1]

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), database)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("quote"), true)...)
}
