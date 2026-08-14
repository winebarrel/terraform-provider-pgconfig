package provider

import (
	"context"
	"os"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/winebarrel/terraform-provider-pgconfig/internal/pgclient"
)

var _ provider.Provider = &PgconfigProvider{}

type PgconfigProvider struct {
	version string
}

type clientCertificateModel struct {
	Cert      types.String `tfsdk:"cert"`
	Key       types.String `tfsdk:"key"`
	SSLInline types.Bool   `tfsdk:"sslinline"`
}

type PgconfigProviderModel struct {
	Scheme                        types.String `tfsdk:"scheme"`
	Host                          types.String `tfsdk:"host"`
	Port                          types.Int64  `tfsdk:"port"`
	Database                      types.String `tfsdk:"database"`
	Username                      types.String `tfsdk:"username"`
	Password                      types.String `tfsdk:"password"`
	SSLMode                       types.String `tfsdk:"sslmode"`
	SSLRootCert                   types.String `tfsdk:"sslrootcert"`
	ClientCert                    types.Object `tfsdk:"clientcert"`
	ConnectTimeout                types.Int64  `tfsdk:"connect_timeout"`
	MaxConnRetries                types.Int64  `tfsdk:"max_conn_retries"`
	ConnectionRetryTimeoutSeconds types.Int64  `tfsdk:"connection_retry_timeout_seconds"`
	MaxConnections                types.Int64  `tfsdk:"max_connections"`
	ConnMaxLifetimeSeconds        types.Int64  `tfsdk:"conn_max_lifetime_seconds"`
	AWSRDSIAMAuth                 types.Bool   `tfsdk:"aws_rds_iam_auth"`
	AWSRDSIAMProfile              types.String `tfsdk:"aws_rds_iam_profile"`
	AWSRDSIAMRegion               types.String `tfsdk:"aws_rds_iam_region"`
	AWSRDSIAMProviderRoleARN      types.String `tfsdk:"aws_rds_iam_provider_role_arn"`
}

func (p *PgconfigProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "pgconfig"
	resp.Version = p.version
}

func (p *PgconfigProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages PostgreSQL role/database configuration parameters " +
			"(`ALTER ROLE ... SET` / `ALTER DATABASE ... SET`) as individual keys, " +
			"so it can coexist with other providers that manage the same role/database.",
		Attributes: map[string]schema.Attribute{
			"scheme": schema.StringAttribute{
				Optional:    true,
				Description: "The driver to use. One of `postgres`, `awspostgres` or `gcppostgres`. Defaults to `postgres`.",
				Validators: []validator.String{
					stringvalidator.OneOf("postgres", "awspostgres", "gcppostgres"),
				},
			},
			"host": schema.StringAttribute{
				Optional:    true,
				Description: "Name of PostgreSQL server address to connect to. Defaults to the `PGHOST` environment variable.",
			},
			"port": schema.Int64Attribute{
				Optional:    true,
				Description: "The PostgreSQL port number to connect to. Defaults to the `PGPORT` environment variable, or `5432`.",
			},
			"database": schema.StringAttribute{
				Optional:    true,
				Description: "The database to connect to. Defaults to the `PGDATABASE` environment variable, or `postgres`.",
			},
			"username": schema.StringAttribute{
				Optional:    true,
				Description: "PostgreSQL user name to connect as. Defaults to the `PGUSER` environment variable.",
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Password for the PostgreSQL user. Defaults to the `PGPASSWORD` environment variable.",
			},
			"sslmode": schema.StringAttribute{
				Optional: true,
				Description: "How to handle SSL connections. One of `disable`, `require`, `verify-ca` or `verify-full`. " +
					"Defaults to the `PGSSLMODE` environment variable, or `require`.",
			},
			"sslrootcert": schema.StringAttribute{
				Optional:    true,
				Description: "The SSL server root certificate file path. The file must contain PEM encoded data.",
			},
			"clientcert": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "SSL client certificate, used for both providing the connection and authenticating.",
				Attributes: map[string]schema.Attribute{
					"cert": schema.StringAttribute{
						Required:    true,
						Description: "The SSL client certificate file path. The file must contain PEM encoded data.",
					},
					"key": schema.StringAttribute{
						Required:    true,
						Sensitive:   true,
						Description: "The SSL client certificate private key file path. The file must contain PEM encoded data.",
					},
					"sslinline": schema.BoolAttribute{
						Optional:    true,
						Description: "Must be set to true if you are inlining the cert/key instead of using a file path.",
					},
				},
			},
			"connect_timeout": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum wait, in seconds, for a connection to become available before returning an error. Defaults to `180`.",
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"max_conn_retries": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum number of connection retries on failure. Defaults to `0`.",
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"connection_retry_timeout_seconds": schema.Int64Attribute{
				Optional:    true,
				Description: "Total timeout, in seconds, for retrying a failed connection. Defaults to `5`.",
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"max_connections": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum number of connections to establish to the database. Zero means unlimited. Defaults to `20`.",
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"conn_max_lifetime_seconds": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum amount of time, in seconds, a connection may be reused. Zero means unlimited. Defaults to `0`.",
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"aws_rds_iam_auth": schema.BoolAttribute{
				Optional: true,
				Description: "Use RDS IAM auth instead of password authentication " +
					"(see: https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.IAMDBAuth.html).",
			},
			"aws_rds_iam_profile": schema.StringAttribute{
				Optional:    true,
				Description: "AWS profile to use for IAM auth.",
			},
			"aws_rds_iam_region": schema.StringAttribute{
				Optional:    true,
				Description: "AWS region to use for IAM auth.",
			},
			"aws_rds_iam_provider_role_arn": schema.StringAttribute{
				Optional:    true,
				Description: "AWS IAM role to assume for IAM auth.",
			},
		},
	}
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}

	return def
}

