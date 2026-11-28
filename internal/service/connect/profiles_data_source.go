package connect

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const connectProfilesPerPage = 100

var _ datasource.DataSource = &routingProfilesDataSource{}
var _ datasource.DataSourceWithConfigure = &routingProfilesDataSource{}
var _ datasource.DataSource = &securityProfilesDataSource{}
var _ datasource.DataSourceWithConfigure = &securityProfilesDataSource{}

type routingProfilesClient interface {
	ListRoutingProfiles(context.Context, *awsconnect.ListRoutingProfilesInput, ...func(*awsconnect.Options)) (*awsconnect.ListRoutingProfilesOutput, error)
	SearchRoutingProfiles(context.Context, *awsconnect.SearchRoutingProfilesInput, ...func(*awsconnect.Options)) (*awsconnect.SearchRoutingProfilesOutput, error)
}

type securityProfilesClient interface {
	ListSecurityProfiles(context.Context, *awsconnect.ListSecurityProfilesInput, ...func(*awsconnect.Options)) (*awsconnect.ListSecurityProfilesOutput, error)
	SearchSecurityProfiles(context.Context, *awsconnect.SearchSecurityProfilesInput, ...func(*awsconnect.Options)) (*awsconnect.SearchSecurityProfilesOutput, error)
}

type routingProfilesDataSource struct {
	client routingProfilesClient
}

type securityProfilesDataSource struct {
	client securityProfilesClient
}

type connectProfileDataSourceModel struct {
	ID   types.String `tfsdk:"id"`
	ARN  types.String `tfsdk:"arn"`
	Name types.String `tfsdk:"name"`
}

type routingProfilesDataSourceModel struct {
	InstanceID      types.String                    `tfsdk:"instance_id"`
	NameContains    types.String                    `tfsdk:"name_contains"`
	Tags            types.Map                       `tfsdk:"tags"`
	RoutingProfiles []connectProfileDataSourceModel `tfsdk:"routing_profiles"`
}

type securityProfilesDataSourceModel struct {
	InstanceID       types.String                    `tfsdk:"instance_id"`
	NameContains     types.String                    `tfsdk:"name_contains"`
	Tags             types.Map                       `tfsdk:"tags"`
	SecurityProfiles []connectProfileDataSourceModel `tfsdk:"security_profiles"`
}

func RoutingProfilesDataSourceFactory() func() datasource.DataSource {
	return func() datasource.DataSource {
		return &routingProfilesDataSource{}
	}
}

func SecurityProfilesDataSourceFactory() func() datasource.DataSource {
	return func() datasource.DataSource {
		return &securityProfilesDataSource{}
	}
}

func (d *routingProfilesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_routing_profiles"
}

func (d *routingProfilesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		MarkdownDescription: "Lists Amazon Connect routing profiles in an instance, with optional tag and case-sensitive name substring filters.",
		Attributes: map[string]datasourceschema.Attribute{
			"instance_id": datasourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
			},
			"name_contains": datasourceschema.StringAttribute{
				MarkdownDescription: "Optional case-sensitive literal substring matched against routing profile names.",
				Optional:            true,
			},
			"tags": datasourceschema.MapAttribute{
				MarkdownDescription: "Optional tag filters. A profile must match every supplied key and value.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"routing_profiles": connectProfilesAttribute("Routing profiles sorted by profile ID."),
		},
	}
}

func (d *securityProfilesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_security_profiles"
}

func (d *securityProfilesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		MarkdownDescription: "Lists Amazon Connect security profiles in an instance, with optional tag and case-sensitive name substring filters.",
		Attributes: map[string]datasourceschema.Attribute{
			"instance_id": datasourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
			},
			"name_contains": datasourceschema.StringAttribute{
				MarkdownDescription: "Optional case-sensitive literal substring matched against security profile names.",
				Optional:            true,
			},
			"tags": datasourceschema.MapAttribute{
				MarkdownDescription: "Optional tag filters. A profile must match every supplied key and value.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"security_profiles": connectProfilesAttribute("Security profiles sorted by profile ID."),
		},
	}
}

func connectProfilesAttribute(description string) datasourceschema.ListNestedAttribute {
	return datasourceschema.ListNestedAttribute{
		MarkdownDescription: description,
		Computed:            true,
		NestedObject: datasourceschema.NestedAttributeObject{
			Attributes: map[string]datasourceschema.Attribute{
				"id": datasourceschema.StringAttribute{
					MarkdownDescription: "Profile identifier.",
					Computed:            true,
				},
				"arn": datasourceschema.StringAttribute{
					MarkdownDescription: "Profile Amazon Resource Name.",
					Computed:            true,
				},
				"name": datasourceschema.StringAttribute{
					MarkdownDescription: "Profile name.",
					Computed:            true,
				},
			},
		},
	}
}

