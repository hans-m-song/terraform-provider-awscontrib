package connect

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/hans-m-song/terraform-provider-awscontrib/internal/conns"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestDataTableEffectiveTagsMergeDefaultsAndFilterIgnoredKeys(t *testing.T) {
	config := conns.TagConfig{
		DefaultTags:       map[string]string{"team": "support", "color": "blue", "ignored": "default", "org:managed": "default"},
		IgnoreKeys:        []string{"ignored"},
		IgnoreKeyPrefixes: []string{"org:"},
	}
	configured := dataTableStringMapValue(map[string]string{"color": "red", "owner": "terraform", "ignored": "resource"})
	effective, known, diagnostics := dataTableEffectiveTagsFromTerraform(configured, config)
	want := map[string]string{"team": "support", "color": "red", "owner": "terraform"}
	if !known || diagnostics.HasError() || !reflect.DeepEqual(effective, want) {
		t.Fatalf("unexpected effective tags %v known=%v diagnostics=%v", effective, known, diagnostics)
	}
}

func TestDataTableConfigureCopiesProviderTagConfig(t *testing.T) {
	implementation := &dataTableResource{}
	providerClient := &conns.Client{TagConfig: conns.TagConfig{
		DefaultTags:       map[string]string{"team": "support"},
		IgnoreKeys:        []string{"external"},
		IgnoreKeyPrefixes: []string{"org:"},
	}}
	response := &resource.ConfigureResponse{}
	implementation.Configure(context.Background(), resource.ConfigureRequest{ProviderData: providerClient}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected configure diagnostics: %v", response.Diagnostics)
	}
	providerClient.TagConfig.DefaultTags["team"] = "mutated"
	providerClient.TagConfig.IgnoreKeys[0] = "mutated"
	providerClient.TagConfig.IgnoreKeyPrefixes[0] = "changed:"
	if implementation.tagConfig.DefaultTags["team"] != "support" || implementation.tagConfig.IgnoreKeys[0] != "external" || implementation.tagConfig.IgnoreKeyPrefixes[0] != "org:" {
		t.Fatalf("resource must own an immutable copy of provider tag config: %#v", implementation.tagConfig)
	}
}

func TestDataTableTagsUnknownValuesDeferAndNullValuesFail(t *testing.T) {
	_, known, diagnostics := dataTableEffectiveTagsFromTerraform(types.MapUnknown(types.StringType), conns.TagConfig{})
	if known || diagnostics.HasError() {
		t.Fatalf("unknown tag map must defer without diagnostics, known=%v diagnostics=%v", known, diagnostics)
	}
	unknownElement := types.MapValueMust(types.StringType, map[string]attr.Value{"team": types.StringUnknown()})
	_, known, diagnostics = dataTableEffectiveTagsFromTerraform(unknownElement, conns.TagConfig{})
	if known || diagnostics.HasError() {
		t.Fatalf("unknown tag element must defer without diagnostics, known=%v diagnostics=%v", known, diagnostics)
	}

	value := types.MapValueMust(types.StringType, map[string]attr.Value{"team": types.StringNull()})
	_, known, diagnostics = dataTableEffectiveTagsFromTerraform(value, conns.TagConfig{})
	if !known || !diagnostics.HasError() {
		t.Fatalf("null tag value must fail before mutation, known=%v diagnostics=%v", known, diagnostics)
	}

	tooLong := strings.Repeat("v", maxDataTableTagValueSize+1)
	value = dataTableStringMapValue(map[string]string{"team": tooLong})
	_, known, diagnostics = dataTableEffectiveTagsFromTerraform(value, conns.TagConfig{})
	if !known || !diagnostics.HasError() {
		t.Fatalf("oversized tag value must fail validation, known=%v diagnostics=%v", known, diagnostics)
	}
	tooLongKey := strings.Repeat("k", maxDataTableTagKeySize+1)
	value = dataTableStringMapValue(map[string]string{tooLongKey: "value"})
	_, known, diagnostics = dataTableEffectiveTagsFromTerraform(value, conns.TagConfig{})
	if !known || !diagnostics.HasError() {
		t.Fatalf("oversized tag key must fail validation, known=%v diagnostics=%v", known, diagnostics)
	}
	tooMany := make(map[string]string, maxDataTableTags+1)
	for index := 0; index <= maxDataTableTags; index++ {
		tooMany[fmt.Sprintf("key-%d", index)] = "value"
	}
	_, known, diagnostics = dataTableEffectiveTagsFromTerraform(dataTableStringMapValue(tooMany), conns.TagConfig{})
	if !known || !diagnostics.HasError() {
		t.Fatalf("more than %d tags must fail validation, known=%v diagnostics=%v", maxDataTableTags, known, diagnostics)
	}
}

