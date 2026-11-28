package connect

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type quickConnectsTestClient struct {
	list   func(*awsconnect.ListQuickConnectsInput) (*awsconnect.ListQuickConnectsOutput, error)
	search func(*awsconnect.SearchQuickConnectsInput) (*awsconnect.SearchQuickConnectsOutput, error)
}

func (f *quickConnectsTestClient) ListQuickConnects(_ context.Context, input *awsconnect.ListQuickConnectsInput, _ ...func(*awsconnect.Options)) (*awsconnect.ListQuickConnectsOutput, error) {
	if f.list == nil {
		return nil, errors.New("unexpected ListQuickConnects call")
	}
	return f.list(input)
}

func (f *quickConnectsTestClient) SearchQuickConnects(_ context.Context, input *awsconnect.SearchQuickConnectsInput, _ ...func(*awsconnect.Options)) (*awsconnect.SearchQuickConnectsOutput, error) {
	if f.search == nil {
		return nil, errors.New("unexpected SearchQuickConnects call")
	}
	return f.search(input)
}

func quickConnectsReadRequest(t *testing.T, quickConnectTypes []string, nameContains *string, tags map[string]string) (datasource.ReadRequest, *datasource.ReadResponse) {
	t.Helper()

	ctx := context.Background()
	schemaResponse := &datasource.SchemaResponse{}
	(&quickConnectsDataSource{}).Schema(ctx, datasource.SchemaRequest{}, schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", schemaResponse.Diagnostics)
	}

	typeValues := types.SetNull(types.StringType)
	if quickConnectTypes != nil {
		values := make([]attr.Value, 0, len(quickConnectTypes))
		for _, value := range quickConnectTypes {
			values = append(values, types.StringValue(value))
		}
		var diagnostics diag.Diagnostics
		typeValues, diagnostics = types.SetValue(types.StringType, values)
		if diagnostics.HasError() {
			t.Fatalf("unexpected quick-connect type conversion diagnostics: %v", diagnostics)
		}
	}

	nameValue := types.StringNull()
	if nameContains != nil {
		nameValue = types.StringValue(*nameContains)
	}
	tagValues := types.MapNull(types.StringType)
	if tags != nil {
		values := make(map[string]attr.Value, len(tags))
		for key, value := range tags {
			values[key] = types.StringValue(value)
		}
		var diagnostics diag.Diagnostics
		tagValues, diagnostics = types.MapValue(types.StringType, values)
		if diagnostics.HasError() {
			t.Fatalf("unexpected tag conversion diagnostics: %v", diagnostics)
		}
	}

	model := quickConnectsDataSourceModel{
		InstanceID:        types.StringValue("instance-id"),
		QuickConnectTypes: typeValues,
		NameContains:      nameValue,
		Tags:              tagValues,
	}
	rootAttributes := make(map[string]attr.Type, len(schemaResponse.Schema.Attributes))
	for name, attribute := range schemaResponse.Schema.Attributes {
		rootAttributes[name] = attribute.GetType()
	}
	var value attr.Value
	diagnostics := tfsdk.ValueFrom(ctx, model, types.ObjectType{AttrTypes: rootAttributes}, &value)
	if diagnostics.HasError() {
		t.Fatalf("unexpected config conversion diagnostics: %v", diagnostics)
	}
	raw, err := value.ToTerraformValue(ctx)
	if err != nil {
		t.Fatalf("unexpected Terraform value conversion error: %v", err)
	}

	return datasource.ReadRequest{Config: tfsdk.Config{Raw: raw, Schema: schemaResponse.Schema}}, &datasource.ReadResponse{
		State: tfsdk.State{Raw: raw, Schema: schemaResponse.Schema},
	}
}

