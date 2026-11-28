package connect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const usersDataSourceTestInstanceID = "00000000-0000-0000-0000-000000000040"

type fakeUsersClient struct {
	search func(context.Context, *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error)
}

func (f *fakeUsersClient) SearchUsers(ctx context.Context, input *awsconnect.SearchUsersInput, _ ...func(*awsconnect.Options)) (*awsconnect.SearchUsersOutput, error) {
	if f.search == nil {
		return &awsconnect.SearchUsersOutput{}, nil
	}
	return f.search(ctx, input)
}

func TestUsersDataSourceReadFiltersCaseSensitiveFieldsAcrossPagesAndSortsResults(t *testing.T) {
	var pageTokens []string
	client := &fakeUsersClient{
		search: func(_ context.Context, input *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
			if aws.ToString(input.InstanceId) != usersDataSourceTestInstanceID {
				t.Fatalf("unexpected instance ID %q", aws.ToString(input.InstanceId))
			}
			if input.MaxResults == nil || aws.ToInt32(input.MaxResults) != maxUsersPerPage {
				t.Fatalf("expected user page size %d, got %#v", maxUsersPerPage, input.MaxResults)
			}
			pageTokens = append(pageTokens, aws.ToString(input.NextToken))

			switch aws.ToString(input.NextToken) {
			case "":
				return &awsconnect.SearchUsersOutput{
					Users: []connecttypes.UserSearchSummary{
						usersTestSummary("user-z", "svc-ACME-operator", "Ada Lovelace", "Byron Lovelace"),
						usersTestSummary("wrong-username-case", "svc-acme-operator", "Ada Lovelace", "Byron Lovelace"),
					},
					NextToken: aws.String("page-2"),
				}, nil
			case "page-2":
				return &awsconnect.SearchUsersOutput{Users: []connecttypes.UserSearchSummary{
					usersTestSummary("user-a", "ACME-agent", "Ada", "Lovelace"),
					usersTestSummary("wrong-first-name-case", "ACME-agent", "ada", "Lovelace"),
					usersTestSummary("wrong-last-name-case", "ACME-agent", "Ada", "loveLace"),
				}}, nil
			default:
				t.Fatalf("unexpected pagination token %q", aws.ToString(input.NextToken))
				return nil, nil
			}
		},
	}

	config := usersDataSourceConfig(t, nil, usersTestString("ACME"), usersTestString("Ada"), usersTestString("Lovelace"))
	response := readUsersDataSource(t, client, config)
	data := usersReadState(t, response)

	if got, want := strings.Join(usersIDs(data.Users), ","), "user-a,user-z"; got != want {
		t.Fatalf("expected only the users matching all case-sensitive filters, sorted by ID %q, got %q", want, got)
	}
	if got, want := pageTokens, []string{"", "page-2"}; !equalStrings(got, want) {
		t.Fatalf("expected every page to be requested, got tokens %v", got)
	}
}

func TestUsersDataSourceReadPassesExactTagConditions(t *testing.T) {
	client := &fakeUsersClient{
		search: func(_ context.Context, input *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
			if input.SearchFilter == nil || input.SearchFilter.TagFilter == nil {
				t.Fatal("expected the configured tags to be sent as a tag filter")
			}
			conditions := input.SearchFilter.TagFilter.AndConditions
			if len(conditions) != 2 {
				t.Fatalf("expected both configured tag conditions, got %#v", conditions)
			}
			if aws.ToString(conditions[0].TagKey) != "Department" || aws.ToString(conditions[0].TagValue) != "Research" {
				t.Fatalf("unexpected first tag condition: %#v", conditions[0])
			}
			if aws.ToString(conditions[1].TagKey) != "Environment" || conditions[1].TagValue == nil || aws.ToString(conditions[1].TagValue) != "" {
				t.Fatalf("expected the explicit empty Environment value, got %#v", conditions[1])
			}
			return &awsconnect.SearchUsersOutput{}, nil
		},
	}

	config := usersDataSourceConfig(t, map[string]string{"Environment": "", "Department": "Research"}, nil, nil, nil)
	response := readUsersDataSource(t, client, config)
	usersReadState(t, response)
}

