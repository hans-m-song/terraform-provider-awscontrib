package connect

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	contactFlowModuleSearchNameMinLength = 2
	contactFlowModuleSearchNameMaxLength = 25
)

var _ datasource.DataSource = &contactFlowModuleDataSource{}
var _ datasource.DataSourceWithConfigure = &contactFlowModuleDataSource{}

type contactFlowModuleClient interface {
	SearchContactFlowModules(context.Context, *awsconnect.SearchContactFlowModulesInput, ...func(*awsconnect.Options)) (*awsconnect.SearchContactFlowModulesOutput, error)
}

type contactFlowModuleDataSource struct {
	client contactFlowModuleClient
}

type contactFlowModuleDataSourceModel struct {
	InstanceID         types.String `tfsdk:"instance_id"`
	Name               types.String `tfsdk:"name"`
	ID                 types.String `tfsdk:"id"`
	ARN                types.String `tfsdk:"arn"`
	Description        types.String `tfsdk:"description"`
	Content            types.String `tfsdk:"content"`
	ContentSHA256      types.String `tfsdk:"content_sha256"`
	Settings           types.String `tfsdk:"settings"`
	State              types.String `tfsdk:"state"`
	Status             types.String `tfsdk:"status"`
	Version            types.Int64  `tfsdk:"version"`
	VersionDescription types.String `tfsdk:"version_description"`
	Tags               types.Map    `tfsdk:"tags"`
}

func NewContactFlowModuleDataSource() datasource.DataSource {
	return &contactFlowModuleDataSource{}
}

func ContactFlowModuleDataSourceFactory() func() datasource.DataSource {
	return func() datasource.DataSource {
		return NewContactFlowModuleDataSource()
	}
}

func (d *contactFlowModuleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_contact_flow_module"
}

func (d *contactFlowModuleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		MarkdownDescription: "Looks up an Amazon Connect contact flow module by exact name.",
		Attributes: map[string]datasourceschema.Attribute{
			"instance_id": datasourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
			},
			"name": datasourceschema.StringAttribute{
				MarkdownDescription: "Exact contact flow module name.",
				Required:            true,
			},
			"id": datasourceschema.StringAttribute{
				MarkdownDescription: "Contact flow module identifier.",
				Computed:            true,
			},
			"arn": datasourceschema.StringAttribute{
				MarkdownDescription: "Contact flow module Amazon Resource Name.",
				Computed:            true,
			},
			"description": datasourceschema.StringAttribute{
				MarkdownDescription: "Contact flow module description.",
				Computed:            true,
			},
			"content": datasourceschema.StringAttribute{
				MarkdownDescription: "JSON content of the contact flow module.",
				Computed:            true,
			},
			"content_sha256": datasourceschema.StringAttribute{
				MarkdownDescription: "SHA-256 hash of the contact flow module content.",
				Computed:            true,
			},
			"settings": datasourceschema.StringAttribute{
				MarkdownDescription: "Contact flow module configuration settings.",
				Computed:            true,
			},
			"state": datasourceschema.StringAttribute{
				MarkdownDescription: "Contact flow module state.",
				Computed:            true,
			},
			"status": datasourceschema.StringAttribute{
				MarkdownDescription: "Contact flow module status.",
				Computed:            true,
			},
			"version": datasourceschema.Int64Attribute{
				MarkdownDescription: "Contact flow module version.",
				Computed:            true,
			},
			"version_description": datasourceschema.StringAttribute{
				MarkdownDescription: "Description of the contact flow module version.",
				Computed:            true,
			},
			"tags": datasourceschema.MapAttribute{
				MarkdownDescription: "Tags associated with the contact flow module.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

func (d *contactFlowModuleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	factory, ok := req.ProviderData.(clientFactory)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected an Amazon Connect client factory, got %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = factory.Connect()
}

func (d *contactFlowModuleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data contactFlowModuleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}
	if data.InstanceID.IsNull() || data.InstanceID.IsUnknown() || data.Name.IsNull() || data.Name.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Contact Flow Module Lookup Not Known",
			"instance_id and name must be known before the contact-flow-module lookup can run",
		)
		return
	}

	modules, err := d.searchContactFlowModules(ctx, data.InstanceID.ValueString(), data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Unable to Search Contact Flow Modules",
			fmt.Sprintf("Could not search contact flow modules in instance %q: %s", data.InstanceID.ValueString(), err),
		)
		return
	}

	matchingModules := make([]connecttypes.ContactFlowModule, 0, len(modules))
	for _, module := range modules {
		if aws.ToString(module.Name) == data.Name.ValueString() {
			matchingModules = append(matchingModules, module)
		}
	}

	switch len(matchingModules) {
	case 0:
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Contact Flow Module Not Found",
			fmt.Sprintf("No contact flow module named %q was found in instance %q.", data.Name.ValueString(), data.InstanceID.ValueString()),
		)
		return
	case 1:
		resp.Diagnostics.Append(setContactFlowModuleData(ctx, &data, matchingModules[0])...)
		if resp.Diagnostics.HasError() {
			return
		}
	default:
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Multiple Contact Flow Modules Found",
			fmt.Sprintf("Found %d contact flow modules named %q in instance %q; the name must identify exactly one module.", len(matchingModules), data.Name.ValueString(), data.InstanceID.ValueString()),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *contactFlowModuleDataSource) searchContactFlowModules(ctx context.Context, instanceID, name string) ([]connecttypes.ContactFlowModule, error) {
	input := &awsconnect.SearchContactFlowModulesInput{
		InstanceId:     aws.String(instanceID),
		SearchCriteria: contactFlowModuleSearchCriteria(name),
	}

	var modules []connecttypes.ContactFlowModule
	var nextToken *string
	for {
		input.NextToken = nextToken
		output, err := d.client.SearchContactFlowModules(ctx, input)
		if err != nil {
			return nil, err
		}
		if output == nil {
			return nil, fmt.Errorf("SearchContactFlowModules returned a nil response")
		}

		modules = append(modules, output.ContactFlowModules...)
		if output.NextToken == nil || aws.ToString(output.NextToken) == "" {
			return modules, nil
		}
		if nextToken != nil && aws.ToString(nextToken) == aws.ToString(output.NextToken) {
			return nil, fmt.Errorf("amazon connect returned duplicate contact-flow-module pagination token %q", aws.ToString(output.NextToken))
		}
		nextToken = output.NextToken
	}
}

