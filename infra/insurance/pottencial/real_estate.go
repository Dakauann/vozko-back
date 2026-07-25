package pottencial

import (
	"fmt"
	"strings"

	"vozko/domain/insurance"
)

type realEstateQuoteRequest struct {
	ProductKey         string               `json:"productKey"`
	PolicyType         string               `json:"policyType"`
	PolicyPeriodStart  string               `json:"policyPeriodStart"`
	PolicyPeriodEnd    string               `json:"policyPeriodEnd"`
	CommissionedAgents []commissionedAgent  `json:"commissionedAgents"`
	Participants       []participant        `json:"participants"`
	RiskObjects        []propertyRiskObject `json:"riskObjects"`
}

type realEstateQuoteDetails struct {
	PolicyType         string               `json:"policyType"`
	PolicyPeriodStart  string               `json:"policyPeriodStart"`
	PolicyPeriodEnd    string               `json:"policyPeriodEnd"`
	CommissionedAgents []commissionedAgent  `json:"commissionedAgents"`
	Participants       []participant        `json:"participants"`
	RiskObjects        []propertyRiskObject `json:"riskObjects"`
}

type propertyRiskObject struct {
	Type                        string          `json:"type"`
	Coverages                   []coverage      `json:"coverages"`
	HistoricalProtectedProperty *bool           `json:"historicalProtectedProperty"`
	SharedProperty              *bool           `json:"sharedProperty"`
	InsuredOwner                *bool           `json:"insuredOwner"`
	PropertyType                string          `json:"propertyType"`
	ConstructionType            string          `json:"constructionType"`
	PropertyUseType             string          `json:"propertyUseType"`
	Address                     propertyAddress `json:"address"`
}

type propertyAddress struct {
	Street     string `json:"street"`
	Number     string `json:"number"`
	District   string `json:"district"`
	City       string `json:"city"`
	State      string `json:"state"`
	ZipCode    string `json:"zipCode"`
	Complement string `json:"complement,omitempty"`
}

func buildRealEstateQuotePayload(req insurance.ProviderQuoteRequest) (interface{}, error) {
	var details realEstateQuoteDetails
	if err := decodeDetails(req.Details, &details); err != nil {
		return nil, fmt.Errorf("pottencial: invalid quote details: %w", err)
	}

	normalizeRealEstateDetails(&details)

	if violations := validateRealEstateDetails(details); len(violations) > 0 {
		return nil, &insurance.MissingRequiredFieldsError{PolicyType: req.PolicyType, Violations: violations}
	}

	if strings.TrimSpace(details.PolicyType) == "" {
		details.PolicyType = defaultPolicyType
	}

	return realEstateQuoteRequest{
		ProductKey:         imobiliarioProductKey,
		PolicyType:         details.PolicyType,
		PolicyPeriodStart:  details.PolicyPeriodStart,
		PolicyPeriodEnd:    details.PolicyPeriodEnd,
		CommissionedAgents: cloneCommissionedAgents(details.CommissionedAgents),
		Participants:       cloneParticipants(details.Participants),
		RiskObjects:        clonePropertyRiskObjects(details.RiskObjects),
	}, nil
}

func normalizeRealEstateDetails(details *realEstateQuoteDetails) {
	if details == nil {
		return
	}

	details.PolicyType = strings.TrimSpace(details.PolicyType)
	details.PolicyPeriodStart = strings.TrimSpace(details.PolicyPeriodStart)
	details.PolicyPeriodEnd = strings.TrimSpace(details.PolicyPeriodEnd)

	for i := range details.CommissionedAgents {
		details.CommissionedAgents[i].DocumentNumber = strings.TrimSpace(details.CommissionedAgents[i].DocumentNumber)
		details.CommissionedAgents[i].Role = strings.TrimSpace(details.CommissionedAgents[i].Role)
	}

	for i := range details.Participants {
		details.Participants[i].DocumentNumber = strings.TrimSpace(details.Participants[i].DocumentNumber)
		details.Participants[i].Role = strings.TrimSpace(details.Participants[i].Role)
		if details.Participants[i].Address != nil {
			addr := details.Participants[i].Address
			addr.Street = strings.TrimSpace(addr.Street)
			addr.Number = strings.TrimSpace(addr.Number)
			addr.District = strings.TrimSpace(addr.District)
			addr.City = strings.TrimSpace(addr.City)
			addr.State = strings.TrimSpace(addr.State)
			addr.ZipCode = strings.TrimSpace(addr.ZipCode)
			addr.Complement = strings.TrimSpace(addr.Complement)
		}
	}

	for i := range details.RiskObjects {
		details.RiskObjects[i].Type = strings.TrimSpace(details.RiskObjects[i].Type)
		if details.RiskObjects[i].Type == "" {
			details.RiskObjects[i].Type = defaultPropertyRiskType
		}
		details.RiskObjects[i].PropertyType = strings.TrimSpace(details.RiskObjects[i].PropertyType)
		details.RiskObjects[i].ConstructionType = strings.TrimSpace(details.RiskObjects[i].ConstructionType)
		details.RiskObjects[i].PropertyUseType = strings.TrimSpace(details.RiskObjects[i].PropertyUseType)
		details.RiskObjects[i].Address.Street = strings.TrimSpace(details.RiskObjects[i].Address.Street)
		details.RiskObjects[i].Address.Number = strings.TrimSpace(details.RiskObjects[i].Address.Number)
		details.RiskObjects[i].Address.District = strings.TrimSpace(details.RiskObjects[i].Address.District)
		details.RiskObjects[i].Address.City = strings.TrimSpace(details.RiskObjects[i].Address.City)
		details.RiskObjects[i].Address.State = strings.TrimSpace(details.RiskObjects[i].Address.State)
		details.RiskObjects[i].Address.ZipCode = strings.TrimSpace(details.RiskObjects[i].Address.ZipCode)
		details.RiskObjects[i].Address.Complement = strings.TrimSpace(details.RiskObjects[i].Address.Complement)
		for j := range details.RiskObjects[i].Coverages {
			details.RiskObjects[i].Coverages[j].Key = strings.TrimSpace(details.RiskObjects[i].Coverages[j].Key)
		}
	}
}