func TestDataTableDefaultTagValidationUsesProviderDiagnostic(t *testing.T) {
	_, known, diagnostics := dataTableEffectiveTagsFromTerraform(types.MapNull(types.StringType), conns.TagConfig{
		DefaultTags: map[string]string{"invalid*key": "value"},
	})
	if !known || !diagnostics.HasError() {
		t.Fatalf("invalid provider defaults must fail validation, known=%v diagnostics=%v", known, diagnostics)
	}
	if _, hasPath := diagnostics[0].(diag.DiagnosticWithPath); hasPath || !strings.Contains(diagnostics[0].Detail(), "default_tags.tags") {
		t.Fatalf("default tag failure must identify provider configuration without a resource attribute path: %#v", diagnostics[0])
	}

	_, _, explicitDiagnostics := dataTableEffectiveTagsFromTerraform(dataTableStringMapValue(map[string]string{"invalid*key": "value"}), conns.TagConfig{})
	if len(explicitDiagnostics) == 0 {
		t.Fatalf("explicit tag failure must remain path-specific: %v", explicitDiagnostics)
	}
	if _, hasPath := explicitDiagnostics[0].(diag.DiagnosticWithPath); !hasPath {
		t.Fatalf("explicit tag failure must retain its resource attribute path: %v", explicitDiagnostics)
	}
}

func TestDataTableModifyPlanComputesMergedTagsWithoutChangingTags(t *testing.T) {
	implementation := &dataTableResource{tagConfig: conns.TagConfig{DefaultTags: map[string]string{"team": "support", "owner": "default"}}}
	planned := sampleDataTableModel(nil, nil)
	planned.Tags = dataTableStringMapValue(map[string]string{"owner": "resource"})
	planned.TagsAll = types.MapUnknown(types.StringType)
	plan := dataTablePlan(t, planned)
	state := tfsdk.State{Raw: tftypes.NewValue(plan.Raw.Type(), nil), Schema: plan.Schema}
	response := &resource.ModifyPlanResponse{Plan: plan}
	implementation.ModifyPlan(context.Background(), resource.ModifyPlanRequest{State: state, Plan: plan}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected modify-plan diagnostics: %v", response.Diagnostics)
	}
	var modified dataTableModel
	response.Diagnostics.Append(response.Plan.Get(context.Background(), &modified)...)
	wantTags := map[string]string{"owner": "resource"}
	wantTagsAll := map[string]string{"team": "support", "owner": "resource"}
	if response.Diagnostics.HasError() || !reflect.DeepEqual(mapStringValues(modified.Tags), wantTags) || !reflect.DeepEqual(mapStringValues(modified.TagsAll), wantTagsAll) {
		t.Fatalf("modify plan changed configured tags or omitted defaults: tags=%v tags_all=%v diagnostics=%v", mapStringValues(modified.Tags), mapStringValues(modified.TagsAll), response.Diagnostics)
	}
}

