package pottencial

import (
	"fmt"
	"strings"

	"vozko/domain/insurance"
)

type quoteRequest struct {
	ProductKey         string               `json:"productKey"`
	PolicyType         string               `json:"policyType"`
	PolicyPeriodStart  string               `json:"policyPeriodStart"`
	PolicyPeriodEnd    string               `json:"policyPeriodEnd"`
	CommissionedAgents []commissionedAgent  `json:"commissionedAgents"`
	Participants       []participant        `json:"participants"`
	RiskObjects        []judicialRiskObject `json:"riskObjects"`
}

type judicialQuoteDetails struct {
	PolicyType         string               `json:"policyType"`
	PolicyPeriodStart  string               `json:"policyPeriodStart"`
	PolicyPeriodEnd    string               `json:"policyPeriodEnd"`
	CommissionedAgents []commissionedAgent  `json:"commissionedAgents"`
	Participants       []participant        `json:"participants"`
	RiskObjects        []judicialRiskObject `json:"riskObjects"`
}

type judicialRiskObject struct {
	Coverages                []coverage `json:"coverages"`
	IncludeCourtInRiskObject *bool      `json:"includeCourtInRiskObject,omitempty"`
	DocumentValidityPeriod   int        `json:"documentValidityPeriod"`
	ManagementModality       string     `json:"managementModality"`
	CourtID                  string     `json:"courtId,omitempty"`
	TaxCharged               string     `json:"taxCharged"`
	ProcessNumber            string     `json:"processNumber,omitempty"`
	InfractionNumber         string     `json:"infractionNumber,omitempty"`
	AdministrativeProcess    string     `json:"administrativeProcess,omitempty"`
	ActiveDebtCertificate    string     `json:"activeDebtCertificate,omitempty"`
}

func buildJudicialQuotePayload(req insurance.ProviderQuoteRequest) (interface{}, error) {
	var details judicialQuoteDetails
	if err := decodeDetails(req.Details, &details); err != nil {
		return nil, fmt.Errorf("pottencial: invalid quote details: %w", err)
	}

	normalizeJudicialDetails(&details)

	if violations := validateJudicialDetails(details); len(violations) > 0 {
		return nil, &insurance.MissingRequiredFieldsError{PolicyType: req.PolicyType, Violations: violations}
	}

	if strings.TrimSpace(details.PolicyType) == "" {
		details.PolicyType = defaultPolicyType
	}

	for i := range details.RiskObjects {
		if details.RiskObjects[i].IncludeCourtInRiskObject == nil {
			include := true
			details.RiskObjects[i].IncludeCourtInRiskObject = &include
		}
	}

	return quoteRequest{
		ProductKey:         judicialExecutionFiscalProductKey,
		PolicyType:         details.PolicyType,
		PolicyPeriodStart:  details.PolicyPeriodStart,
		PolicyPeriodEnd:    details.PolicyPeriodEnd,
		CommissionedAgents: cloneCommissionedAgents(details.CommissionedAgents),
		Participants:       cloneParticipants(details.Participants),
		RiskObjects:        cloneJudicialRiskObjects(details.RiskObjects),
	}, nil
}

func normalizeJudicialDetails(details *judicialQuoteDetails) {
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
	}

	for i := range details.RiskObjects {
		details.RiskObjects[i].ManagementModality = strings.TrimSpace(details.RiskObjects[i].ManagementModality)
		details.RiskObjects[i].CourtID = strings.TrimSpace(details.RiskObjects[i].CourtID)
		details.RiskObjects[i].TaxCharged = strings.TrimSpace(details.RiskObjects[i].TaxCharged)
		details.RiskObjects[i].ProcessNumber = strings.TrimSpace(details.RiskObjects[i].ProcessNumber)
		details.RiskObjects[i].InfractionNumber = strings.TrimSpace(details.RiskObjects[i].InfractionNumber)
		details.RiskObjects[i].AdministrativeProcess = strings.TrimSpace(details.RiskObjects[i].AdministrativeProcess)
		details.RiskObjects[i].ActiveDebtCertificate = strings.TrimSpace(details.RiskObjects[i].ActiveDebtCertificate)
		for j := range details.RiskObjects[i].Coverages {
			details.RiskObjects[i].Coverages[j].Key = strings.TrimSpace(details.RiskObjects[i].Coverages[j].Key)
		}
	}
}

func validateJudicialDetails(details judicialQuoteDetails) []insurance.FieldViolation {
	var violations []insurance.FieldViolation

	if details.PolicyPeriodStart == "" {
		violations = append(violations, newViolation("policyPeriodStart", "policyPeriodStart is required"))
	}
	if details.PolicyPeriodEnd == "" {
		violations = append(violations, newViolation("policyPeriodEnd", "policyPeriodEnd is required"))
	}
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
		violations = append(violations, newViolation("riskObjects[]", "at least one risk object is required"))
		return violations
	}

	for _, risk := range details.RiskObjects {
		if len(risk.Coverages) == 0 {
			violations = append(violations, newViolation("riskObjects[].coverages[]", "at least one coverage is required"))
		}
		for _, coverage := range risk.Coverages {
			if coverage.Key == "" {
				violations = append(violations, newViolation("riskObjects[].coverages[].key", "coverage key is required"))
			}
			if coverage.InsuredAmount <= 0 {
				violations = append(violations, newViolation("riskObjects[].coverages[].insuredAmount", "insuredAmount must be greater than zero"))
			}
		}

		if risk.ManagementModality == "" {
			violations = append(violations, newViolation("riskObjects[].managementModality", "managementModality is required"))
		}
		if risk.TaxCharged == "" {
			violations = append(violations, newViolation("riskObjects[].taxCharged", "taxCharged is required"))
		}
		if risk.DocumentValidityPeriod <= 0 {
			violations = append(violations, newViolation("riskObjects[].documentValidityPeriod", "documentValidityPeriod must be greater than zero"))
		}

		if risk.ProcessNumber == "" && risk.InfractionNumber == "" && risk.AdministrativeProcess == "" && risk.ActiveDebtCertificate == "" && risk.CourtID == "" {
			violations = append(violations, insurance.FieldViolation{
				Field:  insurance.RequiredField{Path: "riskObjects[]", Description: "one of processNumber, infractionNumber, administrativeProcess, activeDebtCertificate or courtId must be provided"},
				Reason: "at least one identifying field must be provided",
			})
		}
	}

	return violations
}

func cloneJudicialRiskObjects(items []judicialRiskObject) []judicialRiskObject {
	if len(items) == 0 {
		return nil
	}
	out := make([]judicialRiskObject, len(items))
	for i, risk := range items {
		out[i] = risk
		if risk.IncludeCourtInRiskObject != nil {
			value := *risk.IncludeCourtInRiskObject
			out[i].IncludeCourtInRiskObject = &value
		}
		out[i].Coverages = cloneCoverages(risk.Coverages)
	}
	return out
}
