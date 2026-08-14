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
	_ resource.Resource                = &RoleSettingResource{}
	_ resource.ResourceWithConfigure   = &RoleSettingResource{}
	_ resource.ResourceWithImportState = &RoleSettingResource{}
)

func NewRoleSettingResource() resource.Resource {
	return &RoleSettingResource{}
}

type RoleSettingResource struct {
	client *pgclient.Client
}

type RoleSettingResourceModel struct {
	Role     types.String `tfsdk:"role"`
	Database types.String `tfsdk:"database"`
	Name     types.String `tfsdk:"name"`
	Value    types.String `tfsdk:"value"`
	Quote    types.Bool   `tfsdk:"quote"`
}

func (r *RoleSettingResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_setting"
}

func (r *RoleSettingResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a single PostgreSQL role configuration parameter " +
			"(`ALTER ROLE ... SET` / `ALTER ROLE ... IN DATABASE ... SET`), so it can coexist with other " +
			"resources/providers managing other parameters on the same role.",
		Attributes: map[string]schema.Attribute{
			"role": schema.StringAttribute{
				Required:    true,
				Description: "The role to set the parameter on.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"database": schema.StringAttribute{
				Optional: true,
				Description: "Restrict the setting to this database (`ALTER ROLE ... IN DATABASE ...`). " +
					"When omitted, the setting applies cluster-wide.",
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

func (r *RoleSettingResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *RoleSettingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RoleSettingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	db, err := r.client.DB(ctx)

	if err != nil {
		resp.Diagnostics.AddError("Failed to connect to PostgreSQL", err.Error())
		return
	}

	sqlStr := buildRoleSetSQL(plan.Role.ValueString(), plan.Database.ValueString(), plan.Name.ValueString(), plan.Value.ValueString(), plan.Quote.ValueBool())
	lockKey := roleLockKey(plan.Role.ValueString(), plan.Database.ValueString())

	if err := execWithAdvisoryLock(ctx, db, lockKey, sqlStr); err != nil {
		resp.Diagnostics.AddError("Failed to set role parameter", sqlErrorHint(err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *RoleSettingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RoleSettingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	db, err := r.client.DB(ctx)

	if err != nil {
		resp.Diagnostics.AddError("Failed to connect to PostgreSQL", err.Error())
		return
	}

	role := state.Role.ValueString()
	database := state.Database.ValueString()
	name := state.Name.ValueString()

	const query = `
		SELECT entry
		FROM pg_db_role_setting s
		JOIN pg_roles r ON r.oid = s.setrole
		CROSS JOIN LATERAL unnest(s.setconfig) AS entry
		WHERE r.rolname = $1
		  AND s.setdatabase = CASE
		        WHEN $2 = '' THEN 0
		        ELSE (SELECT oid FROM pg_database WHERE datname = $2)
		      END
		  AND split_part(entry, '=', 1) = $3
	`

	entry, ok, err := findSettingEntry(ctx, db, query, role, database, name)

	if err != nil {
		resp.Diagnostics.AddError("Failed to read role parameter", sqlErrorHint(err))
		return
	}

	if !ok {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Value = types.StringValue(strings.TrimPrefix(entry, name+"="))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RoleSettingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan RoleSettingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	db, err := r.client.DB(ctx)

	if err != nil {
		resp.Diagnostics.AddError("Failed to connect to PostgreSQL", err.Error())
		return
	}

	sqlStr := buildRoleSetSQL(plan.Role.ValueString(), plan.Database.ValueString(), plan.Name.ValueString(), plan.Value.ValueString(), plan.Quote.ValueBool())
	lockKey := roleLockKey(plan.Role.ValueString(), plan.Database.ValueString())

	if err := execWithAdvisoryLock(ctx, db, lockKey, sqlStr); err != nil {
		resp.Diagnostics.AddError("Failed to set role parameter", sqlErrorHint(err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *RoleSettingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RoleSettingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	db, err := r.client.DB(ctx)

	if err != nil {
		resp.Diagnostics.AddError("Failed to connect to PostgreSQL", err.Error())
		return
	}

	sqlStr := buildRoleResetSQL(state.Role.ValueString(), state.Database.ValueString(), state.Name.ValueString())
	lockKey := roleLockKey(state.Role.ValueString(), state.Database.ValueString())

	if err := execWithAdvisoryLock(ctx, db, lockKey, sqlStr); err != nil {
		resp.Diagnostics.AddError("Failed to reset role parameter", sqlErrorHint(err))
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *RoleSettingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 3)

	if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			`Expected import ID in the format "<role>/<database>/<name>" `+
				`(database may be empty for a cluster-wide setting, e.g. "myrole//pgaudit.log"), got: `+req.ID,
		)
		return
	}

	role, database, name := parts[0], parts[1], parts[2]

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role"), role)...)

	// Leave "database" null (rather than "") for a cluster-wide setting, to
	// match the state produced by a config that omits the optional
	// attribute entirely.
	if database != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), database)...)
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("quote"), true)...)
}