func TestDataTableModifyPlanDefersUnknownTagsAndUnknownsTagsAll(t *testing.T) {
	implementation := &dataTableResource{tagConfig: conns.TagConfig{DefaultTags: map[string]string{"team": "support"}}}
	planned := sampleDataTableModel(nil, nil)
	planned.Tags = types.MapUnknown(types.StringType)
	planned.TagsAll = dataTableStringMapValue(map[string]string{"stale": "value"})
	plan := dataTablePlan(t, planned)
	state := tfsdk.State{Raw: tftypes.NewValue(plan.Raw.Type(), nil), Schema: plan.Schema}
	response := &resource.ModifyPlanResponse{Plan: plan}
	implementation.ModifyPlan(context.Background(), resource.ModifyPlanRequest{State: state, Plan: plan}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unknown tags should defer without diagnostics: %v", response.Diagnostics)
	}
	var modified dataTableModel
	response.Diagnostics.Append(response.Plan.Get(context.Background(), &modified)...)
	if response.Diagnostics.HasError() || !modified.Tags.IsUnknown() || !modified.TagsAll.IsUnknown() {
		t.Fatalf("unknown tags should leave tags unknown and defer tags_all: tags=%v tags_all=%v diagnostics=%v", modified.Tags, modified.TagsAll, response.Diagnostics)
	}
}

func TestDataTableCreateUsesMergedTagsAndPreservesNullTags(t *testing.T) {
	var requestTags map[string]string
	remote := sampleRemoteDataTable()
	client := &fakeDataTableClient{
		createTable: func(_ context.Context, input *awsconnect.CreateDataTableInput) (*awsconnect.CreateDataTableOutput, error) {
			requestTags = make(map[string]string, len(input.Tags))
			for key, value := range input.Tags {
				requestTags[key] = value
			}
			remote.Tags = requestTags
			return &awsconnect.CreateDataTableOutput{Id: aws.String(dataTableTestID), Arn: aws.String("arn:test")}, nil
		},
		describeTable: func(context.Context, *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
			return &awsconnect.DescribeDataTableOutput{DataTable: remote}, nil
		},
	}
	implementation := &dataTableResource{
		client:      client,
		coordinator: newDataTableCoordinator(),
		tagConfig:   conns.TagConfig{DefaultTags: map[string]string{"team": "support", "owner": "default"}},
	}
	planned := sampleDataTableModel(nil, nil)
	planned.Tags = types.MapNull(types.StringType)
	response := &resource.CreateResponse{State: dataTableSchemaOnlyState(t)}
	implementation.Create(context.Background(), resource.CreateRequest{Plan: dataTablePlan(t, planned)}, response)
	want := map[string]string{"team": "support", "owner": "default"}
	if response.Diagnostics.HasError() || !reflect.DeepEqual(requestTags, want) {
		t.Fatalf("create should send merged defaults, tags=%v diagnostics=%v", requestTags, response.Diagnostics)
	}
	var state dataTableModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	if response.Diagnostics.HasError() || !state.Tags.IsNull() || !reflect.DeepEqual(mapStringValues(state.TagsAll), want) {
		t.Fatalf("create should retain null tags and report effective tags_all, tags=%v tags_all=%v diagnostics=%v", state.Tags, mapStringValues(state.TagsAll), response.Diagnostics)
	}
}

