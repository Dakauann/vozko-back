package pottencial

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"vozko/domain/insurance"
)

type commissionedAgent struct {
	DocumentNumber          string  `json:"documentNumber"`
	Role                    string  `json:"role"`
	Lead                    *bool   `json:"lead,omitempty"`
	ParticipationPercentage float64 `json:"participationPercentage,omitempty"`
	CommissionPercentage    float64 `json:"commissionPercentage,omitempty"`
}

type participant struct {
	DocumentNumber          string              `json:"documentNumber"`
	Role                    string              `json:"role"`
	ParticipationPercentage float64             `json:"participationPercentage,omitempty"`
	Address                 *participantAddress `json:"address,omitempty"`
}

type participantAddress struct {
	Street     string `json:"street"`
	Number     string `json:"number"`
	District   string `json:"district"`
	City       string `json:"city"`
	State      string `json:"state"`
	ZipCode    string `json:"zipCode"`
	Complement string `json:"complement,omitempty"`
}

type coverage struct {
	Key           string  `json:"key"`
	InsuredAmount float64 `json:"insuredAmount"`
}

type quoteResponse struct {
	QuoteID           string               `json:"quoteId"`
	ProductKey        string               `json:"productKey"`
	PolicyType        string               `json:"policyType"`
	QuoteNumber       int64Value           `json:"quoteNumber"`
	Status            string               `json:"status"`
	CommercialPremium numberValue          `json:"commercialPremium"`
	GrossPremium      numberValue          `json:"grossPremium"`
	IOF               numberValue          `json:"iof"`
	PolicyPeriodStart string               `json:"policyPeriodStart"`
	PolicyPeriodEnd   string               `json:"policyPeriodEnd"`
	DocumentPeriodEnd string               `json:"documentPeriodEnd"`
	CreatedAt         string               `json:"createdAt"`
	RiskObjects       []riskObjectResponse `json:"riskObjects"`
}

type riskObjectResponse struct {
	Coverages []coverageResponse `json:"coverages"`
}

type coverageResponse struct {
	Key           string      `json:"key"`
	InsuredAmount numberValue `json:"insuredAmount"`
}

type numberValue struct {
	value float64
	valid bool
}

func (n *numberValue) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		n.valid = false
		n.value = 0
		return nil
	}

	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			n.valid = false
			n.value = 0
			return nil
		}
		parsed, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(raw, " ", ""), ",", "."), 64)
		if err != nil {
			return err
		}
		n.value = parsed
		n.valid = true
		return nil
	}

	var parsed float64
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	n.value = parsed
	n.valid = true
	return nil
}

func (n numberValue) Float64() float64 { return n.value }

func (n numberValue) Valid() bool { return n.valid }

type int64Value struct {
	value int64
	valid bool
}

func (i *int64Value) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		i.valid = false
		i.value = 0
		return nil
	}

	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			i.valid = false
			i.value = 0
			return nil
		}
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		i.value = parsed
		i.valid = true
		return nil
	}

	var num json.Number
	if err := json.Unmarshal(data, &num); err != nil {
		return err
	}
	parsed, err := num.Int64()
	if err != nil {
		return err
	}
	i.value = parsed
	i.valid = true
	return nil
}

func (i int64Value) Int64() int64 { return i.value }

func (i int64Value) Valid() bool { return i.valid }

func cloneCommissionedAgents(items []commissionedAgent) []commissionedAgent {
	if len(items) == 0 {
		return nil
	}
	out := make([]commissionedAgent, len(items))
	for i, agent := range items {
		out[i] = agent
		if agent.Lead != nil {
			value := *agent.Lead
			out[i].Lead = &value
		}
	}
	return out
}

func cloneParticipants(items []participant) []participant {
	if len(items) == 0 {
		return nil
	}
	out := make([]participant, len(items))
	for i, p := range items {
		out[i] = p
		if p.Address != nil {
			addrCopy := *p.Address
			out[i].Address = &addrCopy
		}
	}
	return out
}

func cloneCoverages(items []coverage) []coverage {
	if len(items) == 0 {
		return nil
	}
	out := make([]coverage, len(items))
	copy(out, items)
	return out
}

func decodeDetails(details map[string]interface{}, target interface{}) error {
	if details == nil {
		return fmt.Errorf("details map is nil")
	}

	data, err := json.Marshal(details)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, target); err != nil {
		return err
	}

	return nil
}

func mapQuoteResponse(req insurance.ProviderQuoteRequest, resp quoteResponse) (*insurance.InsuranceQuote, error) {
	if strings.TrimSpace(resp.QuoteID) == "" {
		return nil, fmt.Errorf("pottencial: quote response missing quoteId")
	}

	coverageTotal := sumCoverage(resp.RiskObjects)
	createdAt, _ := parseTime(resp.CreatedAt)
	validUntil, _ := parseTime(resp.PolicyPeriodEnd)
	if validUntil.IsZero() {
		validUntil, _ = parseTime(resp.DocumentPeriodEnd)
	}

	metadata := map[string]interface{}{
		"status":     resp.Status,
		"productKey": resp.ProductKey,
	}
	if resp.QuoteNumber.Valid() {
		metadata["quoteNumber"] = resp.QuoteNumber.Int64()
	}
	if resp.CommercialPremium.Valid() {
		metadata["commercialPremium"] = resp.CommercialPremium.Float64()
	}
	if resp.GrossPremium.Valid() {
		metadata["grossPremium"] = resp.GrossPremium.Float64()
	}
	if resp.IOF.Valid() {
		metadata["iof"] = resp.IOF.Float64()
	}

	quote := &insurance.InsuranceQuote{
		ExternalID:  resp.QuoteID,
		UserID:      req.UserID,
		PolicyType:  req.PolicyType,
		Provider:    insurance.InsuranceProviderPottencial,
		CoverageAmt: coverageTotal,
		Premium:     resp.GrossPremium.Float64(),
		Currency:    "BRL",
		CreatedAt:   createdAt,
		Metadata:    metadata,
	}

	if !validUntil.IsZero() {
		quote.ValidUntil = &validUntil
	}

	return quote, nil
}

func sumCoverage(riskObjects []riskObjectResponse) float64 {
	var total float64
	for _, risk := range riskObjects {
		for _, coverage := range risk.Coverages {
			if coverage.InsuredAmount.Valid() {
				total += coverage.InsuredAmount.Float64()
			}
		}
	}
	return total
}

func parseTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time value")
	}

	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", value)
}

func newViolation(path, reason string) insurance.FieldViolation {
	return insurance.FieldViolation{
		Field:  insurance.RequiredField{Path: path},
		Reason: reason,
	}
}
