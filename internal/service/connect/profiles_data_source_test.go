package connect

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const profilesDataSourceTestInstanceID = "00000000-0000-0000-0000-000000000040"

type fakeConnectProfilesClient struct {
	listRoutingProfiles    func(context.Context, *awsconnect.ListRoutingProfilesInput) (*awsconnect.ListRoutingProfilesOutput, error)
	searchRoutingProfiles  func(context.Context, *awsconnect.SearchRoutingProfilesInput) (*awsconnect.SearchRoutingProfilesOutput, error)
	listSecurityProfiles   func(context.Context, *awsconnect.ListSecurityProfilesInput) (*awsconnect.ListSecurityProfilesOutput, error)
	searchSecurityProfiles func(context.Context, *awsconnect.SearchSecurityProfilesInput) (*awsconnect.SearchSecurityProfilesOutput, error)
}

func (f *fakeConnectProfilesClient) ListRoutingProfiles(ctx context.Context, input *awsconnect.ListRoutingProfilesInput, _ ...func(*awsconnect.Options)) (*awsconnect.ListRoutingProfilesOutput, error) {
	if f.listRoutingProfiles == nil {
		return &awsconnect.ListRoutingProfilesOutput{}, nil
	}
	return f.listRoutingProfiles(ctx, input)
}

func (f *fakeConnectProfilesClient) SearchRoutingProfiles(ctx context.Context, input *awsconnect.SearchRoutingProfilesInput, _ ...func(*awsconnect.Options)) (*awsconnect.SearchRoutingProfilesOutput, error) {
	if f.searchRoutingProfiles == nil {
		return &awsconnect.SearchRoutingProfilesOutput{}, nil
	}
	return f.searchRoutingProfiles(ctx, input)
}

func (f *fakeConnectProfilesClient) ListSecurityProfiles(ctx context.Context, input *awsconnect.ListSecurityProfilesInput, _ ...func(*awsconnect.Options)) (*awsconnect.ListSecurityProfilesOutput, error) {
	if f.listSecurityProfiles == nil {
		return &awsconnect.ListSecurityProfilesOutput{}, nil
	}
	return f.listSecurityProfiles(ctx, input)
}

func (f *fakeConnectProfilesClient) SearchSecurityProfiles(ctx context.Context, input *awsconnect.SearchSecurityProfilesInput, _ ...func(*awsconnect.Options)) (*awsconnect.SearchSecurityProfilesOutput, error) {
	if f.searchSecurityProfiles == nil {
		return &awsconnect.SearchSecurityProfilesOutput{}, nil
	}
	return f.searchSecurityProfiles(ctx, input)
}

func TestRoutingProfilesDataSourceListPaginatesFiltersAndMapsResults(t *testing.T) {
	var tokens []string
	client := &fakeConnectProfilesClient{
		listRoutingProfiles: func(_ context.Context, input *awsconnect.ListRoutingProfilesInput) (*awsconnect.ListRoutingProfilesOutput, error) {
			assertProfileListRequest(t, input.InstanceId, input.MaxResults, input.NextToken, aws.String(profilesDataSourceTestInstanceID), tokens)
			tokens = append(tokens, aws.ToString(input.NextToken))
			if input.NextToken == nil {
				return &awsconnect.ListRoutingProfilesOutput{
					RoutingProfileSummaryList: []connecttypes.RoutingProfileSummary{
						{Id: aws.String("rp-z"), Arn: aws.String("arn:rp-z"), Name: aws.String("Support West")},
						{Id: aws.String("rp-lower"), Arn: aws.String("arn:rp-lower"), Name: aws.String("support lower case")},
					},
					NextToken: aws.String("routing-page-2"),
				}, nil
			}
			if aws.ToString(input.NextToken) != "routing-page-2" {
				t.Fatalf("unexpected routing-profile continuation token %q", aws.ToString(input.NextToken))
			}
			return &awsconnect.ListRoutingProfilesOutput{RoutingProfileSummaryList: []connecttypes.RoutingProfileSummary{
				{Id: aws.String("rp-a"), Arn: aws.String("arn:rp-a"), Name: aws.String("Support East")},
			}}, nil
		},
	}

	nameContains := "Support"
	response, profiles := readConnectProfiles(t, true, client, &nameContains, nil)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected routing-profile read diagnostics: %v", response.Diagnostics)
	}
	if !reflect.DeepEqual(tokens, []string{"", "routing-page-2"}) {
		t.Fatalf("expected complete routing-profile pagination, got %v", tokens)
	}
	assertConnectProfileResults(t, profiles, []connectProfileDataSourceModel{
		{ID: stringValue("rp-a"), ARN: stringValue("arn:rp-a"), Name: stringValue("Support East")},
		{ID: stringValue("rp-z"), ARN: stringValue("arn:rp-z"), Name: stringValue("Support West")},
	})
}