func TestDataTableUpdateReconcilesOnlyTagsUnderTableLock(t *testing.T) {
	remote := sampleRemoteDataTable()
	remote.Description = aws.String("description")
	remote.Tags = map[string]string{"environment": "old", "remove": "gone", "external": "external", "aws:service": "system"}
	var operations []string
	metadataCalls := 0
	client := &fakeDataTableClient{
		describeTable: func(context.Context, *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
			return &awsconnect.DescribeDataTableOutput{DataTable: remote}, nil
		},
		updateMetadata: func(context.Context, *awsconnect.UpdateDataTableMetadataInput) (*awsconnect.UpdateDataTableMetadataOutput, error) {
			metadataCalls++
			return &awsconnect.UpdateDataTableMetadataOutput{}, nil
		},
		untagResource: func(_ context.Context, input *awsconnect.UntagResourceInput) (*awsconnect.UntagResourceOutput, error) {
			operations = append(operations, "untag")
			if !reflect.DeepEqual(input.TagKeys, []string{"remove"}) {
				t.Fatalf("unexpected removed tag keys %v", input.TagKeys)
			}
			for _, key := range input.TagKeys {
				delete(remote.Tags, key)
			}
			return &awsconnect.UntagResourceOutput{}, nil
		},
		tagResource: func(_ context.Context, input *awsconnect.TagResourceInput) (*awsconnect.TagResourceOutput, error) {
			operations = append(operations, "tag")
			want := map[string]string{"environment": "new", "owner": ""}
			if !reflect.DeepEqual(input.Tags, want) {
				t.Fatalf("unexpected changed and added tags %v", input.Tags)
			}
			for key, value := range input.Tags {
				remote.Tags[key] = value
			}
			return &awsconnect.TagResourceOutput{}, nil
		},
	}
	implementation := &dataTableResource{
		client:      client,
		coordinator: newDataTableCoordinator(),
		tagConfig:   conns.TagConfig{DefaultTags: map[string]string{"environment": "new"}, IgnoreKeys: []string{"external"}},
	}
	prior := sampleDataTableModel(nil, nil)
	prior.Description = types.StringValue("description")
	prior.TagsAll = dataTableStringMapValue(map[string]string{"environment": "old", "remove": "gone"})
	planned := sampleDataTableModel(nil, nil)
	planned.Description = types.StringValue("description")
	planned.Tags = dataTableStringMapValue(map[string]string{"owner": ""})
	response := &resource.UpdateResponse{State: dataTableState(t, prior)}
	implementation.Update(context.Background(), resource.UpdateRequest{State: dataTableState(t, prior), Plan: dataTablePlan(t, planned)}, response)
	if response.Diagnostics.HasError() || !reflect.DeepEqual(operations, []string{"untag", "tag"}) || metadataCalls != 0 {
		t.Fatalf("tag-only update should avoid metadata mutation and remove before add, operations=%v metadata_calls=%d diagnostics=%v", operations, metadataCalls, response.Diagnostics)
	}
	if remote.Tags["external"] != "external" || remote.Tags["aws:service"] != "system" {
		t.Fatalf("ignored and AWS system tags must remain untouched: %v", remote.Tags)
	}
	var state dataTableModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	wantTagsAll := map[string]string{"environment": "new", "owner": ""}
	if response.Diagnostics.HasError() || !reflect.DeepEqual(mapStringValues(state.Tags), map[string]string{"owner": ""}) || !reflect.DeepEqual(mapStringValues(state.TagsAll), wantTagsAll) {
		t.Fatalf("update state should preserve configured tags and effective tags_all: tags=%v tags_all=%v diagnostics=%v", mapStringValues(state.Tags), mapStringValues(state.TagsAll), response.Diagnostics)
	}
}

func TestDataTableUpdateRemovesFormerDefaultWhenTagsAreOmitted(t *testing.T) {
	remote := sampleRemoteDataTable()
	remote.Tags = map[string]string{"inherited": "old default"}
	untagCalls := 0
	tagCalls := 0
	metadataCalls := 0
	client := &fakeDataTableClient{
		describeTable: func(context.Context, *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
			return &awsconnect.DescribeDataTableOutput{DataTable: remote}, nil
		},
		updateMetadata: func(context.Context, *awsconnect.UpdateDataTableMetadataInput) (*awsconnect.UpdateDataTableMetadataOutput, error) {
			metadataCalls++
			return &awsconnect.UpdateDataTableMetadataOutput{}, nil
		},
		untagResource: func(_ context.Context, input *awsconnect.UntagResourceInput) (*awsconnect.UntagResourceOutput, error) {
			untagCalls++
			if aws.ToString(input.ResourceArn) != "arn:test" || !reflect.DeepEqual(input.TagKeys, []string{"inherited"}) {
				t.Fatalf("unexpected former default removal request %#v", input)
			}
			delete(remote.Tags, "inherited")
			return &awsconnect.UntagResourceOutput{}, nil
		},
		tagResource: func(context.Context, *awsconnect.TagResourceInput) (*awsconnect.TagResourceOutput, error) {
			tagCalls++
			return nil, errors.New("unexpected tag request")
		},
	}
	implementation := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
	prior := sampleDataTableModel(nil, nil)
	prior.Description = types.StringValue("description")
	prior.TagsAll = dataTableStringMapValue(map[string]string{"inherited": "old default"})
	planned := sampleDataTableModel(nil, nil)
	planned.Description = types.StringValue("description")
	planned.Tags = types.MapNull(types.StringType)
	response := &resource.UpdateResponse{State: dataTableState(t, prior)}
	implementation.Update(context.Background(), resource.UpdateRequest{State: dataTableState(t, prior), Plan: dataTablePlan(t, planned)}, response)
	if response.Diagnostics.HasError() || untagCalls != 1 || tagCalls != 0 || metadataCalls != 0 {
		t.Fatalf("removing provider defaults should only untag inherited keys: untag=%d tag=%d metadata=%d diagnostics=%v", untagCalls, tagCalls, metadataCalls, response.Diagnostics)
	}
	var state dataTableModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &state)...)
	if response.Diagnostics.HasError() || !state.Tags.IsNull() || state.TagsAll.IsNull() || state.TagsAll.IsUnknown() || len(state.TagsAll.Elements()) != 0 {
		t.Fatalf("former default should be absent while omitted tags remain null: tags=%v tags_all=%v diagnostics=%v", state.Tags, mapStringValues(state.TagsAll), response.Diagnostics)
	}
}

