package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hans-m-song/terraform-provider-awscontrib/internal/conns"
	connectservice "github.com/hans-m-song/terraform-provider-awscontrib/internal/service/connect"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var _ provider.Provider = &AWSContribProvider{}

type AWSContribProvider struct {
	version                string
	loadClient             func(context.Context, conns.Config) (*conns.Client, error)
	resourceConstructors   []func() resource.Resource
	dataSourceConstructors []func() datasource.DataSource
}

type AWSContribProviderModel struct {
	Profile     types.String `tfsdk:"profile"`
	Region      types.String `tfsdk:"region"`
	DefaultTags types.Object `tfsdk:"default_tags"`
	IgnoreTags  types.Object `tfsdk:"ignore_tags"`
}

type defaultTagsBlockModel struct {
	Tags types.Map `tfsdk:"tags"`
}

type ignoreTagsBlockModel struct {
	Keys        types.Set `tfsdk:"keys"`
	KeyPrefixes types.Set `tfsdk:"key_prefixes"`
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
		Blocks: map[string]schema.Block{
			"default_tags": schema.SingleNestedBlock{
				MarkdownDescription: "Default tags for supported resources. Currently, only `awscontrib_connect_data_table` consumes these settings.",
				Attributes: map[string]schema.Attribute{
					"tags": schema.MapAttribute{
						MarkdownDescription: "Map of default tag keys and values.",
						ElementType:         types.StringType,
						Optional:            true,
					},
				},
			},
			"ignore_tags": schema.SingleNestedBlock{
				MarkdownDescription: "Tags matching these keys or prefixes are omitted during refresh for supported resources. Currently, only `awscontrib_connect_data_table` consumes these settings. Do not also configure ignored keys in the resource's `tags`, because omitted refreshed values can produce persistent differences.",
				Attributes: map[string]schema.Attribute{
					"keys": schema.SetAttribute{
						MarkdownDescription: "Tag keys to omit during refresh.",
						ElementType:         types.StringType,
						Optional:            true,
					},
					"key_prefixes": schema.SetAttribute{
						MarkdownDescription: "Tag key prefixes to omit during refresh.",
						ElementType:         types.StringType,
						Optional:            true,
					},
				},
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
	tagConfig, tagDiagnostics := tagConfigFromProvider(ctx, data)
	resp.Diagnostics.Append(tagDiagnostics...)
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
	client.TagConfig = tagConfig

	resp.DataSourceData = client
	resp.ResourceData = client
}

func tagConfigFromProvider(ctx context.Context, data AWSContribProviderModel) (conns.TagConfig, diag.Diagnostics) {
	tagConfig := conns.TagConfig{DefaultTags: map[string]string{}}
	var diagnostics diag.Diagnostics

	if data.DefaultTags.IsUnknown() {
		diagnostics.AddError("Unknown Provider Configuration", "The `default_tags` block must be known before the AWS client can be configured.")
		return tagConfig, diagnostics
	}
	if !data.DefaultTags.IsNull() {
		var defaultTags defaultTagsBlockModel
		diagnostics.Append(data.DefaultTags.As(ctx, &defaultTags, basetypes.ObjectAsOptions{})...)
		if diagnostics.HasError() {
			return tagConfig, diagnostics
		}

		if defaultTags.Tags.IsUnknown() {
			diagnostics.AddError("Unknown Provider Configuration", "The `default_tags.tags` map must be known before the AWS client can be configured.")
			return tagConfig, diagnostics
		}
		if !defaultTags.Tags.IsNull() {
			elements := defaultTags.Tags.Elements()
			keys := make([]string, 0, len(elements))
			for key := range elements {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				value, ok := elements[key].(types.String)
				if !ok {
					diagnostics.AddError("Invalid Provider Configuration", fmt.Sprintf("The value for `default_tags.tags[%q]` must be a string.", key))
					continue
				}
				if value.IsUnknown() {
					diagnostics.AddError("Unknown Provider Configuration", fmt.Sprintf("The value for `default_tags.tags[%q]` must be known before the AWS client can be configured.", key))
					continue
				}
				if value.IsNull() {
					diagnostics.AddError("Invalid Provider Configuration", fmt.Sprintf("The value for `default_tags.tags[%q]` must not be null.", key))
					continue
				}
				tagConfig.DefaultTags[key] = value.ValueString()
			}
		}
	}

	if data.IgnoreTags.IsUnknown() {
		diagnostics.AddError("Unknown Provider Configuration", "The `ignore_tags` block must be known before the AWS client can be configured.")
		return tagConfig, diagnostics
	}
	if !data.IgnoreTags.IsNull() {
		var ignoreTags ignoreTagsBlockModel
		diagnostics.Append(data.IgnoreTags.As(ctx, &ignoreTags, basetypes.ObjectAsOptions{})...)
		if diagnostics.HasError() {
			return tagConfig, diagnostics
		}

		var setDiagnostics diag.Diagnostics
		tagConfig.IgnoreKeys, setDiagnostics = stringsFromSet(ignoreTags.Keys, "ignore_tags.keys")
		diagnostics.Append(setDiagnostics...)
		tagConfig.IgnoreKeyPrefixes, setDiagnostics = stringsFromSet(ignoreTags.KeyPrefixes, "ignore_tags.key_prefixes")
		diagnostics.Append(setDiagnostics...)
	}

	return tagConfig, diagnostics
}

func stringsFromSet(values types.Set, path string) ([]string, diag.Diagnostics) {
	if values.IsNull() {
		return nil, nil
	}
	if values.IsUnknown() {
		var diagnostics diag.Diagnostics
		diagnostics.AddError("Unknown Provider Configuration", fmt.Sprintf("The `%s` set must be known before the AWS client can be configured.", path))
		return nil, diagnostics
	}

	elements := values.Elements()
	result := make([]string, 0, len(elements))
	var diagnostics diag.Diagnostics
	for _, element := range elements {
		value, ok := element.(types.String)
		if !ok {
			diagnostics.AddError("Invalid Provider Configuration", fmt.Sprintf("Every value in `%s` must be a string.", path))
			continue
		}
		if value.IsUnknown() {
			diagnostics.AddError("Unknown Provider Configuration", fmt.Sprintf("Every value in `%s` must be known before the AWS client can be configured.", path))
			continue
		}
		if value.IsNull() {
			diagnostics.AddError("Invalid Provider Configuration", fmt.Sprintf("Values in `%s` must not be null.", path))
			continue
		}
		result = append(result, value.ValueString())
	}
	sort.Strings(result)

	return result, diagnostics
}

func (p *AWSContribProvider) Resources(context.Context) []func() resource.Resource {
	return p.resourceConstructors
}

func (p *AWSContribProvider) DataSources(context.Context) []func() datasource.DataSource {
	return p.dataSourceConstructors
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		dataTableResourceFactory, dataTableRecordResourceFactory := connectservice.DataTableResourceFactories()

		return &AWSContribProvider{
			version:    version,
			loadClient: conns.New,
			resourceConstructors: []func() resource.Resource{
				connectservice.QueueQuickConnectAssociationsResourceFactory(),
				connectservice.HoursOfOperationOverrideResourceFactory(),
				dataTableResourceFactory,
				dataTableRecordResourceFactory,
			},
			dataSourceConstructors: []func() datasource.DataSource{
				connectservice.PhoneNumberDataSourceFactory(),
				connectservice.ContactFlowModuleDataSourceFactory(),
				connectservice.DataTableDataSourceFactory(),
			},
		}
	}
}