func (d *routingProfilesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *securityProfilesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *routingProfilesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data routingProfilesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}
	if data.InstanceID.IsNull() || data.InstanceID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("instance_id"), "Instance ID Not Known", "instance_id must be known before the routing-profile lookup can run")
		return
	}
	if data.NameContains.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("name_contains"), "Name Filter Not Known", "name_contains must be known before the routing-profile lookup can run")
		return
	}

	tagFilter, diagnostics := discoveryTagFilter(ctx, data.Tags)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	profiles, err := d.routingProfiles(ctx, data.InstanceID.ValueString(), tagFilter)
	if err != nil {
		operation := "list"
		if tagFilter != nil {
			operation = "search"
		}
		resp.Diagnostics.AddError("Unable to Read Routing Profiles", fmt.Sprintf("Could not %s routing profiles in instance %q: %s", operation, data.InstanceID.ValueString(), err))
		return
	}

	nameContains := ""
	if !data.NameContains.IsNull() {
		nameContains = data.NameContains.ValueString()
	}
	data.RoutingProfiles = filterAndSortConnectProfiles(profiles, nameContains)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *securityProfilesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data securityProfilesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}
	if data.InstanceID.IsNull() || data.InstanceID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("instance_id"), "Instance ID Not Known", "instance_id must be known before the security-profile lookup can run")
		return
	}
	if data.NameContains.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("name_contains"), "Name Filter Not Known", "name_contains must be known before the security-profile lookup can run")
		return
	}

	tagFilter, diagnostics := discoveryTagFilter(ctx, data.Tags)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	profiles, err := d.securityProfiles(ctx, data.InstanceID.ValueString(), tagFilter)
	if err != nil {
		operation := "list"
		if tagFilter != nil {
			operation = "search"
		}
		resp.Diagnostics.AddError("Unable to Read Security Profiles", fmt.Sprintf("Could not %s security profiles in instance %q: %s", operation, data.InstanceID.ValueString(), err))
		return
	}

	nameContains := ""
	if !data.NameContains.IsNull() {
		nameContains = data.NameContains.ValueString()
	}
	data.SecurityProfiles = filterAndSortConnectProfiles(profiles, nameContains)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *routingProfilesDataSource) routingProfiles(ctx context.Context, instanceID string, tagFilter *connecttypes.ControlPlaneTagFilter) ([]connectProfileDataSourceModel, error) {
	if tagFilter == nil {
		return paginateConnectProfiles(ctx, "ListRoutingProfiles", func(ctx context.Context, nextToken *string) ([]connectProfileDataSourceModel, *string, error) {
			output, err := d.client.ListRoutingProfiles(ctx, &awsconnect.ListRoutingProfilesInput{
				InstanceId: aws.String(instanceID),
				MaxResults: aws.Int32(connectProfilesPerPage),
				NextToken:  nextToken,
			})
			if err != nil {
				return nil, nil, err
			}
			if output == nil {
				return nil, nil, fmt.Errorf("ListRoutingProfiles returned a nil response")
			}

			profiles := make([]connectProfileDataSourceModel, 0, len(output.RoutingProfileSummaryList))
			for _, profile := range output.RoutingProfileSummaryList {
				model, err := newConnectProfileDataSourceModel(profile.Id, profile.Arn, profile.Name, "routing profile")
				if err != nil {
					return nil, nil, err
				}
				profiles = append(profiles, model)
			}
			return profiles, output.NextToken, nil
		})
	}

	return paginateConnectProfiles(ctx, "SearchRoutingProfiles", func(ctx context.Context, nextToken *string) ([]connectProfileDataSourceModel, *string, error) {
		output, err := d.client.SearchRoutingProfiles(ctx, &awsconnect.SearchRoutingProfilesInput{
			InstanceId: aws.String(instanceID),
			MaxResults: aws.Int32(connectProfilesPerPage),
			NextToken:  nextToken,
			SearchFilter: &connecttypes.RoutingProfileSearchFilter{
				TagFilter: tagFilter,
			},
		})
		if err != nil {
			return nil, nil, err
		}
		if output == nil {
			return nil, nil, fmt.Errorf("SearchRoutingProfiles returned a nil response")
		}

		profiles := make([]connectProfileDataSourceModel, 0, len(output.RoutingProfiles))
		for _, profile := range output.RoutingProfiles {
			model, err := newConnectProfileDataSourceModel(profile.RoutingProfileId, profile.RoutingProfileArn, profile.Name, "routing profile")
			if err != nil {
				return nil, nil, err
			}
			profiles = append(profiles, model)
		}
		return profiles, output.NextToken, nil
	})
}