func TestRoutingProfilesDataSourceSearchPaginatesAndMapsTagFilter(t *testing.T) {
	var tokens []string
	tags := map[string]string{"zone": "west", "team": "ops"}
	client := &fakeConnectProfilesClient{
		searchRoutingProfiles: func(_ context.Context, input *awsconnect.SearchRoutingProfilesInput) (*awsconnect.SearchRoutingProfilesOutput, error) {
			assertProfileListRequest(t, input.InstanceId, input.MaxResults, input.NextToken, aws.String(profilesDataSourceTestInstanceID), tokens)
			assertProfileTagFilter(t, input.SearchFilter.TagFilter, tags)
			tokens = append(tokens, aws.ToString(input.NextToken))
			if input.NextToken == nil {
				return &awsconnect.SearchRoutingProfilesOutput{
					RoutingProfiles: []connecttypes.RoutingProfile{
						{RoutingProfileId: aws.String("rp-z"), RoutingProfileArn: aws.String("arn:rp-z"), Name: aws.String("Ops West")},
						{RoutingProfileId: aws.String("rp-lower"), RoutingProfileArn: aws.String("arn:rp-lower"), Name: aws.String("ops lower case")},
					},
					NextToken: aws.String("routing-search-page-2"),
				}, nil
			}
			if aws.ToString(input.NextToken) != "routing-search-page-2" {
				t.Fatalf("unexpected routing-profile search token %q", aws.ToString(input.NextToken))
			}
			return &awsconnect.SearchRoutingProfilesOutput{RoutingProfiles: []connecttypes.RoutingProfile{
				{RoutingProfileId: aws.String("rp-a"), RoutingProfileArn: aws.String("arn:rp-a"), Name: aws.String("Ops East")},
			}}, nil
		},
	}

	nameContains := "Ops"
	response, profiles := readConnectProfiles(t, true, client, &nameContains, tags)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected routing-profile search diagnostics: %v", response.Diagnostics)
	}
	if !reflect.DeepEqual(tokens, []string{"", "routing-search-page-2"}) {
		t.Fatalf("expected complete routing-profile search pagination, got %v", tokens)
	}
	assertConnectProfileResults(t, profiles, []connectProfileDataSourceModel{
		{ID: stringValue("rp-a"), ARN: stringValue("arn:rp-a"), Name: stringValue("Ops East")},
		{ID: stringValue("rp-z"), ARN: stringValue("arn:rp-z"), Name: stringValue("Ops West")},
	})
}