func TestUsersDataSourceReadOmitsNullAndEmptyTagFilters(t *testing.T) {
	for _, test := range []struct {
		name string
		tags map[string]string
	}{
		{name: "null map"},
		{name: "empty map", tags: map[string]string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeUsersClient{
				search: func(_ context.Context, input *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
					if input.SearchFilter != nil {
						t.Fatalf("expected no search filter for %s tags, got %#v", test.name, input.SearchFilter)
					}
					return &awsconnect.SearchUsersOutput{}, nil
				},
			}

			config := usersDataSourceConfig(t, test.tags, nil, nil, nil)
			response := readUsersDataSource(t, client, config)
			usersReadState(t, response)
		})
	}
}

func TestUsersDataSourceReadMapsUnsetNamesToNull(t *testing.T) {
	client := &fakeUsersClient{
		search: func(context.Context, *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
			return &awsconnect.SearchUsersOutput{Users: []connecttypes.UserSearchSummary{
				{Id: aws.String("user-no-identity"), Username: aws.String("plain-login")},
				{Id: aws.String("user-no-names"), Username: aws.String("empty-identity"), IdentityInfo: &connecttypes.UserIdentityInfoLite{}},
			}}, nil
		},
	}

	config := usersDataSourceConfig(t, nil, nil, nil, nil)
	response := readUsersDataSource(t, client, config)
	data := usersReadState(t, response)
	if len(data.Users) != 2 {
		t.Fatalf("expected both users, got %#v", data.Users)
	}
	for _, user := range data.Users {
		if !user.FirstName.IsNull() || !user.LastName.IsNull() {
			t.Errorf("expected unset names for %q to remain null, got first=%#v last=%#v", user.ID.ValueString(), user.FirstName, user.LastName)
		}
	}
}

func TestUsersDataSourceReadSetsKnownEmptyResults(t *testing.T) {
	client := &fakeUsersClient{
		search: func(context.Context, *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
			return &awsconnect.SearchUsersOutput{}, nil
		},
	}

	config := usersDataSourceConfig(t, nil, nil, nil, nil)
	response := readUsersDataSource(t, client, config)
	data := usersReadState(t, response)
	if len(data.Users) != 0 {
		t.Fatalf("expected no users, got %#v", data.Users)
	}

	var rawState map[string]tftypes.Value
	if err := response.State.Raw.As(&rawState); err != nil {
		t.Fatalf("could not inspect raw state: %v", err)
	}
	usersValue, ok := rawState["users"]
	if !ok || !usersValue.IsKnown() || usersValue.IsNull() {
		t.Fatalf("expected users to be a known empty list, got %#v", usersValue)
	}
}

func TestUsersDataSourceReadReportsSearchErrors(t *testing.T) {
	apiError := errors.New("SearchUsers unavailable")
	tests := []struct {
		name          string
		search        func(context.Context, *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error)
		wantDetail    string
		wantCallCount int
	}{
		{
			name: "API error",
			search: func(context.Context, *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
				return nil, apiError
			},
			wantDetail:    apiError.Error(),
			wantCallCount: 1,
		},
		{
			name: "nil response",
			search: func(context.Context, *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
				return nil, nil
			},
			wantDetail:    "SearchUsers returned a nil response",
			wantCallCount: 1,
		},
		{
			name: "later page error",
			search: func(_ context.Context, input *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
				if input.NextToken == nil {
					return &awsconnect.SearchUsersOutput{Users: []connecttypes.UserSearchSummary{usersTestSummary("user-1", "login", "First", "Last")}, NextToken: aws.String("page-2")}, nil
				}
				return nil, errors.New("page two unavailable")
			},
			wantDetail:    "page two unavailable",
			wantCallCount: 2,
		},
		{
			name: "repeated pagination token",
			search: func(_ context.Context, input *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
				return &awsconnect.SearchUsersOutput{NextToken: aws.String("repeat")}, nil
			},
			wantDetail:    "repeated pagination token from SearchUsers",
			wantCallCount: 2,
		},
		{
			name: "matching user without ID",
			search: func(context.Context, *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
				return &awsconnect.SearchUsersOutput{Users: []connecttypes.UserSearchSummary{{Username: aws.String("login")}}}, nil
			},
			wantDetail:    "matching user without an ID",
			wantCallCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := &fakeUsersClient{
				search: func(ctx context.Context, input *awsconnect.SearchUsersInput) (*awsconnect.SearchUsersOutput, error) {
					calls++
					return test.search(ctx, input)
				},
			}
			config := usersDataSourceConfig(t, nil, nil, nil, nil)
			response := readUsersDataSource(t, client, config)
			if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Unable to Search Connect Users" {
				t.Fatalf("expected user search error diagnostic, got %v", response.Diagnostics)
			}
			if !strings.Contains(response.Diagnostics[0].Detail(), test.wantDetail) {
				t.Fatalf("expected diagnostic to contain %q, got %q", test.wantDetail, response.Diagnostics[0].Detail())
			}
			if calls != test.wantCallCount {
				t.Fatalf("expected %d SearchUsers calls, got %d", test.wantCallCount, calls)
			}
		})
	}
}

