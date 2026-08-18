package provider

import (
	"context"

	"github.com/hans-m-song/terraform-provider-awscontrib/internal/conns"
	connectservice "github.com/hans-m-song/terraform-provider-awscontrib/internal/service/connect"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &AWSContribProvider{}

type AWSContribProvider struct {
	version              string
	loadClient           func(context.Context, conns.Config) (*conns.Client, error)
	resourceConstructors []func() resource.Resource
}

type AWSContribProviderModel struct {
	Profile types.String `tfsdk:"profile"`
	Region  types.String `tfsdk:"region"`
}

func (p *AWSContribProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "awscontrib"
	resp.Version = p.version
}

func (p *AWSContribProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A focused provider for AWS capabilities that are not available in the HashiCorp AWS or AWS Cloud Control providers.",
		Attributes: map[string]schema.Attribute{
			"profile": schema.StringAttribute{
				MarkdownDescription: "AWS shared configuration profile to use.",
				Optional:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "AWS Region to use. When omitted, the AWS SDK resolves the Region from its default configuration chain.",
				Optional:            true,
			},
		},
	}
}

func (p *AWSContribProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data AWSContribProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config := conns.Config{}
	if !data.Profile.IsNull() && !data.Profile.IsUnknown() {
		config.Profile = data.Profile.ValueString()
	}
	if !data.Region.IsNull() && !data.Region.IsUnknown() {
		config.Region = data.Region.ValueString()
	}

	client, err := p.loadClient(ctx, config)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Configure AWS Client", "The AWS SDK configuration could not be loaded: "+err.Error())
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *AWSContribProvider) Resources(context.Context) []func() resource.Resource {
	return p.resourceConstructors
}

func (p *AWSContribProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &AWSContribProvider{
			version:    version,
			loadClient: conns.New,
			resourceConstructors: []func() resource.Resource{
				connectservice.QueueQuickConnectAssociationResourceFactory(),
			},
		}
	}
}
