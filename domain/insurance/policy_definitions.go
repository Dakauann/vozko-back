package insurance

import "sort"

type PolicySummary struct {
	Type        PolicyType          `json:"type"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Providers   []InsuranceProvider `json:"providers"`
}

var policySummaries = map[PolicyType]PolicySummary{
	PolicyTypeJudicialExecutionFiscal: {
		Type:        PolicyTypeJudicialExecutionFiscal,
		Name:        "Garantia Judicial Execução Fiscal",
		Description: "Seguro garantia para execuções fiscais, cobrindo obrigações judiciais tributárias.",
		Providers:   []InsuranceProvider{InsuranceProviderPottencial},
	},
	PolicyTypeImobiliario: {
		Type:        PolicyTypeImobiliario,
		Name:        "Imobiliário",
		Description: "Seguro incêndio para locações residenciais destinado a imobiliárias.",
		Providers:   []InsuranceProvider{InsuranceProviderPottencial},
	},
}

func AvailablePolicySummaries() []PolicySummary {
	out := make([]PolicySummary, 0, len(policySummaries))
	for _, summary := range policySummaries {
		copySummary := summary
		copySummary.Providers = append([]InsuranceProvider(nil), summary.Providers...)
		out = append(out, copySummary)
	}

	sortPolicySummaries(out)
	return out
}

func PolicySummaryByType(policy PolicyType) (PolicySummary, bool) {
	summary, ok := policySummaries[policy]
	if !ok {
		return PolicySummary{}, false
	}
	copySummary := summary
	copySummary.Providers = append([]InsuranceProvider(nil), summary.Providers...)
	return copySummary, true
}

func sortPolicySummaries(items []PolicySummary) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type == items[j].Type {
			return items[i].Name < items[j].Name
		}
		return items[i].Type < items[j].Type
	})
}
