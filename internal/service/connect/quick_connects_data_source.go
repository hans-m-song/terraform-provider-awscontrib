package connect

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	maxQuickConnectsPerListPage   = 1000
	maxQuickConnectsPerSearchPage = 100
)

var _ datasource.DataSource = &quickConnectsDataSource{}
var _ datasource.DataSourceWithConfigure = &quickConnectsDataSource{}

type quickConnectsClient interface {
	ListQuickConnects(context.Context, *awsconnect.ListQuickConnectsInput, ...func(*awsconnect.Options)) (*awsconnect.ListQuickConnectsOutput, error)
	SearchQuickConnects(context.Context, *awsconnect.SearchQuickConnectsInput, ...func(*awsconnect.Options)) (*awsconnect.SearchQuickConnectsOutput, error)
}

type quickConnectsDataSource struct {
	client quickConnectsClient
}

type quickConnectsDataSourceModel struct {
	InstanceID        types.String                         `tfsdk:"instance_id"`
	QuickConnectTypes types.Set                            `tfsdk:"quick_connect_types"`
	NameContains      types.String                         `tfsdk:"name_contains"`
	Tags              types.Map                            `tfsdk:"tags"`
	QuickConnects     []quickConnectSummaryDataSourceModel `tfsdk:"quick_connects"`
}

type quickConnectSummaryDataSourceModel struct {
	ID                 types.String `tfsdk:"id"`
	ARN                types.String `tfsdk:"arn"`
	Name               types.String `tfsdk:"name"`
	QuickConnectType   types.String `tfsdk:"quick_connect_type"`
	LastModifiedRegion types.String `tfsdk:"last_modified_region"`
	LastModifiedTime   types.String `tfsdk:"last_modified_time"`
}

type quickConnectTypesValidator struct{}

func (quickConnectTypesValidator) Description(context.Context) string {
	return "must contain only USER, QUEUE, PHONE_NUMBER, or FLOW"
}

func (v quickConnectTypesValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (quickConnectTypesValidator) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	var values []types.String
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &values, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for _, value := range values {
		if value.IsUnknown() {
			continue
		}
		if value.IsNull() || !validQuickConnectType(value.ValueString()) {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid Quick Connect Type",
				fmt.Sprintf("quick_connect_types values must be USER, QUEUE, PHONE_NUMBER, or FLOW; got %q", value.ValueString()),
			)
		}
	}
}

func validQuickConnectType(value string) bool {
	switch value {
	case string(connecttypes.QuickConnectTypeUser), string(connecttypes.QuickConnectTypeQueue), string(connecttypes.QuickConnectTypePhoneNumber), string(connecttypes.QuickConnectTypeFlow):
		return true
	default:
		return false
	}
}

func QuickConnectsDataSourceFactory() func() datasource.DataSource {
	return func() datasource.DataSource {
		return &quickConnectsDataSource{}
	}
}

func (d *quickConnectsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_quick_connects"
}

