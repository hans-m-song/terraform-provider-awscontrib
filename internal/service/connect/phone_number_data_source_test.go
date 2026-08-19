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
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const (
	phoneDataSourceInstanceID = "00000000-0000-0000-0000-000000000020"
	phoneNumberOne            = "+123456789012345"
	phoneNumberTwo            = "+123456789012346"
)

type fakePhoneNumberClient struct {
	list func(context.Context, *awsconnect.ListPhoneNumbersV2Input) (*awsconnect.ListPhoneNumbersV2Output, error)
}

func (f *fakePhoneNumberClient) ListPhoneNumbersV2(ctx context.Context, input *awsconnect.ListPhoneNumbersV2Input, _ ...func(*awsconnect.Options)) (*awsconnect.ListPhoneNumbersV2Output, error) {
	if f.list == nil {
		return &awsconnect.ListPhoneNumbersV2Output{}, nil
	}
	return f.list(ctx, input)
}

type fakePhoneNumberClientFactory struct {
	client *awsconnect.Client
}

func (f fakePhoneNumberClientFactory) Connect() *awsconnect.Client {
	return f.client
}

func newTestPhoneNumberDataSource(client phoneNumberClient) *phoneNumberDataSource {
	return &phoneNumberDataSource{client: client}
}

func TestPhoneNumberDataSourceMetadataUsesTypeName(t *testing.T) {
	response := &datasource.MetadataResponse{}
	NewPhoneNumberDataSource().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "awscontrib"}, response)
	if response.TypeName != "awscontrib_connect_phone_number" {
		t.Fatalf("unexpected data source type %q", response.TypeName)
	}
}

func TestPhoneNumberDataSourceSchema(t *testing.T) {
	response := &datasource.SchemaResponse{}
	NewPhoneNumberDataSource().Schema(context.Background(), datasource.SchemaRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}

	for _, name := range []string{"instance_id", "phone_number"} {
		attribute, ok := response.Schema.Attributes[name].(datasourceschema.StringAttribute)
		if !ok {
			t.Fatalf("expected %s to be a string attribute", name)
		}
		if !attribute.Required || attribute.Computed {
			t.Fatalf("expected %s to be required and not computed: %#v", name, attribute)
		}
	}
	phoneNumberAttribute, ok := response.Schema.Attributes["phone_number"].(datasourceschema.StringAttribute)
	if !ok {
		t.Fatal("expected phone_number to be a string attribute")
	}
	if len(phoneNumberAttribute.Validators) != 1 {
		t.Fatalf("expected phone_number E.164 validator, got %d", len(phoneNumberAttribute.Validators))
	}

	for _, name := range []string{"id", "arn", "country_code", "type", "description", "source_phone_number_arn", "target_arn"} {
		attribute, ok := response.Schema.Attributes[name].(datasourceschema.StringAttribute)
		if !ok {
			t.Fatalf("expected %s to be a string attribute", name)
		}
		if !attribute.Computed || attribute.Required || attribute.Optional {
			t.Fatalf("expected %s to be computed only: %#v", name, attribute)
		}
	}
}

func TestPhoneNumberDataSourceConfigureAcceptsNilProviderData(t *testing.T) {
	implementation := NewPhoneNumberDataSource()
	configurable, ok := implementation.(datasource.DataSourceWithConfigure)
	if !ok {
		t.Fatal("expected configurable data source")
	}
	response := &datasource.ConfigureResponse{}
	configurable.Configure(context.Background(), datasource.ConfigureRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected nil provider data diagnostic: %v", response.Diagnostics)
	}
}

func TestPhoneNumberDataSourceConfigureAcceptsClientFactory(t *testing.T) {
	implementation := NewPhoneNumberDataSource()
	dataSource, ok := implementation.(*phoneNumberDataSource)
	if !ok {
		t.Fatal("expected phone number data source implementation")
	}
	response := &datasource.ConfigureResponse{}
	dataSource.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: fakePhoneNumberClientFactory{client: &awsconnect.Client{}}}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected typed provider data diagnostic: %v", response.Diagnostics)
	}
	if dataSource.client == nil {
		t.Fatal("expected configured Connect client")
	}
}