func TestUsersDataSourceReadRequiresConfiguredClient(t *testing.T) {
	config := usersDataSourceConfig(t, nil, nil, nil, nil)
	response := readUsersDataSource(t, nil, config)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Amazon Connect Client Not Configured" {
		t.Fatalf("expected missing-client diagnostic, got %v", response.Diagnostics)
	}
}

func usersTestSummary(id, username, firstName, lastName string) connecttypes.UserSearchSummary {
	return connecttypes.UserSearchSummary{
		Id:       aws.String(id),
		Username: aws.String(username),
		IdentityInfo: &connecttypes.UserIdentityInfoLite{
			FirstName: aws.String(firstName),
			LastName:  aws.String(lastName),
		},
	}
}

func usersTestString(value string) *string {
	return aws.String(value)
}

func usersDataSourceSchema(t *testing.T) datasourceschema.Schema {
	t.Helper()
	response := &datasource.SchemaResponse{}
	NewUsersDataSource().Schema(context.Background(), datasource.SchemaRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected users schema diagnostics: %v", response.Diagnostics)
	}
	return response.Schema
}

func usersDataSourceConfig(t *testing.T, tags map[string]string, username, firstName, lastName *string) tfsdk.Config {
	t.Helper()
	schema := usersDataSourceSchema(t)
	return tfsdk.Config{
		Raw:    usersDataSourceRawValue(tags, username, firstName, lastName),
		Schema: schema,
	}
}

func usersDataSourceRawValue(tags map[string]string, username, firstName, lastName *string) tftypes.Value {
	tagType := tftypes.Map{ElementType: tftypes.String}
	var tagElements map[string]tftypes.Value
	if tags != nil {
		tagElements = make(map[string]tftypes.Value, len(tags))
		for key, value := range tags {
			tagElements[key] = tftypes.NewValue(tftypes.String, value)
		}
	}

	userType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":         tftypes.String,
		"arn":        tftypes.String,
		"username":   tftypes.String,
		"first_name": tftypes.String,
		"last_name":  tftypes.String,
	}}
	userListType := tftypes.List{ElementType: userType}
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"instance_id":         tftypes.String,
		"tags":                tagType,
		"username_contains":   tftypes.String,
		"first_name_contains": tftypes.String,
		"last_name_contains":  tftypes.String,
		"users":               userListType,
	}}

	return tftypes.NewValue(objectType, map[string]tftypes.Value{
		"instance_id":         tftypes.NewValue(tftypes.String, usersDataSourceTestInstanceID),
		"tags":                tftypes.NewValue(tagType, tagElements),
		"username_contains":   usersTestTerraformString(username),
		"first_name_contains": usersTestTerraformString(firstName),
		"last_name_contains":  usersTestTerraformString(lastName),
		"users":               tftypes.NewValue(userListType, nil),
	})
}

func usersTestTerraformString(value *string) tftypes.Value {
	if value == nil {
		return tftypes.NewValue(tftypes.String, nil)
	}
	return tftypes.NewValue(tftypes.String, *value)
}

func readUsersDataSource(t *testing.T, client usersClient, config tfsdk.Config) *datasource.ReadResponse {
	t.Helper()
	response := &datasource.ReadResponse{State: tfsdk.State(config)}
	(&usersDataSource{client: client}).Read(context.Background(), datasource.ReadRequest{Config: config}, response)
	return response
}

func usersReadState(t *testing.T, response *datasource.ReadResponse) usersDataSourceModel {
	t.Helper()
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected users read diagnostics: %v", response.Diagnostics)
	}
	var data usersDataSourceModel
	if diagnostics := response.State.Get(context.Background(), &data); diagnostics.HasError() {
		t.Fatalf("unexpected users state diagnostics: %v", diagnostics)
	}
	return data
}

func usersIDs(users []usersDataSourceUserModel) []string {
	ids := make([]string, len(users))
	for index, user := range users {
		ids[index] = user.ID.ValueString()
	}
	return ids
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