func (d *quickConnectsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		MarkdownDescription: "Lists Amazon Connect quick connects in an instance. Optional type and case-sensitive literal name filters are applied to results. When `tags` is set, Amazon Connect searches for resources matching every exact key-value pair.",
		Attributes: map[string]datasourceschema.Attribute{
			"instance_id": datasourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
			},
			"quick_connect_types": datasourceschema.SetAttribute{
				MarkdownDescription: "Optional quick-connect types to return: USER, QUEUE, PHONE_NUMBER, or FLOW.",
				ElementType:         types.StringType,
				Optional:            true,
				Validators:          []validator.Set{quickConnectTypesValidator{}},
			},
			"name_contains": datasourceschema.StringAttribute{
				MarkdownDescription: "Optional case-sensitive literal substring that quick-connect names must contain.",
				Optional:            true,
			},
			"tags": datasourceschema.MapAttribute{
				MarkdownDescription: "Optional exact tag key-value pairs. Results must match every pair.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"quick_connects": datasourceschema.ListNestedAttribute{
				MarkdownDescription: "Quick-connect summaries sorted by ID.",
				Computed:            true,
				NestedObject: datasourceschema.NestedAttributeObject{
					Attributes: map[string]datasourceschema.Attribute{
						"id": datasourceschema.StringAttribute{
							MarkdownDescription: "Quick-connect identifier.",
							Computed:            true,
						},
						"arn": datasourceschema.StringAttribute{
							MarkdownDescription: "Quick-connect Amazon Resource Name, when returned.",
							Computed:            true,
						},
						"name": datasourceschema.StringAttribute{
							MarkdownDescription: "Quick-connect name, when returned.",
							Computed:            true,
						},
						"quick_connect_type": datasourceschema.StringAttribute{
							MarkdownDescription: "Quick-connect type, when returned.",
							Computed:            true,
						},
						"last_modified_region": datasourceschema.StringAttribute{
							MarkdownDescription: "AWS Region where the quick connect was last modified, when returned.",
							Computed:            true,
						},
						"last_modified_time": datasourceschema.StringAttribute{
							MarkdownDescription: "Time the quick connect was last modified, formatted as RFC 3339, when returned.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *quickConnectsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *quickConnectsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data quickConnectsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}
	if data.InstanceID.IsNull() || data.InstanceID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("instance_id"), "Quick Connect Instance Not Known", "instance_id must be known before quick connects can be listed.")
		return
	}
	if data.NameContains.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("name_contains"), "Quick Connect Name Filter Not Known", "name_contains must be known before quick connects can be listed.")
		return
	}
	if data.QuickConnectTypes.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("quick_connect_types"), "Quick Connect Types Not Known", "quick_connect_types must be known before quick connects can be listed.")
		return
	}

	typeFilter, typeValues, typeDiags := quickConnectTypesFilter(ctx, data.QuickConnectTypes)
	resp.Diagnostics.Append(typeDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	tagFilter, tagDiags := discoveryTagFilter(ctx, data.Tags)
	resp.Diagnostics.Append(tagDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var summaries []connecttypes.QuickConnectSummary
	var err error
	if tagFilter == nil {
		summaries, err = d.listQuickConnects(ctx, data.InstanceID.ValueString(), typeFilter)
	} else {
		summaries, err = d.searchQuickConnects(ctx, data.InstanceID.ValueString(), tagFilter)
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to List Quick Connects",
			fmt.Sprintf("Could not list quick connects in instance %q: %s", data.InstanceID.ValueString(), err),
		)
		return
	}

	nameContains := ""
	if !data.NameContains.IsNull() {
		nameContains = data.NameContains.ValueString()
	}

	filtered := make([]connecttypes.QuickConnectSummary, 0, len(summaries))
	for _, summary := range summaries {
		if nameContains != "" && !strings.Contains(aws.ToString(summary.Name), nameContains) {
			continue
		}
		if len(typeValues) > 0 {
			if _, ok := typeValues[string(summary.QuickConnectType)]; !ok {
				continue
			}
		}
		if summary.Id == nil || aws.ToString(summary.Id) == "" {
			resp.Diagnostics.AddError("Incomplete Quick Connect Summary", "Amazon Connect returned a quick-connect summary without an ID.")
			return
		}
		filtered = append(filtered, summary)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return aws.ToString(filtered[i].Id) < aws.ToString(filtered[j].Id)
	})

	data.QuickConnects = make([]quickConnectSummaryDataSourceModel, 0, len(filtered))
	for _, summary := range filtered {
		data.QuickConnects = append(data.QuickConnects, quickConnectSummaryModel(summary))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func quickConnectTypesFilter(ctx context.Context, configured types.Set) ([]connecttypes.QuickConnectType, map[string]struct{}, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	if configured.IsNull() {
		return nil, nil, diagnostics
	}

	var values []types.String
	diagnostics.Append(configured.ElementsAs(ctx, &values, false)...)
	if diagnostics.HasError() {
		return nil, nil, diagnostics
	}

	apiValues := make([]connecttypes.QuickConnectType, 0, len(values))
	filterValues := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value.IsNull() || value.IsUnknown() {
			diagnostics.AddError("Invalid Quick Connect Types", "quick_connect_types must contain only known, non-null values.")
			return nil, nil, diagnostics
		}
		if !validQuickConnectType(value.ValueString()) {
			diagnostics.AddError("Invalid Quick Connect Type", fmt.Sprintf("Unsupported quick-connect type %q.", value.ValueString()))
			return nil, nil, diagnostics
		}
		apiValues = append(apiValues, connecttypes.QuickConnectType(value.ValueString()))
		filterValues[value.ValueString()] = struct{}{}
	}
	if len(apiValues) == 0 {
		return nil, nil, diagnostics
	}

	sort.Slice(apiValues, func(i, j int) bool { return apiValues[i] < apiValues[j] })
	return apiValues, filterValues, diagnostics
}