func TestSecurityProfilesDataSourceListPaginatesFiltersAndMapsResults(t *testing.T) {
	var tokens []string
	client := &fakeConnectProfilesClient{
		listSecurityProfiles: func(_ context.Context, input *awsconnect.ListSecurityProfilesInput) (*awsconnect.ListSecurityProfilesOutput, error) {
			assertProfileListRequest(t, input.InstanceId, input.MaxResults, input.NextToken, aws.String(profilesDataSourceTestInstanceID), tokens)
			tokens = append(tokens, aws.ToString(input.NextToken))
			if input.NextToken == nil {
				return &awsconnect.ListSecurityProfilesOutput{
					SecurityProfileSummaryList: []connecttypes.SecurityProfileSummary{
						{Id: aws.String("sp-z"), Arn: aws.String("arn:sp-z"), Name: aws.String("Agent Support West")},
						{Id: aws.String("sp-lower"), Arn: aws.String("arn:sp-lower"), Name: aws.String("agent support lower case")},
					},
					NextToken: aws.String("security-page-2"),
				}, nil
			}
			if aws.ToString(input.NextToken) != "security-page-2" {
				t.Fatalf("unexpected security-profile continuation token %q", aws.ToString(input.NextToken))
			}
			return &awsconnect.ListSecurityProfilesOutput{SecurityProfileSummaryList: []connecttypes.SecurityProfileSummary{
				{Id: aws.String("sp-a"), Arn: aws.String("arn:sp-a"), Name: aws.String("Agent Support East")},
			}}, nil
		},
	}

	nameContains := "Agent Support"
	response, profiles := readConnectProfiles(t, false, client, &nameContains, nil)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected security-profile read diagnostics: %v", response.Diagnostics)
	}
	if !reflect.DeepEqual(tokens, []string{"", "security-page-2"}) {
		t.Fatalf("expected complete security-profile pagination, got %v", tokens)
	}
	assertConnectProfileResults(t, profiles, []connectProfileDataSourceModel{
		{ID: stringValue("sp-a"), ARN: stringValue("arn:sp-a"), Name: stringValue("Agent Support East")},
		{ID: stringValue("sp-z"), ARN: stringValue("arn:sp-z"), Name: stringValue("Agent Support West")},
	})
}

func TestSecurityProfilesDataSourceSearchPaginatesAndMapsTagFilter(t *testing.T) {
	var tokens []string
	tags := map[string]string{"zone": "west", "team": "ops"}
	client := &fakeConnectProfilesClient{
		searchSecurityProfiles: func(_ context.Context, input *awsconnect.SearchSecurityProfilesInput) (*awsconnect.SearchSecurityProfilesOutput, error) {
			assertProfileListRequest(t, input.InstanceId, input.MaxResults, input.NextToken, aws.String(profilesDataSourceTestInstanceID), tokens)
			assertProfileTagFilter(t, input.SearchFilter.TagFilter, tags)
			tokens = append(tokens, aws.ToString(input.NextToken))
			if input.NextToken == nil {
				return &awsconnect.SearchSecurityProfilesOutput{
					SecurityProfiles: []connecttypes.SecurityProfileSearchSummary{
						{Id: aws.String("sp-z"), Arn: aws.String("arn:sp-z"), SecurityProfileName: aws.String("Ops West")},
						{Id: aws.String("sp-lower"), Arn: aws.String("arn:sp-lower"), SecurityProfileName: aws.String("ops lower case")},
					},
					NextToken: aws.String("security-search-page-2"),
				}, nil
			}
			if aws.ToString(input.NextToken) != "security-search-page-2" {
				t.Fatalf("unexpected security-profile search token %q", aws.ToString(input.NextToken))
			}
			return &awsconnect.SearchSecurityProfilesOutput{SecurityProfiles: []connecttypes.SecurityProfileSearchSummary{
				{Id: aws.String("sp-a"), Arn: aws.String("arn:sp-a"), SecurityProfileName: aws.String("Ops East")},
			}}, nil
		},
	}

	nameContains := "Ops"
	response, profiles := readConnectProfiles(t, false, client, &nameContains, tags)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected security-profile search diagnostics: %v", response.Diagnostics)
	}
	if !reflect.DeepEqual(tokens, []string{"", "security-search-page-2"}) {
		t.Fatalf("expected complete security-profile search pagination, got %v", tokens)
	}
	assertConnectProfileResults(t, profiles, []connectProfileDataSourceModel{
		{ID: stringValue("sp-a"), ARN: stringValue("arn:sp-a"), Name: stringValue("Ops East")},
		{ID: stringValue("sp-z"), ARN: stringValue("arn:sp-z"), Name: stringValue("Ops West")},
	})
}

func TestConnectProfilesDataSourcesReturnEmptyResults(t *testing.T) {
	tests := []struct {
		name    string
		routing bool
		client  *fakeConnectProfilesClient
	}{
		{name: "routing", routing: true, client: &fakeConnectProfilesClient{}},
		{name: "security", client: &fakeConnectProfilesClient{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, profiles := readConnectProfiles(t, test.routing, test.client, nil, nil)
			if response.Diagnostics.HasError() {
				t.Fatalf("unexpected empty-result diagnostics: %v", response.Diagnostics)
			}
			if len(profiles) != 0 {
				t.Fatalf("expected no profiles, got %#v", profiles)
			}
		})
	}
}