func validateRealEstateDetails(details realEstateQuoteDetails) []insurance.FieldViolation {
	var violations []insurance.FieldViolation

	if len(details.CommissionedAgents) == 0 {
		violations = append(violations, newViolation("commissionedAgents[]", "at least one commissioned agent is required"))
	}
	for _, agent := range details.CommissionedAgents {
		if agent.DocumentNumber == "" {
			violations = append(violations, newViolation("commissionedAgents[].documentNumber", "documentNumber is required"))
		}
		if agent.Role == "" {
			violations = append(violations, newViolation("commissionedAgents[].role", "role is required"))
		}
	}

	if len(details.Participants) == 0 {
		violations = append(violations, newViolation("participants[]", "at least one participant is required"))
	}
	for _, participant := range details.Participants {
		if participant.DocumentNumber == "" {
			violations = append(violations, newViolation("participants[].documentNumber", "documentNumber is required"))
		}
		if participant.Role == "" {
			violations = append(violations, newViolation("participants[].role", "role is required"))
		}
	}

	if len(details.RiskObjects) == 0 {
		violations = append(violations, newViolation("riskObjects[]", "at least one property risk object is required"))
		return violations
	}
	if len(details.RiskObjects) > 1 {
		violations = append(violations, insurance.FieldViolation{
			Field:  insurance.RequiredField{Path: "riskObjects[]", Description: "only one property risk object is supported"},
			Reason: "multiple risk objects provided",
		})
	}

	for _, risk := range details.RiskObjects {
		if risk.Type == "" {
			violations = append(violations, newViolation("riskObjects[].type", "risk object type is required"))
		} else if !strings.EqualFold(risk.Type, defaultPropertyRiskType) {
			violations = append(violations, insurance.FieldViolation{
				Field:  insurance.RequiredField{Path: "riskObjects[].type"},
				Reason: "risk object type must be Property",
			})
		}
		for _, coverage := range risk.Coverages {
			if coverage.Key == "" {
				violations = append(violations, newViolation("riskObjects[].coverages[].key", "coverage key is required when provided"))
			}
			if coverage.InsuredAmount <= 0 {
				violations = append(violations, newViolation("riskObjects[].coverages[].insuredAmount", "insuredAmount must be greater than zero when provided"))
			}
		}

		if risk.HistoricalProtectedProperty == nil {
			violations = append(violations, newViolation("riskObjects[].historicalProtectedProperty", "historicalProtectedProperty is required"))
		}
		if risk.SharedProperty == nil {
			violations = append(violations, newViolation("riskObjects[].sharedProperty", "sharedProperty is required"))
		}
		if risk.InsuredOwner == nil {
			violations = append(violations, newViolation("riskObjects[].insuredOwner", "insuredOwner is required"))
		}
		if risk.PropertyType == "" {
			violations = append(violations, newViolation("riskObjects[].propertyType", "propertyType is required"))
		}
		if risk.ConstructionType == "" {
			violations = append(violations, newViolation("riskObjects[].constructionType", "constructionType is required"))
		}
		if risk.PropertyUseType == "" {
			violations = append(violations, newViolation("riskObjects[].propertyUseType", "propertyUseType is required"))
		}

		if risk.Address.Street == "" {
			violations = append(violations, newViolation("riskObjects[].address.street", "street is required"))
		}
		if risk.Address.Number == "" {
			violations = append(violations, newViolation("riskObjects[].address.number", "number is required"))
		}
		if risk.Address.District == "" {
			violations = append(violations, newViolation("riskObjects[].address.district", "district is required"))
		}
		if risk.Address.City == "" {
			violations = append(violations, newViolation("riskObjects[].address.city", "city is required"))
		}
		if risk.Address.State == "" {
			violations = append(violations, newViolation("riskObjects[].address.state", "state is required"))
		}
		if risk.Address.ZipCode == "" {
			violations = append(violations, newViolation("riskObjects[].address.zipCode", "zipCode is required"))
		}
	}

	return violations
}

func clonePropertyRiskObjects(items []propertyRiskObject) []propertyRiskObject {
	if len(items) == 0 {
		return nil
	}
	out := make([]propertyRiskObject, len(items))
	for i, risk := range items {
		out[i] = risk
		if risk.HistoricalProtectedProperty != nil {
			value := *risk.HistoricalProtectedProperty
			out[i].HistoricalProtectedProperty = &value
		}
		if risk.SharedProperty != nil {
			value := *risk.SharedProperty
			out[i].SharedProperty = &value
		}
		if risk.InsuredOwner != nil {
			value := *risk.InsuredOwner
			out[i].InsuredOwner = &value
		}
		out[i].Coverages = cloneCoverages(risk.Coverages)
	}
	return out
}