func contactFlowModuleSearchCriteria(name string) *connecttypes.ContactFlowModuleSearchCriteria {
	nameLength := utf8.RuneCountInString(name)
	if nameLength < contactFlowModuleSearchNameMinLength || nameLength > contactFlowModuleSearchNameMaxLength {
		return nil
	}

	return &connecttypes.ContactFlowModuleSearchCriteria{
		StringCondition: &connecttypes.StringCondition{
			ComparisonType: connecttypes.StringComparisonTypeContains,
			FieldName:      aws.String("name"),
			Value:          aws.String(name),
		},
	}
}

func setContactFlowModuleData(ctx context.Context, data *contactFlowModuleDataSourceModel, module connecttypes.ContactFlowModule) diag.Diagnostics {
	data.ID = contactFlowModuleStringValue(module.Id)
	data.ARN = contactFlowModuleStringValue(module.Arn)
	data.Description = contactFlowModuleStringValue(module.Description)
	data.Content = contactFlowModuleStringValue(module.Content)
	data.ContentSHA256 = contactFlowModuleStringValue(module.FlowModuleContentSha256)
	data.Settings = contactFlowModuleStringValue(module.Settings)
	data.State = contactFlowModuleEnumStringValue(string(module.State))
	data.Status = contactFlowModuleEnumStringValue(string(module.Status))
	data.Version = contactFlowModuleInt64Value(module.Version)
	data.VersionDescription = contactFlowModuleStringValue(module.VersionDescription)
	if module.Tags == nil {
		data.Tags = types.MapNull(types.StringType)
		return nil
	}

	var diagnostics diag.Diagnostics
	data.Tags, diagnostics = types.MapValueFrom(ctx, types.StringType, module.Tags)
	return diagnostics
}

func contactFlowModuleStringValue(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func contactFlowModuleEnumStringValue(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

func contactFlowModuleInt64Value(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*value)
}