func quickConnectsTestSummary(id, name string, quickConnectType connecttypes.QuickConnectType) connecttypes.QuickConnectSummary {
	return connecttypes.QuickConnectSummary{
		Id:               aws.String(id),
		Arn:              aws.String("arn:aws:connect:region:000000000000:instance/instance-id/quick-connect/" + id),
		Name:             aws.String(name),
		QuickConnectType: quickConnectType,
	}
}

func quickConnectsTestSearchResult(id, name string, quickConnectType connecttypes.QuickConnectType) connecttypes.QuickConnect {
	return connecttypes.QuickConnect{
		QuickConnectId:  aws.String(id),
		QuickConnectARN: aws.String("arn:aws:connect:region:000000000000:instance/instance-id/quick-connect/" + id),
		Name:            aws.String(name),
		QuickConnectConfig: &connecttypes.QuickConnectConfig{
			QuickConnectType: quickConnectType,
		},
	}
}

func quickConnectsState(t *testing.T, response *datasource.ReadResponse) quickConnectsDataSourceModel {
	t.Helper()
	var state quickConnectsDataSourceModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected state diagnostics: %v", response.Diagnostics)
	}
	return state
}

func quickConnectIDs(summaries []quickConnectSummaryDataSourceModel) []string {
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		ids = append(ids, summary.ID.ValueString())
	}
	return ids
}

func TestQuickConnectsDataSourceReadListsFiltersAndPaginates(t *testing.T) {
	nameContains := "Target"
	var tokens []string
	client := &quickConnectsTestClient{list: func(input *awsconnect.ListQuickConnectsInput) (*awsconnect.ListQuickConnectsOutput, error) {
		tokens = append(tokens, aws.ToString(input.NextToken))
		if aws.ToString(input.InstanceId) != "instance-id" || aws.ToInt32(input.MaxResults) != maxQuickConnectsPerListPage {
			t.Fatalf("unexpected ListQuickConnects input: %#v", input)
		}
		gotTypes := make([]string, 0, len(input.QuickConnectTypes))
		for _, value := range input.QuickConnectTypes {
			gotTypes = append(gotTypes, string(value))
		}
		if !reflect.DeepEqual(gotTypes, []string{"QUEUE", "USER"}) {
			t.Fatalf("unexpected API type filter: %v", gotTypes)
		}

		if input.NextToken == nil {
			return &awsconnect.ListQuickConnectsOutput{
				QuickConnectSummaryList: []connecttypes.QuickConnectSummary{
					quickConnectsTestSummary("qc-z", "Target queue", connecttypes.QuickConnectTypeQueue),
					quickConnectsTestSummary("qc-lower", "target user", connecttypes.QuickConnectTypeUser),
					quickConnectsTestSummary("qc-flow", "Target flow", connecttypes.QuickConnectTypeFlow),
				},
				NextToken: aws.String("next-page"),
			}, nil
		}
		if aws.ToString(input.NextToken) != "next-page" {
			t.Fatalf("unexpected pagination token %q", aws.ToString(input.NextToken))
		}
		return &awsconnect.ListQuickConnectsOutput{QuickConnectSummaryList: []connecttypes.QuickConnectSummary{
			quickConnectsTestSummary("qc-a", "Target user", connecttypes.QuickConnectTypeUser),
			quickConnectsTestSummary("qc-b", "Target queue", connecttypes.QuickConnectTypeQueue),
		}}, nil
	}}
	request, response := quickConnectsReadRequest(t, []string{"USER", "QUEUE"}, &nameContains, nil)
	(&quickConnectsDataSource{client: client}).Read(context.Background(), request, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", response.Diagnostics)
	}
	if !reflect.DeepEqual(tokens, []string{"", "next-page"}) {
		t.Fatalf("unexpected pagination calls: %v", tokens)
	}

	state := quickConnectsState(t, response)
	if got, want := quickConnectIDs(state.QuickConnects), []string{"qc-a", "qc-b", "qc-z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected filtered and sorted quick connects: got %v, want %v", got, want)
	}
	if state.QuickConnects[0].Name.ValueString() != "Target user" || state.QuickConnects[0].QuickConnectType.ValueString() != "USER" || state.QuickConnects[0].ARN.IsNull() {
		t.Fatalf("unexpected quick-connect summary mapping: %#v", state.QuickConnects[0])
	}
}