func (d *securityProfilesDataSource) securityProfiles(ctx context.Context, instanceID string, tagFilter *connecttypes.ControlPlaneTagFilter) ([]connectProfileDataSourceModel, error) {
	if tagFilter == nil {
		return paginateConnectProfiles(ctx, "ListSecurityProfiles", func(ctx context.Context, nextToken *string) ([]connectProfileDataSourceModel, *string, error) {
			output, err := d.client.ListSecurityProfiles(ctx, &awsconnect.ListSecurityProfilesInput{
				InstanceId: aws.String(instanceID),
				MaxResults: aws.Int32(connectProfilesPerPage),
				NextToken:  nextToken,
			})
			if err != nil {
				return nil, nil, err
			}
			if output == nil {
				return nil, nil, fmt.Errorf("ListSecurityProfiles returned a nil response")
			}

			profiles := make([]connectProfileDataSourceModel, 0, len(output.SecurityProfileSummaryList))
			for _, profile := range output.SecurityProfileSummaryList {
				model, err := newConnectProfileDataSourceModel(profile.Id, profile.Arn, profile.Name, "security profile")
				if err != nil {
					return nil, nil, err
				}
				profiles = append(profiles, model)
			}
			return profiles, output.NextToken, nil
		})
	}

	return paginateConnectProfiles(ctx, "SearchSecurityProfiles", func(ctx context.Context, nextToken *string) ([]connectProfileDataSourceModel, *string, error) {
		output, err := d.client.SearchSecurityProfiles(ctx, &awsconnect.SearchSecurityProfilesInput{
			InstanceId: aws.String(instanceID),
			MaxResults: aws.Int32(connectProfilesPerPage),
			NextToken:  nextToken,
			SearchFilter: &connecttypes.SecurityProfilesSearchFilter{
				TagFilter: tagFilter,
			},
		})
		if err != nil {
			return nil, nil, err
		}
		if output == nil {
			return nil, nil, fmt.Errorf("SearchSecurityProfiles returned a nil response")
		}

		profiles := make([]connectProfileDataSourceModel, 0, len(output.SecurityProfiles))
		for _, profile := range output.SecurityProfiles {
			model, err := newConnectProfileDataSourceModel(profile.Id, profile.Arn, profile.SecurityProfileName, "security profile")
			if err != nil {
				return nil, nil, err
			}
			profiles = append(profiles, model)
		}
		return profiles, output.NextToken, nil
	})
}

func paginateConnectProfiles(ctx context.Context, operation string, fetch func(context.Context, *string) ([]connectProfileDataSourceModel, *string, error)) ([]connectProfileDataSourceModel, error) {
	var profiles []connectProfileDataSourceModel
	var nextToken *string
	seenTokens := make(map[string]struct{})
	for {
		pageProfiles, token, err := fetch(ctx, nextToken)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, pageProfiles...)
		if token == nil || aws.ToString(token) == "" {
			return profiles, nil
		}

		tokenValue := aws.ToString(token)
		if _, repeated := seenTokens[tokenValue]; repeated {
			return nil, fmt.Errorf("%s returned a repeated pagination token", operation)
		}
		seenTokens[tokenValue] = struct{}{}
		nextToken = token
	}
}

func newConnectProfileDataSourceModel(id, arn, name *string, profileType string) (connectProfileDataSourceModel, error) {
	idValue := aws.ToString(id)
	if idValue == "" {
		return connectProfileDataSourceModel{}, fmt.Errorf("received a %s without an ID", profileType)
	}

	return connectProfileDataSourceModel{
		ID:   types.StringValue(idValue),
		ARN:  types.StringPointerValue(arn),
		Name: types.StringPointerValue(name),
	}, nil
}

func filterAndSortConnectProfiles(profiles []connectProfileDataSourceModel, nameContains string) []connectProfileDataSourceModel {
	matching := make([]connectProfileDataSourceModel, 0, len(profiles))
	for _, profile := range profiles {
		if strings.Contains(profile.Name.ValueString(), nameContains) {
			matching = append(matching, profile)
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].ID.ValueString() < matching[j].ID.ValueString()
	})
	return matching
}