func TestDataTableUnknownTagsFailBeforeMutation(t *testing.T) {
	metadataCalls := 0
	client := &fakeDataTableClient{
		updateMetadata: func(context.Context, *awsconnect.UpdateDataTableMetadataInput) (*awsconnect.UpdateDataTableMetadataOutput, error) {
			metadataCalls++
			return &awsconnect.UpdateDataTableMetadataOutput{}, nil
		},
	}
	implementation := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
	prior := sampleDataTableModel(nil, nil)
	planned := sampleDataTableModel(nil, nil)
	planned.Tags = types.MapUnknown(types.StringType)
	response := &resource.UpdateResponse{State: dataTableState(t, prior)}
	implementation.Update(context.Background(), resource.UpdateRequest{State: dataTableState(t, prior), Plan: dataTablePlan(t, planned)}, response)
	if !response.Diagnostics.HasError() || metadataCalls != 0 {
		t.Fatalf("unknown tags must fail before all API mutations, calls=%d diagnostics=%v", metadataCalls, response.Diagnostics)
	}
}

func TestDataTableReadRecoversAfterPartialTagMutationFailure(t *testing.T) {
	apiFailure := errors.New("tag API failed")
	remote := sampleRemoteDataTable()
	remote.Description = aws.String("description")
	remote.Tags = map[string]string{"remove": "gone", "keep": "value"}
	client := &fakeDataTableClient{
		describeTable: func(context.Context, *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
			return &awsconnect.DescribeDataTableOutput{DataTable: remote}, nil
		},
		untagResource: func(_ context.Context, input *awsconnect.UntagResourceInput) (*awsconnect.UntagResourceOutput, error) {
			for _, key := range input.TagKeys {
				delete(remote.Tags, key)
			}
			return &awsconnect.UntagResourceOutput{}, nil
		},
		tagResource: func(context.Context, *awsconnect.TagResourceInput) (*awsconnect.TagResourceOutput, error) {
			return nil, apiFailure
		},
	}
	implementation := &dataTableResource{client: client, coordinator: newDataTableCoordinator()}
	prior := sampleDataTableModel(nil, nil)
	prior.Description = types.StringValue("description")
	planned := sampleDataTableModel(nil, nil)
	planned.Description = types.StringValue("description")
	planned.Tags = dataTableStringMapValue(map[string]string{"new": "value", "keep": "value"})
	updateResponse := &resource.UpdateResponse{State: dataTableState(t, prior)}
	implementation.Update(context.Background(), resource.UpdateRequest{State: dataTableState(t, prior), Plan: dataTablePlan(t, planned)}, updateResponse)
	if !updateResponse.Diagnostics.HasError() || !strings.Contains(updateResponse.Diagnostics.Errors()[0].Detail(), apiFailure.Error()) {
		t.Fatalf("partial tag failure should preserve its API cause, diagnostics=%v", updateResponse.Diagnostics)
	}
	readResponse := &resource.ReadResponse{State: dataTableState(t, prior)}
	implementation.Read(context.Background(), resource.ReadRequest{State: dataTableState(t, prior)}, readResponse)
	if readResponse.Diagnostics.HasError() {
		t.Fatalf("refresh after partial tag failure should recover remote state: %v", readResponse.Diagnostics)
	}
	var recovered dataTableModel
	readResponse.Diagnostics.Append(readResponse.State.Get(context.Background(), &recovered)...)
	want := map[string]string{"keep": "value"}
	if readResponse.Diagnostics.HasError() || !reflect.DeepEqual(mapStringValues(recovered.TagsAll), want) || !reflect.DeepEqual(mapStringValues(recovered.Tags), want) {
		t.Fatalf("read should expose tags remaining after partial mutation, tags=%v tags_all=%v diagnostics=%v", mapStringValues(recovered.Tags), mapStringValues(recovered.TagsAll), readResponse.Diagnostics)
	}
}