func TestQuickConnectsDataSourceReadSearchesTagsFiltersAndPaginates(t *testing.T) {
	nameContains := "Sales"
	var tokens []string
	client := &quickConnectsTestClient{search: func(input *awsconnect.SearchQuickConnectsInput) (*awsconnect.SearchQuickConnectsOutput, error) {
		tokens = append(tokens, aws.ToString(input.NextToken))
		if aws.ToString(input.InstanceId) != "instance-id" || aws.ToInt32(input.MaxResults) != maxQuickConnectsPerSearchPage {
			t.Fatalf("unexpected SearchQuickConnects input: %#v", input)
		}
		if input.SearchFilter == nil || input.SearchFilter.TagFilter == nil {
			t.Fatalf("expected a tag filter, got %#v", input.SearchFilter)
		}
		conditions := input.SearchFilter.TagFilter.AndConditions
		if len(conditions) != 2 || aws.ToString(conditions[0].TagKey) != "env" || aws.ToString(conditions[0].TagValue) != "prod" || aws.ToString(conditions[1].TagKey) != "team" || aws.ToString(conditions[1].TagValue) != "support" {
			t.Fatalf("unexpected AND tag conditions: %#v", conditions)
		}

		if input.NextToken == nil {
			return &awsconnect.SearchQuickConnectsOutput{
				QuickConnects: []connecttypes.QuickConnect{
					quickConnectsTestSearchResult("qc-d", "Sales East", connecttypes.QuickConnectTypeQueue),
					quickConnectsTestSearchResult("qc-user", "Sales Agent", connecttypes.QuickConnectTypeUser),
					quickConnectsTestSearchResult("qc-case", "sales west", connecttypes.QuickConnectTypeQueue),
				},
				NextToken: aws.String("search-page"),
			}, nil
		}
		if aws.ToString(input.NextToken) != "search-page" {
			t.Fatalf("unexpected pagination token %q", aws.ToString(input.NextToken))
		}
		return &awsconnect.SearchQuickConnectsOutput{QuickConnects: []connecttypes.QuickConnect{
			quickConnectsTestSearchResult("qc-b", "PreSales", connecttypes.QuickConnectTypeQueue),
			quickConnectsTestSearchResult("qc-phone", "Sales phone", connecttypes.QuickConnectTypePhoneNumber),
		}}, nil
	}}
	request, response := quickConnectsReadRequest(t, []string{"QUEUE"}, &nameContains, map[string]string{"team": "support", "env": "prod"})
	(&quickConnectsDataSource{client: client}).Read(context.Background(), request, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", response.Diagnostics)
	}
	if !reflect.DeepEqual(tokens, []string{"", "search-page"}) {
		t.Fatalf("unexpected pagination calls: %v", tokens)
	}

	state := quickConnectsState(t, response)
	if got, want := quickConnectIDs(state.QuickConnects), []string{"qc-b", "qc-d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected filtered and sorted quick connects: got %v, want %v", got, want)
	}
}

func TestQuickConnectsDataSourceReadReturnsEmptyList(t *testing.T) {
	client := &quickConnectsTestClient{list: func(*awsconnect.ListQuickConnectsInput) (*awsconnect.ListQuickConnectsOutput, error) {
		return &awsconnect.ListQuickConnectsOutput{}, nil
	}}
	request, response := quickConnectsReadRequest(t, nil, nil, nil)
	(&quickConnectsDataSource{client: client}).Read(context.Background(), request, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", response.Diagnostics)
	}
	state := quickConnectsState(t, response)
	if state.QuickConnects == nil || len(state.QuickConnects) != 0 {
		t.Fatalf("expected a known empty quick-connect list, got %#v", state.QuickConnects)
	}
}