func (d *quickConnectsDataSource) listQuickConnects(ctx context.Context, instanceID string, quickConnectTypes []connecttypes.QuickConnectType) ([]connecttypes.QuickConnectSummary, error) {
	input := &awsconnect.ListQuickConnectsInput{
		InstanceId:        aws.String(instanceID),
		MaxResults:        aws.Int32(maxQuickConnectsPerListPage),
		QuickConnectTypes: quickConnectTypes,
	}

	var summaries []connecttypes.QuickConnectSummary
	seenTokens := make(map[string]struct{})
	for {
		output, err := d.client.ListQuickConnects(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("list quick connects request failed: %w", err)
		}
		if output == nil {
			return nil, fmt.Errorf("list quick connects returned a nil response")
		}
		summaries = append(summaries, output.QuickConnectSummaryList...)
		if output.NextToken == nil || aws.ToString(output.NextToken) == "" {
			return summaries, nil
		}

		token := aws.ToString(output.NextToken)
		if _, repeated := seenTokens[token]; repeated {
			return nil, fmt.Errorf("repeated pagination token from ListQuickConnects")
		}
		seenTokens[token] = struct{}{}
		input.NextToken = output.NextToken
	}
}

func (d *quickConnectsDataSource) searchQuickConnects(ctx context.Context, instanceID string, tagFilter *connecttypes.ControlPlaneTagFilter) ([]connecttypes.QuickConnectSummary, error) {
	input := &awsconnect.SearchQuickConnectsInput{
		InstanceId: aws.String(instanceID),
		MaxResults: aws.Int32(maxQuickConnectsPerSearchPage),
		SearchFilter: &connecttypes.QuickConnectSearchFilter{
			TagFilter: tagFilter,
		},
	}

	var summaries []connecttypes.QuickConnectSummary
	seenTokens := make(map[string]struct{})
	for {
		output, err := d.client.SearchQuickConnects(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("search quick connects request failed: %w", err)
		}
		if output == nil {
			return nil, fmt.Errorf("search quick connects returned a nil response")
		}
		for _, quickConnect := range output.QuickConnects {
			var quickConnectType connecttypes.QuickConnectType
			if quickConnect.QuickConnectConfig != nil {
				quickConnectType = quickConnect.QuickConnectConfig.QuickConnectType
			}
			summaries = append(summaries, connecttypes.QuickConnectSummary{
				Arn:                quickConnect.QuickConnectARN,
				Id:                 quickConnect.QuickConnectId,
				LastModifiedRegion: quickConnect.LastModifiedRegion,
				LastModifiedTime:   quickConnect.LastModifiedTime,
				Name:               quickConnect.Name,
				QuickConnectType:   quickConnectType,
			})
		}
		if output.NextToken == nil || aws.ToString(output.NextToken) == "" {
			return summaries, nil
		}

		token := aws.ToString(output.NextToken)
		if _, repeated := seenTokens[token]; repeated {
			return nil, fmt.Errorf("repeated pagination token from SearchQuickConnects")
		}
		seenTokens[token] = struct{}{}
		input.NextToken = output.NextToken
	}
}

func quickConnectSummaryModel(summary connecttypes.QuickConnectSummary) quickConnectSummaryDataSourceModel {
	return quickConnectSummaryDataSourceModel{
		ID:                 types.StringPointerValue(summary.Id),
		ARN:                types.StringPointerValue(summary.Arn),
		Name:               types.StringPointerValue(summary.Name),
		QuickConnectType:   quickConnectTypeValue(summary.QuickConnectType),
		LastModifiedRegion: types.StringPointerValue(summary.LastModifiedRegion),
		LastModifiedTime:   quickConnectTimeValue(summary.LastModifiedTime),
	}
}

func quickConnectTypeValue(value connecttypes.QuickConnectType) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(string(value))
}

func quickConnectTimeValue(value *time.Time) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(value.UTC().Format(time.RFC3339Nano))
}
