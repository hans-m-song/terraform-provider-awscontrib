package connect

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconnect "github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/hans-m-song/terraform-provider-awscontrib/internal/conns"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	maxDataTableTags         = 50
	maxDataTableTagKeySize   = 128
	maxDataTableTagValueSize = 256
)

func cloneDataTableTagConfig(config conns.TagConfig) conns.TagConfig {
	cloned := conns.TagConfig{
		DefaultTags:       make(map[string]string, len(config.DefaultTags)),
		IgnoreKeys:        append([]string(nil), config.IgnoreKeys...),
		IgnoreKeyPrefixes: append([]string(nil), config.IgnoreKeyPrefixes...),
	}
	for key, value := range config.DefaultTags {
		cloned.DefaultTags[key] = value
	}
	return cloned
}

func dataTableEffectiveTagsFromTerraform(value types.Map, config conns.TagConfig) (map[string]string, bool, diag.Diagnostics) {
	if value.IsUnknown() {
		return nil, false, nil
	}

	explicit := make(map[string]string)
	if !value.IsNull() {
		for key, element := range value.Elements() {
			tagValue, ok := element.(types.String)
			if !ok || tagValue.IsUnknown() {
				return nil, false, nil
			}
			if tagValue.IsNull() {
				return nil, true, diag.Diagnostics{diag.NewAttributeErrorDiagnostic(path.Root("tags").AtMapKey(key), "Null Data Table Tag Value", "tag values must be known strings; null values cannot be sent to Amazon Connect")}
			}
			explicit[key] = tagValue.ValueString()
		}
	}

	var diagnostics diag.Diagnostics
	diagnostics.Append(validateDataTableDefaultTags(config.DefaultTags)...)
	diagnostics.Append(validateDataTableTags(explicit, path.Root("tags"))...)
	if diagnostics.HasError() {
		return nil, true, diagnostics
	}

	effective := make(map[string]string, len(config.DefaultTags)+len(explicit))
	for key, value := range config.DefaultTags {
		if !dataTableTagIgnored(key, config) {
			effective[key] = value
		}
	}
	for key, value := range explicit {
		if !dataTableTagIgnored(key, config) {
			effective[key] = value
		}
	}
	if len(effective) > maxDataTableTags {
		diagnostics.AddAttributeError(path.Root("tags"), "Too Many Effective Data Table Tags", fmt.Sprintf("Amazon Connect permits at most %d effective tags per data table; the merged default and resource tag set has %d", maxDataTableTags, len(effective)))
		return nil, true, diagnostics
	}
	return effective, true, nil
}

func validateDataTableDefaultTags(tags map[string]string) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	for _, validationDiagnostic := range validateDataTableTags(tags, path.Root("tags")) {
		diagnostics.AddError(validationDiagnostic.Summary(), fmt.Sprintf("Provider default_tags.tags: %s", validationDiagnostic.Detail()))
	}
	return diagnostics
}

func validateDataTableTags(tags map[string]string, attributePath path.Path) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	if len(tags) > maxDataTableTags {
		diagnostics.AddAttributeError(attributePath, "Too Many Data Table Tags", fmt.Sprintf("Amazon Connect permits at most %d tags per data table; the configured set has %d", maxDataTableTags, len(tags)))
	}
	for key, value := range tags {
		keyPath := attributePath.AtMapKey(key)
		switch {
		case key == "":
			diagnostics.AddAttributeError(keyPath, "Invalid Data Table Tag Key", "tag keys must contain at least one character")
		case strings.HasPrefix(key, "aws:"):
			diagnostics.AddAttributeError(keyPath, "Reserved Data Table Tag Key", "tag keys beginning with aws: are reserved by Amazon Connect")
		case utf8.RuneCountInString(key) > maxDataTableTagKeySize:
			diagnostics.AddAttributeError(keyPath, "Data Table Tag Key Too Long", fmt.Sprintf("tag keys must be no longer than %d characters", maxDataTableTagKeySize))
		case !validDataTableTagKey(key):
			diagnostics.AddAttributeError(keyPath, "Invalid Data Table Tag Key", "tag keys may contain Unicode letters, separators, and numbers, plus _ . : / = + - @")
		}
		if utf8.RuneCountInString(value) > maxDataTableTagValueSize {
			diagnostics.AddAttributeError(keyPath, "Data Table Tag Value Too Long", fmt.Sprintf("tag values must be no longer than %d characters", maxDataTableTagValueSize))
		}
	}
	return diagnostics
}