func TestQuickConnectsDataSourceReadReportsListAndSearchFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tags   map[string]string
		client *quickConnectsTestClient
		want   string
	}{
		{
			name: "list API error",
			client: &quickConnectsTestClient{list: func(*awsconnect.ListQuickConnectsInput) (*awsconnect.ListQuickConnectsOutput, error) {
				return nil, errors.New("list unavailable")
			}},
			want: "list quick connects request failed: list unavailable",
		},
		{
			name: "list nil response",
			client: &quickConnectsTestClient{list: func(*awsconnect.ListQuickConnectsInput) (*awsconnect.ListQuickConnectsOutput, error) {
				return nil, nil
			}},
			want: "list quick connects returned a nil response",
		},
		{
			name: "list repeated pagination token",
			client: &quickConnectsTestClient{list: func(*awsconnect.ListQuickConnectsInput) (*awsconnect.ListQuickConnectsOutput, error) {
				return &awsconnect.ListQuickConnectsOutput{NextToken: aws.String("repeat")}, nil
			}},
			want: "repeated pagination token from ListQuickConnects",
		},
		{
			name: "search API error",
			tags: map[string]string{"team": "support"},
			client: &quickConnectsTestClient{search: func(*awsconnect.SearchQuickConnectsInput) (*awsconnect.SearchQuickConnectsOutput, error) {
				return nil, errors.New("search unavailable")
			}},
			want: "search quick connects request failed: search unavailable",
		},
		{
			name: "search nil response",
			tags: map[string]string{"team": "support"},
			client: &quickConnectsTestClient{search: func(*awsconnect.SearchQuickConnectsInput) (*awsconnect.SearchQuickConnectsOutput, error) {
				return nil, nil
			}},
			want: "search quick connects returned a nil response",
		},
		{
			name: "search repeated pagination token",
			tags: map[string]string{"team": "support"},
			client: &quickConnectsTestClient{search: func(*awsconnect.SearchQuickConnectsInput) (*awsconnect.SearchQuickConnectsOutput, error) {
				return &awsconnect.SearchQuickConnectsOutput{NextToken: aws.String("repeat")}, nil
			}},
			want: "repeated pagination token from SearchQuickConnects",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, response := quickConnectsReadRequest(t, nil, nil, tc.tags)
			(&quickConnectsDataSource{client: tc.client}).Read(context.Background(), request, response)
			if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Unable to List Quick Connects" {
				t.Fatalf("expected list diagnostic, got %v", response.Diagnostics)
			}
			if !strings.Contains(response.Diagnostics[0].Detail(), tc.want) {
				t.Fatalf("expected error detail containing %q, got %q", tc.want, response.Diagnostics[0].Detail())
			}
		})
	}
}

func TestQuickConnectsDataSourceReadFormatsModificationTime(t *testing.T) {
	modified := time.Date(2026, time.November, 28, 10, 20, 30, 123000000, time.FixedZone("test", 3600))
	client := &quickConnectsTestClient{list: func(*awsconnect.ListQuickConnectsInput) (*awsconnect.ListQuickConnectsOutput, error) {
		result := quickConnectsTestSummary("qc-1", "Target", connecttypes.QuickConnectTypeQueue)
		result.LastModifiedTime = &modified
		return &awsconnect.ListQuickConnectsOutput{QuickConnectSummaryList: []connecttypes.QuickConnectSummary{result}}, nil
	}}
	request, response := quickConnectsReadRequest(t, nil, nil, nil)
	(&quickConnectsDataSource{client: client}).Read(context.Background(), request, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", response.Diagnostics)
	}
	state := quickConnectsState(t, response)
	if got, want := state.QuickConnects[0].LastModifiedTime.ValueString(), modified.UTC().Format(time.RFC3339Nano); got != want {
		t.Fatalf("unexpected modification time: got %q, want %q", got, want)
	}
}