func TestDataTableTagsForReadFiltersDefaultsSystemAndIgnoredTags(t *testing.T) {
	config := conns.TagConfig{
		DefaultTags: map[string]string{"owner": "platform"},
		IgnoreKeys:  []string{"external"},
	}
	remote := map[string]string{"owner": "platform", "local": "table", "external": "owned elsewhere", "aws:createdBy": "service"}
	imported := dataTableTagsForRead(types.MapNull(types.StringType), remote, config)
	if got := mapStringValues(imported); !reflect.DeepEqual(got, map[string]string{"local": "table"}) {
		t.Fatalf("import should adopt only non-default managed tags, got %v", got)
	}
	if imported.IsNull() {
		t.Fatal("imported non-default tags must be represented in state")
	}

	prior := dataTableStringMapValue(map[string]string{"owner": "platform"})
	refreshed := dataTableTagsForRead(prior, remote, config)
	if got := mapStringValues(refreshed); !reflect.DeepEqual(got, map[string]string{"owner": "platform", "local": "table"}) {
		t.Fatalf("explicit tag equal to a default must stay in tags, got %v", got)
	}

	missingDefault := dataTableTagsForRead(prior, map[string]string{"local": "table"}, config)
	if got := mapStringValues(missingDefault); !reflect.DeepEqual(got, map[string]string{"local": "table"}) {
		t.Fatalf("missing remote default must not be fabricated from prior state, got %v", got)
	}

	if empty := dataTableTagsForRead(types.MapNull(types.StringType), nil, config); !empty.IsNull() {
		t.Fatalf("an omitted empty tags map must remain null, got %#v", empty)
	}
	if empty := dataTableTagsForRead(dataTableStringMapValue(map[string]string{}), nil, config); empty.IsNull() || len(empty.Elements()) != 0 {
		t.Fatalf("an explicitly empty tags map must remain empty, got %#v", empty)
	}
}

func TestDataTableImportReadAdoptsTagsWithoutMutation(t *testing.T) {
	remote := sampleRemoteDataTable()
	remote.Tags = map[string]string{
		"owner":         "platform",
		"local":         "table",
		"external":      "managed elsewhere",
		"aws:createdBy": "service",
	}
	client := &fakeDataTableClient{
		describeTable: func(context.Context, *awsconnect.DescribeDataTableInput) (*awsconnect.DescribeDataTableOutput, error) {
			return &awsconnect.DescribeDataTableOutput{DataTable: remote}, nil
		},
		tagResource: func(context.Context, *awsconnect.TagResourceInput) (*awsconnect.TagResourceOutput, error) {
			t.Fatal("import refresh must not call TagResource")
			return nil, nil
		},
		untagResource: func(context.Context, *awsconnect.UntagResourceInput) (*awsconnect.UntagResourceOutput, error) {
			t.Fatal("import refresh must not call UntagResource")
			return nil, nil
		},
	}
	implementation := &dataTableResource{
		client:      client,
		coordinator: newDataTableCoordinator(),
		tagConfig:   conns.TagConfig{DefaultTags: map[string]string{"owner": "platform"}, IgnoreKeys: []string{"external"}},
	}
	state := dataTableState(t, sampleDataTableModel(nil, nil))
	importResponse := &resource.ImportStateResponse{State: state}
	implementation.ImportState(context.Background(), resource.ImportStateRequest{ID: dataTableTestInstanceID + ":" + dataTableTestID}, importResponse)
	if importResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected import diagnostics: %v", importResponse.Diagnostics)
	}
	readResponse := &resource.ReadResponse{State: importResponse.State}
	implementation.Read(context.Background(), resource.ReadRequest{State: importResponse.State}, readResponse)
	if readResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected import refresh diagnostics: %v", readResponse.Diagnostics)
	}
	var adopted dataTableModel
	readResponse.Diagnostics.Append(readResponse.State.Get(context.Background(), &adopted)...)
	wantTags := map[string]string{"local": "table"}
	wantTagsAll := map[string]string{"owner": "platform", "local": "table"}
	if readResponse.Diagnostics.HasError() || !reflect.DeepEqual(mapStringValues(adopted.Tags), wantTags) || !reflect.DeepEqual(mapStringValues(adopted.TagsAll), wantTagsAll) {
		t.Fatalf("import should adopt remote non-default tags only: tags=%v tags_all=%v diagnostics=%v", mapStringValues(adopted.Tags), mapStringValues(adopted.TagsAll), readResponse.Diagnostics)
	}
}

