package connect

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const maxDataTablesPerPage = 1000

var _ datasource.DataSource = &dataTableDataSource{}
var _ datasource.DataSourceWithConfigure = &dataTableDataSource{}
var _ datasource.DataSourceWithValidateConfig = &dataTableDataSource{}

type dataTableLookupClient interface {
	DescribeDataTable(context.Context, *awsconnect.DescribeDataTableInput, ...func(*awsconnect.Options)) (*awsconnect.DescribeDataTableOutput, error)
	SearchDataTables(context.Context, *awsconnect.SearchDataTablesInput, ...func(*awsconnect.Options)) (*awsconnect.SearchDataTablesOutput, error)
}

type dataTableDataSource struct {
	client dataTableLookupClient
}

type dataTableDataSourceModel struct {
	InstanceID     types.String `tfsdk:"instance_id"`
	DataTableID    types.String `tfsdk:"data_table_id"`
	Name           types.String `tfsdk:"name"`
	ID             types.String `tfsdk:"id"`
	ARN            types.String `tfsdk:"arn"`
	Description    types.String `tfsdk:"description"`
	TimeZone       types.String `tfsdk:"time_zone"`
	Status         types.String `tfsdk:"status"`
	ValueLockLevel types.String `tfsdk:"value_lock_level"`
	Tags           types.Map    `tfsdk:"tags"`
}

func NewDataTableDataSource() datasource.DataSource {
	return &dataTableDataSource{}
}

func DataTableDataSourceFactory() func() datasource.DataSource {
	return func() datasource.DataSource {
		return NewDataTableDataSource()
	}
}

func (d *dataTableDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_data_table"
}