func TestPhoneNumberDataSourceConfigureRejectsUnexpectedProviderData(t *testing.T) {
	implementation := NewPhoneNumberDataSource()
	configurable, ok := implementation.(datasource.DataSourceWithConfigure)
	if !ok {
		t.Fatal("expected configurable data source")
	}
	response := &datasource.ConfigureResponse{}
	configurable.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: "unexpected"}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected unexpected provider data diagnostic")
	}
}

func TestPhoneNumberPrefixCapsAtAPIMaximum(t *testing.T) {
	if got := phoneNumberPrefix(phoneNumberOne); got != "+1234567890" {
		t.Fatalf("expected 11-character prefix, got %q", got)
	}
	if got := phoneNumberPrefix("+123"); got != "+123" {
		t.Fatalf("expected short phone number to remain unchanged, got %q", got)
	}
	if got := phoneNumberPrefix("+1234567890é"); got != "+1234567890" {
		t.Fatalf("expected rune-safe prefix cap, got %q", got)
	}
}

func TestPhoneNumberDataSourceListPaginatesAndExactMatches(t *testing.T) {
	var tokens []string
	client := &fakePhoneNumberClient{
		list: func(_ context.Context, input *awsconnect.ListPhoneNumbersV2Input) (*awsconnect.ListPhoneNumbersV2Output, error) {
			tokens = append(tokens, aws.ToString(input.NextToken))
			if input.NextToken == nil {
				if aws.ToString(input.InstanceId) != phoneDataSourceInstanceID {
					t.Fatalf("unexpected instance ID %q", aws.ToString(input.InstanceId))
				}
				if aws.ToString(input.PhoneNumberPrefix) != "+1234567890" {
					t.Fatalf("unexpected phone prefix %q", aws.ToString(input.PhoneNumberPrefix))
				}
				return &awsconnect.ListPhoneNumbersV2Output{
					ListPhoneNumbersSummaryList: []connecttypes.ListPhoneNumbersSummary{
						{PhoneNumber: aws.String(phoneNumberTwo)},
					},
					NextToken: aws.String("page-2"),
				}, nil
			}
			return &awsconnect.ListPhoneNumbersV2Output{
				ListPhoneNumbersSummaryList: []connecttypes.ListPhoneNumbersSummary{
					{PhoneNumber: aws.String(phoneNumberOne)},
				},
			}, nil
		},
	}

	matches, err := newTestPhoneNumberDataSource(client).listPhoneNumbers(context.Background(), phoneDataSourceInstanceID, phoneNumberOne)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(matches) != 1 || aws.ToString(matches[0].PhoneNumber) != phoneNumberOne {
		t.Fatalf("unexpected exact matches: %#v", matches)
	}
	if !reflect.DeepEqual(tokens, []string{"", "page-2"}) {
		t.Fatalf("expected complete pagination, got tokens %v", tokens)
	}
}

