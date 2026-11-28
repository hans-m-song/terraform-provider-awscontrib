package connect

import (
	"context"
	"maps"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	connecttypes "github.com/aws/aws-sdk-go-v2/service/connect/types"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func discoveryTagFilter(ctx context.Context, tags types.Map) (*connecttypes.ControlPlaneTagFilter, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	if tags.IsUnknown() {
		diagnostics.AddError("Unknown tag filter", "The tags filter must be known before discovery.")
		return nil, diagnostics
	}
	if tags.IsNull() || len(tags.Elements()) == 0 {
		return nil, diagnostics
	}
	var values map[string]types.String
	diagnostics.Append(tags.ElementsAs(ctx, &values, false)...)
	if diagnostics.HasError() {
		return nil, diagnostics
	}
	filter := &connecttypes.ControlPlaneTagFilter{}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		value := values[key]
		if key == "" || value.IsNull() || value.IsUnknown() {
			diagnostics.AddError("Invalid tag filter", "Each tag filter must have a nonempty key and a known string value.")
			return nil, diagnostics
		}
		filter.AndConditions = append(filter.AndConditions, connecttypes.TagCondition{TagKey: aws.String(key), TagValue: value.ValueStringPointer()})
	}
	return filter, diagnostics
}