func (d *dataTableDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		MarkdownDescription: "Looks up one Amazon Connect data table by ID or exact name within an instance.",
		Attributes: map[string]datasourceschema.Attribute{
			"instance_id": datasourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
			},
			"data_table_id": datasourceschema.StringAttribute{
				MarkdownDescription: "Data-table identifier. Specify exactly one of data_table_id or name.",
				Optional:            true,
			},
			"name": datasourceschema.StringAttribute{
				MarkdownDescription: "Exact data-table name. Specify exactly one of data_table_id or name; the remote name is returned for ID lookups.",
				Optional:            true,
				Computed:            true,
			},
			"id": datasourceschema.StringAttribute{
				MarkdownDescription: "Data-table identifier.",
				Computed:            true,
			},
			"arn": datasourceschema.StringAttribute{
				MarkdownDescription: "Data-table ARN.",
				Computed:            true,
			},
			"description": datasourceschema.StringAttribute{
				MarkdownDescription: "Data-table description, when set.",
				Computed:            true,
			},
			"time_zone": datasourceschema.StringAttribute{
				MarkdownDescription: "IANA time-zone identifier used by the data table.",
				Computed:            true,
			},
			"status": datasourceschema.StringAttribute{
				MarkdownDescription: "Data-table status.",
				Computed:            true,
			},
			"value_lock_level": datasourceschema.StringAttribute{
				MarkdownDescription: "Value concurrency lock level.",
				Computed:            true,
			},
			"tags": datasourceschema.MapAttribute{
				MarkdownDescription: "Data-table tags returned by Amazon Connect.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

func (d *dataTableDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var data dataTableDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.DataTableID.IsUnknown() || data.Name.IsUnknown() {
		return
	}
	if data.DataTableID.IsNull() == data.Name.IsNull() {
		resp.Diagnostics.AddError("Invalid Data Table Lookup", "Specify exactly one of data_table_id or name.")
	}
}

func (d *dataTableDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	factory, ok := req.ProviderData.(clientFactory)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", fmt.Sprintf("Expected an Amazon Connect client factory, got %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	d.client = factory.Connect()
}

func (d *dataTableDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data dataTableDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}
	if data.InstanceID.IsNull() || data.InstanceID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("instance_id"), "Data Table Instance Not Known", "instance_id must be known before the data-table lookup can run.")
		return
	}
	if data.DataTableID.IsUnknown() || data.Name.IsUnknown() || data.DataTableID.IsNull() == data.Name.IsNull() {
		resp.Diagnostics.AddError("Invalid Data Table Lookup", "Exactly one of data_table_id or name must be known before the data-table lookup can run.")
		return
	}

	tableID := data.DataTableID.ValueString()
	if data.DataTableID.IsNull() {
		var err error
		tableID, err = d.searchByName(ctx, data.InstanceID.ValueString(), data.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Unable to Find Data Table", err.Error())
			return
		}
	}

	output, err := d.client.DescribeDataTable(ctx, &awsconnect.DescribeDataTableInput{
		InstanceId:  aws.String(data.InstanceID.ValueString()),
		DataTableId: aws.String(tableID),
	})
	if err != nil {
		var notFound *connecttypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			resp.Diagnostics.AddError("Data Table Not Found", fmt.Sprintf("Data table %q was not found in instance %q.", tableID, data.InstanceID.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Unable to Describe Data Table", fmt.Sprintf("Could not describe data table %q in instance %q: %s", tableID, data.InstanceID.ValueString(), err))
		return
	}
	if output == nil || output.DataTable == nil || output.DataTable.Id == nil || output.DataTable.Name == nil || output.DataTable.Arn == nil {
		resp.Diagnostics.AddError("Incomplete Data Table Response", fmt.Sprintf("Amazon Connect returned incomplete metadata for data table %q in instance %q.", tableID, data.InstanceID.ValueString()))
		return
	}

	table := output.DataTable
	if !data.Name.IsNull() && aws.ToString(table.Name) != data.Name.ValueString() {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Data Table Name Changed", fmt.Sprintf("Data table %q no longer has the requested name %q.", tableID, data.Name.ValueString()))
		return
	}
	data.ID = types.StringValue(aws.ToString(table.Id))
	data.ARN = types.StringValue(aws.ToString(table.Arn))
	data.Name = types.StringValue(aws.ToString(table.Name))
	data.Description = dataTableStringValue(table.Description)
	data.TimeZone = dataTableStringValue(table.TimeZone)
	data.Status = dataTableEnumValue(string(table.Status))
	data.ValueLockLevel = dataTableEnumValue(string(table.ValueLockLevel))
	if table.Tags == nil {
		data.Tags = types.MapNull(types.StringType)
	} else {
		var diagnostics diag.Diagnostics
		data.Tags, diagnostics = types.MapValueFrom(ctx, types.StringType, table.Tags)
		resp.Diagnostics.Append(diagnostics...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func dataTableStringValue(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func dataTableEnumValue(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

func (d *dataTableDataSource) searchByName(ctx context.Context, instanceID, name string) (string, error) {
	input := &awsconnect.SearchDataTablesInput{
		InstanceId: aws.String(instanceID),
		MaxResults: aws.Int32(maxDataTablesPerPage),
		SearchCriteria: &connecttypes.DataTableSearchCriteria{StringCondition: &connecttypes.StringCondition{
			ComparisonType: connecttypes.StringComparisonTypeExact,
			FieldName:      aws.String("name"),
			Value:          aws.String(name),
		}},
	}
	var matchedID string
	matchCount := 0
	seenTokens := make(map[string]struct{})
	for {
		output, err := d.client.SearchDataTables(ctx, input)
		if err != nil {
			return "", fmt.Errorf("could not search data tables in instance %q: %w", instanceID, err)
		}
		if output == nil {
			return "", fmt.Errorf("SearchDataTables returned a nil response")
		}
		for _, table := range output.DataTables {
			if aws.ToString(table.Name) != name {
				continue
			}
			matchCount++
			matchedID = aws.ToString(table.Id)
		}
		if output.NextToken == nil || aws.ToString(output.NextToken) == "" {
			break
		}
		token := aws.ToString(output.NextToken)
		if _, repeated := seenTokens[token]; repeated {
			return "", fmt.Errorf("amazon connect returned duplicate data-table pagination token %q", token)
		}
		seenTokens[token] = struct{}{}
		input.NextToken = output.NextToken
	}
	switch matchCount {
	case 0:
		return "", fmt.Errorf("no data table named %q was found in instance %q", name, instanceID)
	case 1:
		if matchedID == "" {
			return "", fmt.Errorf("data table named %q in instance %q has no ID", name, instanceID)
		}
		return matchedID, nil
	default:
		return "", fmt.Errorf("found %d data tables named %q in instance %q; the name must identify exactly one table", matchCount, name, instanceID)
	}
}