func TestPhoneNumberDataSourceReadMapsSummaryAndPreservesOptionalNulls(t *testing.T) {
	phoneNumberID := "phone-number-id"
	phoneNumberARN := "arn:aws:connect:ap-southeast-2:123456789012:phone-number/phone-number-id"
	description := "primary"
	sourceARN := "arn:aws:wisdom:ap-southeast-2:123456789012:phone-number/source"
	targetARN := "arn:aws:connect:ap-southeast-2:123456789012:instance/instance"
	client := &fakePhoneNumberClient{
		list: func(context.Context, *awsconnect.ListPhoneNumbersV2Input) (*awsconnect.ListPhoneNumbersV2Output, error) {
			return &awsconnect.ListPhoneNumbersV2Output{ListPhoneNumbersSummaryList: []connecttypes.ListPhoneNumbersSummary{{
				PhoneNumber:            aws.String(phoneNumberOne),
				PhoneNumberId:          aws.String(phoneNumberID),
				PhoneNumberArn:         aws.String(phoneNumberARN),
				PhoneNumberCountryCode: connecttypes.PhoneNumberCountryCodeAu,
				PhoneNumberType:        connecttypes.PhoneNumberTypeTollFree,
				PhoneNumberDescription: aws.String(description),
				SourcePhoneNumberArn:   aws.String(sourceARN),
				TargetArn:              aws.String(targetARN),
			}}}, nil
		},
	}

	dataSource := newTestPhoneNumberDataSource(client)
	response := &datasource.ReadResponse{State: phoneNumberState(t)}
	dataSource.Read(context.Background(), datasource.ReadRequest{Config: phoneNumberConfig(t)}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", response.Diagnostics)
	}

	var data phoneNumberDataSourceModel
	response.Diagnostics.Append(response.State.Get(context.Background(), &data)...)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected state decode diagnostics: %v", response.Diagnostics)
	}
	if data.ID.ValueString() != phoneNumberID || data.ARN.ValueString() != phoneNumberARN || data.CountryCode.ValueString() != "AU" || data.Type.ValueString() != "TOLL_FREE" || data.Description.ValueString() != description || data.SourcePhoneNumberARN.ValueString() != sourceARN || data.TargetARN.ValueString() != targetARN {
		t.Fatalf("unexpected mapped state: %#v", data)
	}
	if data.PhoneNumber.ValueString() != phoneNumberOne || data.InstanceID.ValueString() != phoneDataSourceInstanceID {
		t.Fatalf("expected configured lookup values in state: %#v", data)
	}

	nullClient := &fakePhoneNumberClient{
		list: func(context.Context, *awsconnect.ListPhoneNumbersV2Input) (*awsconnect.ListPhoneNumbersV2Output, error) {
			return &awsconnect.ListPhoneNumbersV2Output{ListPhoneNumbersSummaryList: []connecttypes.ListPhoneNumbersSummary{{
				PhoneNumber: aws.String(phoneNumberOne),
			}}}, nil
		},
	}
	nullResponse := &datasource.ReadResponse{State: phoneNumberState(t)}
	newTestPhoneNumberDataSource(nullClient).Read(context.Background(), datasource.ReadRequest{Config: phoneNumberConfig(t)}, nullResponse)
	if nullResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected null mapping diagnostics: %v", nullResponse.Diagnostics)
	}
	var nullData phoneNumberDataSourceModel
	nullResponse.Diagnostics.Append(nullResponse.State.Get(context.Background(), &nullData)...)
	if nullResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected null state decode diagnostics: %v", nullResponse.Diagnostics)
	}
	for name, value := range map[string]types.String{
		"id":                      nullData.ID,
		"arn":                     nullData.ARN,
		"country_code":            nullData.CountryCode,
		"type":                    nullData.Type,
		"description":             nullData.Description,
		"source_phone_number_arn": nullData.SourcePhoneNumberARN,
		"target_arn":              nullData.TargetARN,
	} {
		if !value.IsNull() {
			t.Errorf("expected absent %s to remain null, got %s", name, value)
		}
	}
}

func TestPhoneNumberDataSourceReadRejectsZeroExactMatches(t *testing.T) {
	client := &fakePhoneNumberClient{
		list: func(context.Context, *awsconnect.ListPhoneNumbersV2Input) (*awsconnect.ListPhoneNumbersV2Output, error) {
			return &awsconnect.ListPhoneNumbersV2Output{ListPhoneNumbersSummaryList: []connecttypes.ListPhoneNumbersSummary{{PhoneNumber: aws.String(phoneNumberTwo)}}}, nil
		},
	}
	response := &datasource.ReadResponse{State: phoneNumberState(t)}
	newTestPhoneNumberDataSource(client).Read(context.Background(), datasource.ReadRequest{Config: phoneNumberConfig(t)}, response)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Phone Number Not Found" {
		t.Fatalf("expected not-found diagnostic, got %v", response.Diagnostics)
	}
}

func TestPhoneNumberDataSourceReadRejectsMultipleExactMatches(t *testing.T) {
	client := &fakePhoneNumberClient{
		list: func(context.Context, *awsconnect.ListPhoneNumbersV2Input) (*awsconnect.ListPhoneNumbersV2Output, error) {
			return &awsconnect.ListPhoneNumbersV2Output{ListPhoneNumbersSummaryList: []connecttypes.ListPhoneNumbersSummary{
				{PhoneNumber: aws.String(phoneNumberOne), PhoneNumberId: aws.String("one")},
				{PhoneNumber: aws.String(phoneNumberOne), PhoneNumberId: aws.String("two")},
			}}, nil
		},
	}
	response := &datasource.ReadResponse{State: phoneNumberState(t)}
	newTestPhoneNumberDataSource(client).Read(context.Background(), datasource.ReadRequest{Config: phoneNumberConfig(t)}, response)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Ambiguous Phone Number" {
		t.Fatalf("expected ambiguity diagnostic, got %v", response.Diagnostics)
	}
}

