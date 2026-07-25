package insurance_usecase

import (
	"context"
	"sort"

	"vozko/domain/insurance"
)

type describeRequirementsUseCase struct {
	providers []insurance.QuoteProvider
}

func NewDescribeRequirementsUseCase(providers []insurance.QuoteProvider) insurance.DescribeQuoteRequirementsUseCase {
	return &describeRequirementsUseCase{
		providers: cloneProviders(providers),
	}
}

func (uc *describeRequirementsUseCase) Execute(ctx context.Context, policyType insurance.PolicyType, providerNames []insurance.InsuranceProvider) (insurance.RequirementSet, error) {
	if policyType == "" {
		return insurance.RequirementSet{}, insurance.ErrUnsupportedPolicyType
	}

	baseFields, err := insurance.RequiredFieldsForPolicy(policyType)
	if err != nil {
		return insurance.RequirementSet{}, err
	}

	selectedProviders := filterProvidersByPolicy(uc.providers, policyType, providerNames)
	if len(providerNames) > 0 && len(selectedProviders) == 0 {
		return insurance.RequirementSet{}, insurance.ErrNoProvidersForPolicy
	}

	var aggregated insurance.RequirementSet
	if len(selectedProviders) > 0 {
		aggregated, err = insurance.AggregateRequiredFields(policyType, selectedProviders)
		if err != nil {
			return insurance.RequirementSet{}, err
		}
	}

	mergedFields := make(map[string]insurance.RequiredField)
	for _, field := range baseFields {
		mergedFields[field.Path] = field
	}
	for _, field := range aggregated.Fields {
		if existing, ok := mergedFields[field.Path]; ok {
			mergedFields[field.Path] = mergeRequiredFields(existing, field)
			continue
		}
		mergedFields[field.Path] = field
	}

	fields := make([]insurance.RequiredField, 0, len(mergedFields))
	for _, field := range mergedFields {
		normalized := normalizeRequiredField(field)
		fields = append(fields, normalized)
	}

	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Path == fields[j].Path {
			return fields[i].Type < fields[j].Type
		}
		return fields[i].Path < fields[j].Path
	})

	providers := aggregated.Providers
	if len(providers) == 0 && len(selectedProviders) > 0 {
		providers = collectProviderNames(selectedProviders)
	}

	return insurance.RequirementSet{
		PolicyType: policyType,
		Providers:  providers,
		Fields:     fields,
	}, nil
}

func mergeRequiredFields(base, addition insurance.RequiredField) insurance.RequiredField {
	if base.Type == insurance.FieldTypeUnknown && addition.Type != insurance.FieldTypeUnknown {
		base.Type = addition.Type
	}
	if base.Description == "" {
		base.Description = addition.Description
	}
	if base.Alias == "" {
		base.Alias = addition.Alias
	}
	if base.Example == nil {
		base.Example = addition.Example
	}
	base.Optional = base.Optional && addition.Optional
	base.Providers = unionProviders(base.Providers, addition.Providers)
	base.AllowedValues = unionStrings(base.AllowedValues, addition.AllowedValues)
	base.AllowedValuesAliases = mergeAllowedValuesAliases(base.AllowedValuesAliases, addition.AllowedValuesAliases)
	return base
}

func normalizeRequiredField(field insurance.RequiredField) insurance.RequiredField {
	field.Providers = unionProviders(nil, field.Providers)
	field.AllowedValues = unionStrings(nil, field.AllowedValues)
	if len(field.Providers) == 0 {
		field.Providers = nil
	}
	if len(field.AllowedValues) == 0 {
		field.AllowedValues = nil
	}
	if len(field.AllowedValuesAliases) == 0 {
		field.AllowedValuesAliases = nil
	}
	return field
}

func collectProviderNames(providers []insurance.QuoteProvider) []insurance.InsuranceProvider {
	if len(providers) == 0 {
		return nil
	}
	set := make(map[insurance.InsuranceProvider]struct{})
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		name := provider.Provider()
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}

	if len(set) == 0 {
		return nil
	}

	list := make([]insurance.InsuranceProvider, 0, len(set))
	for name := range set {
		list = append(list, name)
	}
	sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
	return list
}

func unionProviders(base []insurance.InsuranceProvider, additions []insurance.InsuranceProvider) []insurance.InsuranceProvider {
	set := make(map[insurance.InsuranceProvider]struct{}, len(base))
	for _, provider := range base {
		if provider == "" {
			continue
		}
		set[provider] = struct{}{}
	}

	for _, provider := range additions {
		if provider == "" {
			continue
		}
		set[provider] = struct{}{}
	}

	if len(set) == 0 {
		return nil
	}

	out := make([]insurance.InsuranceProvider, 0, len(set))
	for provider := range set {
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func unionStrings(base []string, additions []string) []string {
	set := make(map[string]struct{}, len(base))
	for _, value := range base {
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}

	for _, value := range additions {
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}

	if len(set) == 0 {
		return nil
	}

	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mergeAllowedValuesAliases(base, additions map[string]string) map[string]string {
	if len(base) == 0 && len(additions) == 0 {
		return nil
	}

	merged := make(map[string]string, len(base)+len(additions))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range additions {
		if _, exists := merged[k]; !exists {
			merged[k] = v
		}
	}
	return merged
}