func TestConnectProfilesDataSourcesReportListAndSearchErrors(t *testing.T) {
	apiError := errors.New("access denied")
	tests := []struct {
		name            string
		routing         bool
		tags            map[string]string
		configureClient func(*fakeConnectProfilesClient)
		wantSummary     string
		wantOperation   string
	}{
		{
			name:          "routing list",
			routing:       true,
			wantSummary:   "Unable to Read Routing Profiles",
			wantOperation: "list routing profiles",
			configureClient: func(client *fakeConnectProfilesClient) {
				client.listRoutingProfiles = func(context.Context, *awsconnect.ListRoutingProfilesInput) (*awsconnect.ListRoutingProfilesOutput, error) {
					return nil, apiError
				}
			},
		},
		{
			name:          "routing search",
			routing:       true,
			tags:          map[string]string{"team": "ops"},
			wantSummary:   "Unable to Read Routing Profiles",
			wantOperation: "search routing profiles",
			configureClient: func(client *fakeConnectProfilesClient) {
				client.searchRoutingProfiles = func(context.Context, *awsconnect.SearchRoutingProfilesInput) (*awsconnect.SearchRoutingProfilesOutput, error) {
					return nil, apiError
				}
			},
		},
		{
			name:          "security list",
			wantSummary:   "Unable to Read Security Profiles",
			wantOperation: "list security profiles",
			configureClient: func(client *fakeConnectProfilesClient) {
				client.listSecurityProfiles = func(context.Context, *awsconnect.ListSecurityProfilesInput) (*awsconnect.ListSecurityProfilesOutput, error) {
					return nil, apiError
				}
			},
		},
		{
			name:          "security search",
			tags:          map[string]string{"team": "ops"},
			wantSummary:   "Unable to Read Security Profiles",
			wantOperation: "search security profiles",
			configureClient: func(client *fakeConnectProfilesClient) {
				client.searchSecurityProfiles = func(context.Context, *awsconnect.SearchSecurityProfilesInput) (*awsconnect.SearchSecurityProfilesOutput, error) {
					return nil, apiError
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeConnectProfilesClient{}
			test.configureClient(client)
			response, _ := readConnectProfiles(t, test.routing, client, nil, test.tags)
			if !response.Diagnostics.HasError() {
				t.Fatal("expected an API error diagnostic")
			}
			diagnostic := response.Diagnostics[0]
			if diagnostic.Summary() != test.wantSummary {
				t.Fatalf("expected diagnostic summary %q, got %q", test.wantSummary, diagnostic.Summary())
			}
			if !strings.Contains(diagnostic.Detail(), test.wantOperation) || !strings.Contains(diagnostic.Detail(), apiError.Error()) {
				t.Fatalf("expected diagnostic detail to mention %q and %q, got %q", test.wantOperation, apiError, diagnostic.Detail())
			}
		})
	}
}

func readConnectProfiles(t *testing.T, routing bool, client *fakeConnectProfilesClient, nameContains *string, tags map[string]string) (*datasource.ReadResponse, []connectProfileDataSourceModel) {
	t.Helper()
	ctx := context.Background()
	var implementation datasource.DataSource
	profilesAttribute := "security_profiles"
	if routing {
		implementation = &routingProfilesDataSource{client: client}
		profilesAttribute = "routing_profiles"
	} else {
		implementation = &securityProfilesDataSource{client: client}
	}

	schemaResponse := &datasource.SchemaResponse{}
	implementation.Schema(ctx, datasource.SchemaRequest{}, schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected data source schema diagnostics: %v", schemaResponse.Diagnostics)
	}

	raw := profilesReadRawValue(profilesAttribute, nameContains, tags)
	request := datasource.ReadRequest{Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema}}
	response := &datasource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: schemaResponse.Schema}}
	implementation.Read(ctx, request, response)
	if response.Diagnostics.HasError() {
		return response, nil
	}

	var profiles []connectProfileDataSourceModel
	if routing {
		var data routingProfilesDataSourceModel
		response.Diagnostics.Append(response.State.Get(ctx, &data)...)
		profiles = data.RoutingProfiles
	} else {
		var data securityProfilesDataSourceModel
		response.Diagnostics.Append(response.State.Get(ctx, &data)...)
		profiles = data.SecurityProfiles
	}
	return response, profiles
}

