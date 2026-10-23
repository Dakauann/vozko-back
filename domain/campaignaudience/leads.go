package campaignaudience

import (
	"errors"
	"slices"
	"strings"
)

type LeadStatus string

const (
	LeadReady     LeadStatus = "ready"
	LeadOut       LeadStatus = "out"
	LeadUndecided LeadStatus = "undecided"
)

var ErrLeadStatusInvalid = errors.New("campaign audience: the lead status must be ready, out or undecided")

var statusOf = map[Outcome]LeadStatus{OutcomeSend: LeadReady, OutcomeOut: LeadOut, OutcomeUndecided: LeadUndecided}

type AudienceLead struct {
	LeadID          string     `json:"leadId"`
	Line            int        `json:"line"`
	Name            string     `json:"name"`
	Contact         string     `json:"contact"`
	Status          LeadStatus `json:"status" enums:"ready,out,undecided"`
	Reason          string     `json:"reason" example:"no_email"`
	Missing         []string   `json:"missing"`
	MetadataMissing []string   `json:"metadataMissing"`
	Variables       []string   `json:"variables"`
}

func LeadOf(c Candidate, d Decision) AudienceLead {
	lead := AudienceLead{
		LeadID: c.LeadID, Line: c.Line, Name: c.Name, Contact: c.Contact, Status: statusOf[d.Outcome],
		Missing: orEmpty(d.Empty), MetadataMissing: orEmpty(d.MetadataMissing), Variables: orEmpty(d.Values),
	}
	if d.Outcome == OutcomeOut {
		lead.Reason = d.Reason
	}
	if d.Values == nil {
		lead.Variables = make([]string, 0, len(c.Values))
		for _, v := range c.Values {
			lead.Variables = append(lead.Variables, strings.TrimSpace(v))
		}
	}
	return lead
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

type LeadFilter struct {
	Status      LeadStatus
	Reason      string
	Variable    string
	MetadataKey string
	Search      string
}

func (f LeadFilter) Validate() error {
	switch f.Status {
	case "", LeadReady, LeadOut, LeadUndecided:
		return nil
	}
	return ErrLeadStatusInvalid
}

func (f LeadFilter) Matches(l AudienceLead) bool {
	switch {
	case f.Status != "" && l.Status != f.Status:
		return false
	case f.Reason != "" && l.Reason != f.Reason:
		return false
	case f.Variable != "" && !slices.Contains(l.Missing, f.Variable):
		return false
	case f.MetadataKey != "" && !slices.Contains(l.MetadataMissing, f.MetadataKey):
		return false
	}
	term := strings.ToLower(strings.TrimSpace(f.Search))
	return term == "" || strings.Contains(strings.ToLower(l.Name), term) || strings.Contains(strings.ToLower(l.Contact), term)
}