func envInt64Or(key string, def int64) int64 {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}

	return def
}

func stringOrEnv(v types.String, key, def string) string {
	if !v.IsNull() && !v.IsUnknown() {
		return v.ValueString()
	}

	return envOr(key, def)
}

func int64OrEnv(v types.Int64, key string, def int64) int64 {
	if !v.IsNull() && !v.IsUnknown() {
		return v.ValueInt64()
	}

	return envInt64Or(key, def)
}

func (p *PgconfigProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data PgconfigProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	scheme := data.Scheme.ValueString()

	if scheme == "" {
		scheme = "postgres"
	}

	host := stringOrEnv(data.Host, "PGHOST", "")

	if host == "" {
		resp.Diagnostics.AddError(
			"Missing PostgreSQL Host",
			"host must be set in the provider configuration or the PGHOST environment variable.",
		)
		return
	}

	username := stringOrEnv(data.Username, "PGUSER", "")

	if username == "" {
		resp.Diagnostics.AddError(
			"Missing PostgreSQL Username",
			"username must be set in the provider configuration or the PGUSER environment variable.",
		)
		return
	}

	port := int64OrEnv(data.Port, "PGPORT", 5432)
	database := stringOrEnv(data.Database, "PGDATABASE", "postgres")
	sslMode := stringOrEnv(data.SSLMode, "PGSSLMODE", "require")

	config := pgclient.Config{
		Scheme:                        scheme,
		Host:                          host,
		Port:                          port,
		Database:                      database,
		Username:                      username,
		SSLMode:                       sslMode,
		SSLRootCertPath:               data.SSLRootCert.ValueString(),
		ConnectTimeoutSec:             valueInt64Default(data.ConnectTimeout, 180),
		MaxConnRetries:                valueInt64Default(data.MaxConnRetries, 0),
		ConnectionRetryTimeoutSeconds: valueInt64Default(data.ConnectionRetryTimeoutSeconds, 5),
		MaxConns:                      valueInt64Default(data.MaxConnections, 20),
		ConnMaxLifetimeSeconds:        valueInt64Default(data.ConnMaxLifetimeSeconds, 0),
	}

	if !data.ClientCert.IsNull() && !data.ClientCert.IsUnknown() {
		var cert clientCertificateModel
		resp.Diagnostics.Append(data.ClientCert.As(ctx, &cert, basetypes.ObjectAsOptions{})...)

		if resp.Diagnostics.HasError() {
			return
		}

		config.SSLClientCert = &pgclient.ClientCertificateConfig{
			CertificatePath: cert.Cert.ValueString(),
			KeyPath:         cert.Key.ValueString(),
			SSLInline:       cert.SSLInline.ValueBool(),
		}
	}

	if data.AWSRDSIAMAuth.ValueBool() {
		token, err := pgclient.GetRDSAuthToken(
			ctx,
			data.AWSRDSIAMRegion.ValueString(),
			data.AWSRDSIAMProfile.ValueString(),
			data.AWSRDSIAMProviderRoleARN.ValueString(),
			username,
			host,
			port,
		)

		if err != nil {
			resp.Diagnostics.AddError("Failed to build RDS IAM auth token", err.Error())
			return
		}

		config.Password = token
	} else {
		config.Password = stringOrEnv(data.Password, "PGPASSWORD", "")
	}

	client := pgclient.NewClient(config)

	resp.DataSourceData = client
	resp.ResourceData = client
}

func valueInt64Default(v types.Int64, def int64) int64 {
	if v.IsNull() || v.IsUnknown() {
		return def
	}

	return v.ValueInt64()
}

func (p *PgconfigProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewRoleSettingResource,
		NewDatabaseSettingResource,
	}
}

func (p *PgconfigProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &PgconfigProvider{
			version: version,
		}
	}
}
