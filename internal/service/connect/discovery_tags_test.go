package connect

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDiscoveryTagFilterPreservesExplicitEmptyValue(t *testing.T) {
	tags := types.MapValueMust(types.StringType, map[string]attr.Value{"enabled": types.StringValue("")})
	filter, diags := discoveryTagFilter(t.Context(), tags)
	if diags.HasError() || filter == nil || len(filter.AndConditions) != 1 {
		t.Fatalf("expected one tag condition, got %+v, diagnostics: %v", filter, diags)
	}
	condition := filter.AndConditions[0]
	if condition.TagValue == nil || *condition.TagValue != "" {
		t.Fatalf("expected an explicit empty tag value, got %v", condition.TagValue)
	}
}

func TestDiscoveryTagFilterRejectsInvalidFilters(t *testing.T) {
	for name, tags := range map[string]types.Map{
		"unknown map":   types.MapUnknown(types.StringType),
		"null value":    types.MapValueMust(types.StringType, map[string]attr.Value{"team": types.StringNull()}),
		"unknown value": types.MapValueMust(types.StringType, map[string]attr.Value{"team": types.StringUnknown()}),
		"empty key":     types.MapValueMust(types.StringType, map[string]attr.Value{"": types.StringValue("team")}),
	} {
		t.Run(name, func(t *testing.T) {
			filter, diags := discoveryTagFilter(t.Context(), tags)
			if !diags.HasError() || filter != nil {
				t.Fatalf("expected an error and no tag filter, got %+v, diagnostics: %v", filter, diags)
			}
		})
	}
}