func profilesReadRawValue(profilesAttribute string, nameContains *string, tags map[string]string) tftypes.Value {
	profileObjectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":   tftypes.String,
		"arn":  tftypes.String,
		"name": tftypes.String,
	}}
	tagsType := tftypes.Map{ElementType: tftypes.String}
	profileListType := tftypes.List{ElementType: profileObjectType}
	attributeTypes := map[string]tftypes.Type{
		"instance_id":     tftypes.String,
		"name_contains":   tftypes.String,
		"tags":            tagsType,
		profilesAttribute: profileListType,
	}
	nameValue := tftypes.NewValue(tftypes.String, nil)
	if nameContains != nil {
		nameValue = tftypes.NewValue(tftypes.String, *nameContains)
	}
	tagValues := tftypes.NewValue(tagsType, nil)
	if tags != nil {
		values := make(map[string]tftypes.Value, len(tags))
		for key, value := range tags {
			values[key] = tftypes.NewValue(tftypes.String, value)
		}
		tagValues = tftypes.NewValue(tagsType, values)
	}
	return tftypes.NewValue(tftypes.Object{AttributeTypes: attributeTypes}, map[string]tftypes.Value{
		"instance_id":     tftypes.NewValue(tftypes.String, profilesDataSourceTestInstanceID),
		"name_contains":   nameValue,
		"tags":            tagValues,
		profilesAttribute: tftypes.NewValue(profileListType, nil),
	})
}

func assertProfileListRequest(t *testing.T, instanceID *string, maxResults *int32, nextToken *string, expectedInstanceID *string, seenTokens []string) {
	t.Helper()
	if aws.ToString(instanceID) != aws.ToString(expectedInstanceID) {
		t.Fatalf("unexpected profile-list instance ID %q", aws.ToString(instanceID))
	}
	if aws.ToInt32(maxResults) != connectProfilesPerPage {
		t.Fatalf("expected profile page size %d, got %d", connectProfilesPerPage, aws.ToInt32(maxResults))
	}
	if len(seenTokens) == 0 && nextToken != nil {
		t.Fatalf("expected first profile page to omit its token, got %q", aws.ToString(nextToken))
	}
	if len(seenTokens) > 0 && (nextToken == nil || aws.ToString(nextToken) == "") {
		t.Fatal("expected a continuation token for the next profile page")
	}
}

func assertProfileTagFilter(t *testing.T, filter *connecttypes.ControlPlaneTagFilter, expected map[string]string) {
	t.Helper()
	if filter == nil {
		t.Fatal("expected tag filter")
	}
	keys := []string{"team", "zone"}
	if len(expected) != len(keys) || len(filter.AndConditions) != len(keys) {
		t.Fatalf("expected %d AND tag conditions, got %#v", len(keys), filter.AndConditions)
	}
	for index, key := range keys {
		condition := filter.AndConditions[index]
		if aws.ToString(condition.TagKey) != key || aws.ToString(condition.TagValue) != expected[key] {
			t.Fatalf("unexpected tag condition at index %d: %#v", index, condition)
		}
	}
}

func assertConnectProfileResults(t *testing.T, got, want []connectProfileDataSourceModel) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d profiles, got %#v", len(want), got)
	}
	for index, expected := range want {
		actual := got[index]
		if actual.ID.ValueString() != expected.ID.ValueString() || actual.ARN.ValueString() != expected.ARN.ValueString() || actual.Name.ValueString() != expected.Name.ValueString() {
			t.Fatalf("unexpected profile at index %d: got %#v, want %#v", index, actual, expected)
		}
	}
}

func stringValue(value string) types.String {
	return types.StringValue(value)
}
