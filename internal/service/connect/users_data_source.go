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

const maxUsersPerPage = 500

var _ datasource.DataSource = &usersDataSource{}
var _ datasource.DataSourceWithConfigure = &usersDataSource{}

type usersClient interface {
	SearchUsers(context.Context, *awsconnect.SearchUsersInput, ...func(*awsconnect.Options)) (*awsconnect.SearchUsersOutput, error)
}

type usersDataSource struct {
	client usersClient
}

type usersDataSourceModel struct {
	InstanceID        types.String               `tfsdk:"instance_id"`
	Tags              types.Map                  `tfsdk:"tags"`
	UsernameContains  types.String               `tfsdk:"username_contains"`
	FirstNameContains types.String               `tfsdk:"first_name_contains"`
	LastNameContains  types.String               `tfsdk:"last_name_contains"`
	Users             []usersDataSourceUserModel `tfsdk:"users"`
}

type usersDataSourceUserModel struct {
	ID        types.String `tfsdk:"id"`
	ARN       types.String `tfsdk:"arn"`
	Username  types.String `tfsdk:"username"`
	FirstName types.String `tfsdk:"first_name"`
	LastName  types.String `tfsdk:"last_name"`
}

func NewUsersDataSource() datasource.DataSource {
	return &usersDataSource{}
}

func UsersDataSourceFactory() func() datasource.DataSource {
	return func() datasource.DataSource {
		return NewUsersDataSource()
	}
}

func (d *usersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_users"
}

func (d *usersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		MarkdownDescription: "Searches Amazon Connect users. Results are sorted by user ID. Optional filters combine with AND; string filters use case-sensitive literal substring matching, and every supplied tag must match.",
		Attributes: map[string]datasourceschema.Attribute{
			"instance_id": datasourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier.",
				Required:            true,
			},
			"tags": datasourceschema.MapAttribute{
				MarkdownDescription: "Optional exact tag key/value pairs. A user must match every supplied pair; an omitted or empty map applies no tag constraint.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"username_contains": datasourceschema.StringAttribute{
				MarkdownDescription: "Optional case-sensitive literal substring required in the user's username.",
				Optional:            true,
			},
			"first_name_contains": datasourceschema.StringAttribute{
				MarkdownDescription: "Optional case-sensitive literal substring required in the user's first name.",
				Optional:            true,
			},
			"last_name_contains": datasourceschema.StringAttribute{
				MarkdownDescription: "Optional case-sensitive literal substring required in the user's last name.",
				Optional:            true,
			},
			"users": datasourceschema.ListNestedAttribute{
				MarkdownDescription: "Users matching all configured filters, sorted by user ID.",
				Computed:            true,
				NestedObject: datasourceschema.NestedAttributeObject{
					Attributes: map[string]datasourceschema.Attribute{
						"id": datasourceschema.StringAttribute{
							MarkdownDescription: "Amazon Connect user identifier.",
							Computed:            true,
						},
						"arn": datasourceschema.StringAttribute{
							MarkdownDescription: "Amazon Connect user ARN.",
							Computed:            true,
						},
						"username": datasourceschema.StringAttribute{
							MarkdownDescription: "Amazon Connect username.",
							Computed:            true,
						},
						"first_name": datasourceschema.StringAttribute{
							MarkdownDescription: "User's first name, when set.",
							Computed:            true,
						},
						"last_name": datasourceschema.StringAttribute{
							MarkdownDescription: "User's last name, when set.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *usersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *usersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data usersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Amazon Connect Client Not Configured", "The provider did not configure an Amazon Connect client.")
		return
	}
	if data.InstanceID.IsNull() || data.InstanceID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("instance_id"), "User Search Instance Not Known", "instance_id must be known before users can be searched.")
		return
	}
	if data.Tags.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("tags"), "User Search Tags Not Known", "tags must be known before users can be searched.")
		return
	}
	if data.UsernameContains.IsUnknown() || data.FirstNameContains.IsUnknown() || data.LastNameContains.IsUnknown() {
		resp.Diagnostics.AddError("User Search Filters Not Known", "All configured user search filters must be known before users can be searched.")
		return
	}

	tagFilter, diagnostics := discoveryTagFilter(ctx, data.Tags)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	users, err := d.searchUsers(ctx, data.InstanceID.ValueString(), tagFilter, data.UsernameContains, data.FirstNameContains, data.LastNameContains)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Search Connect Users",
			fmt.Sprintf("Could not search users in instance %q: %s", data.InstanceID.ValueString(), err),
		)
		return
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].ID.ValueString() < users[j].ID.ValueString()
	})
	data.Users = users

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *usersDataSource) searchUsers(ctx context.Context, instanceID string, tagFilter *connecttypes.ControlPlaneTagFilter, username, firstName, lastName types.String) ([]usersDataSourceUserModel, error) {
	input := &awsconnect.SearchUsersInput{
		InstanceId: aws.String(instanceID),
		MaxResults: aws.Int32(maxUsersPerPage),
	}
	if tagFilter != nil {
		input.SearchFilter = &connecttypes.UserSearchFilter{TagFilter: tagFilter}
	}

	users := make([]usersDataSourceUserModel, 0)
	seenTokens := make(map[string]struct{})
	for {
		output, err := d.client.SearchUsers(ctx, input)
		if err != nil {
			return nil, err
		}
		if output == nil {
			return nil, fmt.Errorf("SearchUsers returned a nil response")
		}

		for _, user := range output.Users {
			if !matchesUsersFilters(user, username, firstName, lastName) {
				continue
			}
			if user.Id == nil || aws.ToString(user.Id) == "" {
				return nil, fmt.Errorf("SearchUsers returned a matching user without an ID")
			}
			users = append(users, usersDataSourceUser(user))
		}

		if output.NextToken == nil || aws.ToString(output.NextToken) == "" {
			return users, nil
		}

		token := aws.ToString(output.NextToken)
		if _, repeated := seenTokens[token]; repeated {
			return nil, fmt.Errorf("repeated pagination token from SearchUsers")
		}
		seenTokens[token] = struct{}{}
		input.NextToken = output.NextToken
	}
}

func matchesUsersFilters(user connecttypes.UserSearchSummary, username, firstName, lastName types.String) bool {
	if !matchesUserString(aws.ToString(user.Username), username) {
		return false
	}
	if user.IdentityInfo == nil {
		return matchesUserString("", firstName) && matchesUserString("", lastName)
	}
	return matchesUserString(aws.ToString(user.IdentityInfo.FirstName), firstName) && matchesUserString(aws.ToString(user.IdentityInfo.LastName), lastName)
}

func matchesUserString(value string, contains types.String) bool {
	if contains.IsNull() || contains.ValueString() == "" {
		return true
	}
	return strings.Contains(value, contains.ValueString())
}

func usersDataSourceUser(user connecttypes.UserSearchSummary) usersDataSourceUserModel {
	result := usersDataSourceUserModel{
		ID:       types.StringPointerValue(user.Id),
		ARN:      types.StringPointerValue(user.Arn),
		Username: types.StringPointerValue(user.Username),
	}
	if user.IdentityInfo == nil {
		result.FirstName = types.StringNull()
		result.LastName = types.StringNull()
		return result
	}

	result.FirstName = types.StringPointerValue(user.IdentityInfo.FirstName)
	result.LastName = types.StringPointerValue(user.IdentityInfo.LastName)
	return result
}
