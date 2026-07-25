package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/insurance"
)

type InsuranceHandler struct {
	quoteUseCase        insurance.QuoteInsuranceUseCase
	listUseCase         insurance.ListUserQuotationsUseCase
	getQuotationUseCase insurance.GetQuotationUseCase
	policiesUseCase     insurance.ListPoliciesUseCase
	describeUseCase     insurance.DescribeQuoteRequirementsUseCase
}

func NewInsuranceHandler(quoteUseCase insurance.QuoteInsuranceUseCase, listUseCase insurance.ListUserQuotationsUseCase, getQuotationUseCase insurance.GetQuotationUseCase, policiesUseCase insurance.ListPoliciesUseCase, describeUseCase insurance.DescribeQuoteRequirementsUseCase) *InsuranceHandler {
	return &InsuranceHandler{
		quoteUseCase:        quoteUseCase,
		listUseCase:         listUseCase,
		getQuotationUseCase: getQuotationUseCase,
		policiesUseCase:     policiesUseCase,
		describeUseCase:     describeUseCase,
	}
}

func (h *InsuranceHandler) Quote(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	if h.quoteUseCase == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Quotation service unavailable", nil)
		return
	}

	var payload struct {
		PolicyType string                 `json:"policyType"`
		Details    map[string]interface{} `json:"details"`
		Providers  []string               `json:"providers"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"policyType": "string (required)",
			"details":    "object",
			"providers":  "array<string>",
		})
		return
	}

	policyType := insurance.PolicyType(strings.TrimSpace(payload.PolicyType))
	if policyType == "" {
		response.WriteValidationError(w, map[string]string{"policyType": "required"})
		return
	}

	providers := normalizeProviders(payload.Providers)
	details := payload.Details
	if details == nil {
		details = map[string]interface{}{}
	}

	request := insurance.InsuranceQuoteRequest{
		UserID:     userID,
		PolicyType: policyType,
		Details:    details,
		Providers:  providers,
	}

	result, err := h.quoteUseCase.Execute(r.Context(), request)
	if err != nil {
		handleInsuranceError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

func (h *InsuranceHandler) ListQuotations(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	if h.listUseCase == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Quotation listing unavailable", nil)
		return
	}

	quotations, err := h.listUseCase.Execute(r.Context(), userID)
	if err != nil {
		handleInsuranceError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, struct {
		Quotations []insurance.Quotation `json:"quotations"`
	}{Quotations: quotations})
}

func (h *InsuranceHandler) GetQuotation(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	if h.getQuotationUseCase == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Quotation service unavailable", nil)
		return
	}

	params := mux.Vars(r)
	quotationID := strings.TrimSpace(params["quotationId"])
	if quotationID == "" {
		response.WriteValidationError(w, map[string]string{"quotationId": "required"})
		return
	}

	quotation, err := h.getQuotationUseCase.Execute(r.Context(), userID, quotationID)
	if err != nil {
		handleInsuranceError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, quotation)
}

func (h *InsuranceHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	if h.policiesUseCase == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Policy listing unavailable", nil)
		return
	}

	policies, err := h.policiesUseCase.Execute(r.Context())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list insurance policies", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, struct {
		Policies []insurance.PolicySummary `json:"policies"`
	}{Policies: policies})
}

func (h *InsuranceHandler) DescribePolicy(w http.ResponseWriter, r *http.Request) {
	if h.describeUseCase == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Quotation requirements unavailable", nil)
		return
	}

	params := mux.Vars(r)
	rawPolicy := strings.TrimSpace(params["policyType"])
	if rawPolicy == "" {
		response.WriteValidationError(w, map[string]string{"policyType": "required"})
		return
	}

	policyType := insurance.PolicyType(strings.ToUpper(rawPolicy))

	providerFilters := parseProviderFilters(r.URL.Query().Get("providers"))

	requirementSet, err := h.describeUseCase.Execute(r.Context(), policyType, providerFilters)
	if err != nil {
		switch {
		case errors.Is(err, insurance.ErrUnsupportedPolicyType):
			response.WriteError(w, http.StatusNotFound, "Policy type not found", nil)
			return
		case errors.Is(err, insurance.ErrNoProvidersForPolicy):
			response.WriteError(w, http.StatusBadRequest, "No providers available for selected policy", nil)
			return
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to describe policy requirements", nil)
			return
		}
	}

	summary, ok := insurance.PolicySummaryByType(policyType)
	if !ok {
		response.WriteError(w, http.StatusNotFound, "Policy type not found", nil)
		return
	}

	providers := summary.Providers
	if len(requirementSet.Providers) > 0 {
		providers = requirementSet.Providers
	}
	summary.Providers = append([]insurance.InsuranceProvider(nil), providers...)

	schema := buildNestedSchema(requirementSet.Fields)

	response.WriteSuccess(w, http.StatusOK, struct {
		Policy    insurance.PolicySummary       `json:"policy"`
		Providers []insurance.InsuranceProvider `json:"providers"`
		Schema    map[string]interface{}        `json:"schema"`
	}{
		Policy:    summary,
		Providers: providers,
		Schema:    schema,
	})
}

func buildNestedSchema(fields []insurance.RequiredField) map[string]interface{} {
	schema := make(map[string]interface{})

	for _, field := range fields {
		path := field.Path
		if path == "" {
			continue
		}

		segments := parsePathSegments(path)
		if len(segments) == 0 {
			continue
		}

		insertFieldInSchema(schema, segments, field)
	}

	return schema
}

func parsePathSegments(path string) []string {
	var segments []string
	var current strings.Builder

	for i := 0; i < len(path); i++ {
		ch := path[i]
		if ch == '.' {
			if current.Len() > 0 {
				segments = append(segments, current.String())
				current.Reset()
			}
		} else if ch == '[' && i+1 < len(path) && path[i+1] == ']' {
			current.WriteString("[]")
			i++
		} else {
			current.WriteByte(ch)
		}
	}

	if current.Len() > 0 {
		segments = append(segments, current.String())
	}

	return segments
}

func insertFieldInSchema(schema map[string]interface{}, segments []string, field insurance.RequiredField) {
	if len(segments) == 0 {
		return
	}

	current := schema
	for i, segment := range segments {
		isLast := i == len(segments)-1
		isArray := strings.HasSuffix(segment, "[]")
		cleanName := strings.TrimSuffix(segment, "[]")

		if isLast {
			fieldDef := map[string]interface{}{
				"_type":     string(field.Type),
				"_required": !field.Optional,
			}
			if field.Description != "" {
				fieldDef["_description"] = field.Description
			}
			if field.Alias != "" {
				fieldDef["_alias"] = field.Alias
			}
			if len(field.AllowedValues) > 0 {
				fieldDef["_allowedValues"] = field.AllowedValues
			}
			if len(field.AllowedValuesAliases) > 0 {
				fieldDef["_allowedValuesAliases"] = field.AllowedValuesAliases
			}

			if isArray {
				fieldDef["_type"] = "array"
				if existing, ok := current[cleanName]; ok {
					if existingMap, ok := existing.(map[string]interface{}); ok {
						for k, v := range fieldDef {
							existingMap[k] = v
						}
					}
				} else {
					current[cleanName] = fieldDef
				}
			} else {
				if existing, ok := current[cleanName]; ok {
					if existingMap, ok := existing.(map[string]interface{}); ok {
						for k, v := range fieldDef {
							if k == "_type" && existingMap["_type"] == "object" {
								continue
							}
							existingMap[k] = v
						}
					}
				} else {
					current[cleanName] = fieldDef
				}
			}
		} else {
			if isArray {
				if existing, ok := current[cleanName]; ok {
					if existingMap, ok := existing.(map[string]interface{}); ok {
						if items, ok := existingMap["_items"]; ok {
							if itemsMap, ok := items.(map[string]interface{}); ok {
								current = itemsMap
							}
						} else {
							itemsMap := make(map[string]interface{})
							existingMap["_items"] = itemsMap
							existingMap["_type"] = "array"
							current = itemsMap
						}
					}
				} else {
					itemsMap := make(map[string]interface{})
					current[cleanName] = map[string]interface{}{
						"_type":  "array",
						"_items": itemsMap,
					}
					current = itemsMap
				}
			} else {
				if existing, ok := current[cleanName]; ok {
					if existingMap, ok := existing.(map[string]interface{}); ok {
						current = existingMap
					}
				} else {
					newMap := map[string]interface{}{
						"_type": "object",
					}
					current[cleanName] = newMap
					current = newMap
				}
			}
		}
	}
}

func parseProviderFilters(raw string) []insurance.InsuranceProvider {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, part)
	}
	if len(values) == 0 {
		return nil
	}
	return normalizeProviders(values)
}

func normalizeProviders(raw []string) []insurance.InsuranceProvider {
	if len(raw) == 0 {
		return nil
	}

	providers := make([]insurance.InsuranceProvider, 0, len(raw))
	for _, value := range raw {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		providers = append(providers, insurance.InsuranceProvider(strings.ToUpper(name)))
	}

	if len(providers) == 0 {
		return nil
	}

	return providers
}

func handleInsuranceError(w http.ResponseWriter, err error) {
	var missingErr *insurance.MissingRequiredFieldsError
	if errors.As(err, &missingErr) {
		missing := make(map[string]string)
		for _, violation := range missingErr.MissingFields() {
			missing[violation.Field.Path] = violation.Reason
		}
		if len(missing) == 0 {
			missing = map[string]string{"details": "missing required fields"}
		}
		optional := optionalFieldsForPolicy(missingErr.PolicyType)
		response.WriteValidationError(w, missing, optional...)
		return
	}

	switch {
	case errors.Is(err, insurance.ErrInvalidQuoteRequest):
		response.WriteValidationError(w, map[string]string{"request": "invalid"})
	case errors.Is(err, insurance.ErrNoProvidersForPolicy):
		response.WriteError(w, http.StatusBadRequest, "No providers available for selected policy", nil)
	case errors.Is(err, insurance.ErrRepositoryNotConfigured):
		response.WriteError(w, http.StatusServiceUnavailable, "Insurance repository not configured", nil)
	case errors.Is(err, insurance.ErrQuotationNotFound):
		response.WriteError(w, http.StatusNotFound, "Quotation not found", nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Insurance operation failed: "+err.Error(), nil)
	}
}

func optionalFieldsForPolicy(policy insurance.PolicyType) []string {
	fields, err := insurance.RequiredFieldsForPolicy(policy)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	optional := make([]string, 0)

	for _, field := range fields {
		if !field.Optional {
			continue
		}

		normalized := normalizeFieldPath(field.Path)
		if _, exists := seen[normalized]; exists {
			continue
		}

		seen[normalized] = struct{}{}
		optional = append(optional, normalized+" (not required)")
	}

	if len(optional) == 0 {
		return nil
	}

	sort.Strings(optional)
	return optional
}

func normalizeFieldPath(path string) string {
	if path == "" {
		return ""
	}

	parts := strings.Split(path, ".")
	for i, part := range parts {
		parts[i] = strings.TrimSuffix(part, "[]")
	}

	return strings.Join(parts, ".")
}