func TestReconcileDataTableTagsRemovesThenAddsAndPreservesIgnoredTags(t *testing.T) {
	var operations []string
	client := &fakeDataTableClient{
		untagResource: func(_ context.Context, input *awsconnect.UntagResourceInput) (*awsconnect.UntagResourceOutput, error) {
			operations = append(operations, "untag")
			if aws.ToString(input.ResourceArn) != "arn:test" || !reflect.DeepEqual(input.TagKeys, []string{"remove"}) {
				t.Fatalf("unexpected untag request %#v", input)
			}
			return &awsconnect.UntagResourceOutput{}, nil
		},
		tagResource: func(_ context.Context, input *awsconnect.TagResourceInput) (*awsconnect.TagResourceOutput, error) {
			operations = append(operations, "tag")
			if aws.ToString(input.ResourceArn) != "arn:test" || !reflect.DeepEqual(input.Tags, map[string]string{"add": "", "change": "new"}) {
				t.Fatalf("unexpected tag request %#v", input)
			}
			return &awsconnect.TagResourceOutput{}, nil
		},
	}
	remote := map[string]string{"remove": "old", "change": "old", "keep": "same", "external": "managed elsewhere", "aws:service": "system"}
	desired := map[string]string{"add": "", "change": "new", "keep": "same"}
	config := conns.TagConfig{IgnoreKeys: []string{"external"}}
	if err := reconcileDataTableTags(context.Background(), client, "arn:test", remote, desired, config); err != nil {
		t.Fatalf("unexpected reconciliation failure: %v", err)
	}
	if !reflect.DeepEqual(operations, []string{"untag", "tag"}) {
		t.Fatalf("expected remove-before-add operations, got %v", operations)
	}
}

func TestReconcileDataTableTagsNoOpAndPreservesAPIErrorCause(t *testing.T) {
	client := &fakeDataTableClient{
		tagResource: func(context.Context, *awsconnect.TagResourceInput) (*awsconnect.TagResourceOutput, error) {
			return nil, errors.New("unexpected tag request")
		},
		untagResource: func(context.Context, *awsconnect.UntagResourceInput) (*awsconnect.UntagResourceOutput, error) {
			return nil, errors.New("unexpected untag request")
		},
	}
	if err := reconcileDataTableTags(context.Background(), client, "arn:test", map[string]string{"same": "value"}, map[string]string{"same": "value"}, conns.TagConfig{}); err != nil {
		t.Fatalf("identical tags must be a no-op: %v", err)
	}

	apiFailure := errors.New("tag API failed")
	client = &fakeDataTableClient{tagResource: func(context.Context, *awsconnect.TagResourceInput) (*awsconnect.TagResourceOutput, error) {
		return nil, apiFailure
	}}
	err := reconcileDataTableTags(context.Background(), client, "arn:test", nil, map[string]string{"new": "value"}, conns.TagConfig{})
	if !errors.Is(err, apiFailure) {
		t.Fatalf("tag API cause must be retained, got %v", err)
	}
}