func TestPhoneNumberDataSourceReadReportsAPIError(t *testing.T) {
	expectedError := errors.New("list failed")
	client := &fakePhoneNumberClient{
		list: func(context.Context, *awsconnect.ListPhoneNumbersV2Input) (*awsconnect.ListPhoneNumbersV2Output, error) {
			return nil, expectedError
		},
	}
	response := &datasource.ReadResponse{State: phoneNumberState(t)}
	newTestPhoneNumberDataSource(client).Read(context.Background(), datasource.ReadRequest{Config: phoneNumberConfig(t)}, response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected API error diagnostic")
	}
	if !strings.Contains(response.Diagnostics[0].Detail(), expectedError.Error()) {
		t.Fatalf("expected API error in diagnostic detail, got %q", response.Diagnostics[0].Detail())
	}
}

func TestPhoneNumberDataSourceReadRequiresConfiguredClient(t *testing.T) {
	response := &datasource.ReadResponse{State: phoneNumberState(t)}
	newTestPhoneNumberDataSource(nil).Read(context.Background(), datasource.ReadRequest{Config: phoneNumberConfig(t)}, response)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Amazon Connect Client Not Configured" {
		t.Fatalf("expected missing-client diagnostic, got %v", response.Diagnostics)
	}
}

func TestE164PhoneNumberValidator(t *testing.T) {
	validatorImplementation := e164PhoneNumberValidator{}
	validResponse := &validator.StringResponse{}
	validatorImplementation.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("phone_number"), ConfigValue: types.StringValue(phoneNumberOne)}, validResponse)
	if validResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected valid E.164 diagnostic: %v", validResponse.Diagnostics)
	}

	invalidResponse := &validator.StringResponse{}
	validatorImplementation.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("phone_number"), ConfigValue: types.StringValue("not-a-phone-number")}, invalidResponse)
	if !invalidResponse.Diagnostics.HasError() {
		t.Fatal("expected invalid E.164 diagnostic")
	}
}

func phoneNumberSchema(t *testing.T) datasourceschema.Schema {
	t.Helper()
	response := &datasource.SchemaResponse{}
	NewPhoneNumberDataSource().Schema(context.Background(), datasource.SchemaRequest{}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}
	return response.Schema
}

func phoneNumberConfig(t *testing.T) tfsdk.Config {
	t.Helper()
	schema := phoneNumberSchema(t)
	return tfsdk.Config{
		Raw:    phoneNumberRawValue(),
		Schema: schema,
	}
}

func phoneNumberState(t *testing.T) tfsdk.State {
	t.Helper()
	schema := phoneNumberSchema(t)
	return tfsdk.State{
		Raw:    phoneNumberRawValue(),
		Schema: schema,
	}
}

func phoneNumberRawValue() tftypes.Value {
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"instance_id":             tftypes.String,
		"phone_number":            tftypes.String,
		"id":                      tftypes.String,
		"arn":                     tftypes.String,
		"country_code":            tftypes.String,
		"type":                    tftypes.String,
		"description":             tftypes.String,
		"source_phone_number_arn": tftypes.String,
		"target_arn":              tftypes.String,
	}}
	return tftypes.NewValue(objectType, map[string]tftypes.Value{
		"instance_id":             tftypes.NewValue(tftypes.String, phoneDataSourceInstanceID),
		"phone_number":            tftypes.NewValue(tftypes.String, phoneNumberOne),
		"id":                      tftypes.NewValue(tftypes.String, nil),
		"arn":                     tftypes.NewValue(tftypes.String, nil),
		"country_code":            tftypes.NewValue(tftypes.String, nil),
		"type":                    tftypes.NewValue(tftypes.String, nil),
		"description":             tftypes.NewValue(tftypes.String, nil),
		"source_phone_number_arn": tftypes.NewValue(tftypes.String, nil),
		"target_arn":              tftypes.NewValue(tftypes.String, nil),
	})
}