func validDataTableTagKey(key string) bool {
	for _, character := range key {
		if unicode.IsLetter(character) || unicode.IsNumber(character) || unicode.Is(unicode.Z, character) || strings.ContainsRune("_.:/=+-@", character) {
			continue
		}
		return false
	}
	return true
}

func dataTableTagIgnored(key string, config conns.TagConfig) bool {
	if strings.HasPrefix(key, "aws:") {
		return true
	}
	for _, ignored := range config.IgnoreKeys {
		if key == ignored {
			return true
		}
	}
	for _, prefix := range config.IgnoreKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func filterDataTableTags(tags map[string]string, config conns.TagConfig) map[string]string {
	filtered := make(map[string]string, len(tags))
	for key, value := range tags {
		if !dataTableTagIgnored(key, config) {
			filtered[key] = value
		}
	}
	return filtered
}

func dataTableStringMapValue(values map[string]string) types.Map {
	elements := make(map[string]attr.Value, len(values))
	for key, value := range values {
		elements[key] = types.StringValue(value)
	}
	return types.MapValueMust(types.StringType, elements)
}

func mapStringValues(value types.Map) map[string]string {
	result := make(map[string]string)
	if value.IsNull() || value.IsUnknown() {
		return result
	}
	for key, element := range value.Elements() {
		stringValue, ok := element.(types.String)
		if ok && !stringValue.IsNull() && !stringValue.IsUnknown() {
			result[key] = stringValue.ValueString()
		}
	}
	return result
}

func dataTableTagsForRead(prior types.Map, remote map[string]string, config conns.TagConfig) types.Map {
	filtered := filterDataTableTags(remote, config)
	defaults := filterDataTableTags(config.DefaultTags, config)
	result := make(map[string]string, len(filtered))
	for key, value := range filtered {
		if defaultValue, isDefault := defaults[key]; !isDefault || defaultValue != value {
			result[key] = value
		}
	}
	if !prior.IsNull() && !prior.IsUnknown() {
		for key, element := range prior.Elements() {
			priorValue, ok := element.(types.String)
			if !ok || priorValue.IsNull() || priorValue.IsUnknown() {
				continue
			}
			defaultValue, isDefault := defaults[key]
			remoteValue, exists := filtered[key]
			if isDefault && priorValue.ValueString() == defaultValue && exists && remoteValue == defaultValue {
				result[key] = defaultValue
			}
		}
	}
	if len(result) == 0 && prior.IsNull() {
		return types.MapNull(types.StringType)
	}
	return dataTableStringMapValue(result)
}

func reconcileDataTableTags(ctx context.Context, client dataTableClient, arn string, remote, desired map[string]string, config conns.TagConfig) error {
	current := filterDataTableTags(remote, config)
	removed := make([]string, 0)
	updated := make(map[string]string)
	for key := range current {
		if _, wanted := desired[key]; !wanted {
			removed = append(removed, key)
		}
	}
	for key, value := range desired {
		currentValue, exists := current[key]
		if !exists || currentValue != value {
			updated[key] = value
		}
	}
	if len(removed) == 0 && len(updated) == 0 {
		return nil
	}
	if arn == "" {
		return fmt.Errorf("cannot reconcile data-table tags because Amazon Connect returned no ARN")
	}
	sort.Strings(removed)
	for start := 0; start < len(removed); start += maxDataTableTags {
		end := min(start+maxDataTableTags, len(removed))
		keys := removed[start:end]
		if _, err := client.UntagResource(ctx, &awsconnect.UntagResourceInput{ResourceArn: aws.String(arn), TagKeys: keys}); err != nil {
			return fmt.Errorf("could not remove data-table tags %q: %w", keys, err)
		}
	}
	keys := make([]string, 0, len(updated))
	for key := range updated {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for start := 0; start < len(keys); start += maxDataTableTags {
		end := min(start+maxDataTableTags, len(keys))
		tags := make(map[string]string, end-start)
		for _, key := range keys[start:end] {
			tags[key] = updated[key]
		}
		if _, err := client.TagResource(ctx, &awsconnect.TagResourceInput{ResourceArn: aws.String(arn), Tags: tags}); err != nil {
			return fmt.Errorf("could not add or update data-table tags %q: %w", keys[start:end], err)
		}
	}
	return nil
}
