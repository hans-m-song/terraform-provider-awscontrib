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
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	maxPhoneNumberPrefixLength = 11
	maxPhoneNumbersPerPage     = 1000
)

var _ datasource.DataSource = &phoneNumberDataSource{}
var _ datasource.DataSourceWithConfigure = &phoneNumberDataSource{}

type phoneNumberClient interface {
	ListPhoneNumbersV2(context.Context, *awsconnect.ListPhoneNumbersV2Input, ...func(*awsconnect.Options)) (*awsconnect.ListPhoneNumbersV2Output, error)
}

type phoneNumberDataSource struct {
	client phoneNumberClient
}

type phoneNumberDataSourceModel struct {
	InstanceID           types.String `tfsdk:"instance_id"`
	PhoneNumber          types.String `tfsdk:"phone_number"`
	ID                   types.String `tfsdk:"id"`
	ARN                  types.String `tfsdk:"arn"`
	CountryCode          types.String `tfsdk:"country_code"`
	Type                 types.String `tfsdk:"type"`
	Description          types.String `tfsdk:"description"`
	SourcePhoneNumberARN types.String `tfsdk:"source_phone_number_arn"`
	TargetARN            types.String `tfsdk:"target_arn"`
}

type e164PhoneNumberValidator struct{}

func (e164PhoneNumberValidator) Description(context.Context) string {
	return "must be a full E.164 phone number"
}

func (e164PhoneNumberValidator) MarkdownDescription(context.Context) string {
	return "must be a full E.164 phone number"
}

func (e164PhoneNumberValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	if !isE164PhoneNumber(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Phone Number",
			fmt.Sprintf("phone_number %q must be a full E.164 phone number", req.ConfigValue.ValueString()),
		)
	}
}

func isE164PhoneNumber(value string) bool {
	if len(value) < 3 || len(value) > 16 || value[0] != '+' || value[1] < '1' || value[1] > '9' {
		return false
	}

	for _, character := range value[2:] {
		if character < '0' || character > '9' {
			return false
		}
	}

	return true
}

func NewPhoneNumberDataSource() datasource.DataSource {
	return &phoneNumberDataSource{}
}

func PhoneNumberDataSourceFactory() func() datasource.DataSource {
	return func() datasource.DataSource {
		return NewPhoneNumberDataSource()
	}
}

func (d *phoneNumberDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connect_phone_number"
}

func (d *phoneNumberDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		MarkdownDescription: "Looks up an Amazon Connect phone number by its full E.164 number.",
		Attributes: map[string]datasourceschema.Attribute{
			"instance_id": datasourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect instance identifier or ARN containing the phone number.",
				Required:            true,
			},
			"phone_number": datasourceschema.StringAttribute{
				MarkdownDescription: "Full phone number in E.164 format.",
				Required:            true,
				Validators:          []validator.String{e164PhoneNumberValidator{}},
			},
			"id": datasourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect phone number identifier.",
				Computed:            true,
			},
			"arn": datasourceschema.StringAttribute{
				MarkdownDescription: "Amazon Connect phone number ARN.",
				Computed:            true,
			},
			"country_code": datasourceschema.StringAttribute{
				MarkdownDescription: "ISO country code for the phone number.",
				Computed:            true,
			},
			"type": datasourceschema.StringAttribute{
				MarkdownDescription: "Phone number type.",
				Computed:            true,
			},
			"description": datasourceschema.StringAttribute{
				MarkdownDescription: "Phone number description, when configured.",
				Computed:            true,
			},
			"source_phone_number_arn": datasourceschema.StringAttribute{
				MarkdownDescription: "ARN of a phone number imported from an external service, when returned.",
				Computed:            true,
			},
			"target_arn": datasourceschema.StringAttribute{
				MarkdownDescription: "ARN of the Connect instance or traffic distribution group handling inbound traffic.",
				Computed:            true,
			},
		},
	}
}

func (d *phoneNumberDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *phoneNumberDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data phoneNumberDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError(
			"Amazon Connect Client Not Configured",
			"The provider did not configure an Amazon Connect client.",
		)
		return
	}

	if data.InstanceID.IsNull() || data.InstanceID.IsUnknown() || data.PhoneNumber.IsNull() || data.PhoneNumber.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("phone_number"),
			"Phone Number Not Known",
			"instance_id and phone_number must be known before the phone-number lookup can run",
		)
		return
	}

	phoneNumber := data.PhoneNumber.ValueString()
	matches, err := d.listPhoneNumbers(ctx, data.InstanceID.ValueString(), phoneNumber)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Read Connect Phone Number",
			fmt.Sprintf("Could not list phone numbers in instance %q for %q: %s", data.InstanceID.ValueString(), phoneNumber, err),
		)
		return
	}

	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError(
			"Phone Number Not Found",
			fmt.Sprintf("No Amazon Connect phone number exactly matched %q in instance %q.", phoneNumber, data.InstanceID.ValueString()),
		)
		return
	case 1:
		data = phoneNumberDataSourceModel{
			InstanceID:           data.InstanceID,
			PhoneNumber:          data.PhoneNumber,
			ID:                   types.StringPointerValue(matches[0].PhoneNumberId),
			ARN:                  types.StringPointerValue(matches[0].PhoneNumberArn),
			CountryCode:          phoneNumberCountryCodeValue(matches[0].PhoneNumberCountryCode),
			Type:                 phoneNumberTypeValue(matches[0].PhoneNumberType),
			Description:          types.StringPointerValue(matches[0].PhoneNumberDescription),
			SourcePhoneNumberARN: types.StringPointerValue(matches[0].SourcePhoneNumberArn),
			TargetARN:            types.StringPointerValue(matches[0].TargetArn),
		}
	default:
		resp.Diagnostics.AddError(
			"Ambiguous Phone Number",
			fmt.Sprintf("Amazon Connect returned %d exact matches for %q in instance %q; refusing to select one.", len(matches), phoneNumber, data.InstanceID.ValueString()),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *phoneNumberDataSource) listPhoneNumbers(ctx context.Context, instanceID, phoneNumber string) ([]connecttypes.ListPhoneNumbersSummary, error) {
	prefix := phoneNumberPrefix(phoneNumber)
	var nextToken *string
	seenTokens := make(map[string]struct{})
	var matches []connecttypes.ListPhoneNumbersSummary

	for {
		output, err := d.client.ListPhoneNumbersV2(ctx, &awsconnect.ListPhoneNumbersV2Input{
			InstanceId:        aws.String(instanceID),
			MaxResults:        aws.Int32(maxPhoneNumbersPerPage),
			NextToken:         nextToken,
			PhoneNumberPrefix: aws.String(prefix),
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return nil, errors.New("amazon connect returned an empty phone-number list response")
		}

		for _, summary := range output.ListPhoneNumbersSummaryList {
			if summary.PhoneNumber != nil && aws.ToString(summary.PhoneNumber) == phoneNumber {
				matches = append(matches, summary)
			}
		}

		if output.NextToken == nil || aws.ToString(output.NextToken) == "" {
			return matches, nil
		}
		token := aws.ToString(output.NextToken)
		if _, repeated := seenTokens[token]; repeated {
			return nil, fmt.Errorf("amazon connect returned duplicate phone-number pagination token %q", token)
		}
		seenTokens[token] = struct{}{}
		nextToken = output.NextToken
	}
}

func phoneNumberPrefix(phoneNumber string) string {
	phoneNumberRunes := []rune(phoneNumber)
	if len(phoneNumberRunes) <= maxPhoneNumberPrefixLength {
		return phoneNumber
	}
	return string(phoneNumberRunes[:maxPhoneNumberPrefixLength])
}

func phoneNumberCountryCodeValue(value connecttypes.PhoneNumberCountryCode) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(string(value))
}

func phoneNumberTypeValue(value connecttypes.PhoneNumberType) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(string(value))
}
