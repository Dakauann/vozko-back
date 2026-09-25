package handlers

import (
	"encoding/json"
	"testing"

	"vozko/domain/aichat"
	copilot_domain "vozko/domain/copilot"
)

func TestMessageDTOCarriesTheReadableProposalAndItsStatus(t *testing.T) {
	raw, _ := json.Marshal(copilot_domain.PendingAction{ID: "act-1", ToolName: "create_template",
		Args: map[string]interface{}{"business_phone_id": "p1"}, Fields: []copilot_domain.Field{{Key: "name", Value: "refiliacao"}}})
	dto := toMessageDTO(&aichat.Message{ID: "m1", Role: aichat.RoleAssistant, ProposalID: "act-1", Proposal: raw, ProposalStatus: aichat.ProposalPending})
	if dto.Proposal == nil || dto.Proposal.ID != "act-1" || dto.Proposal.Status != "pending" || len(dto.Proposal.Fields) != 1 {
		t.Fatalf("proposal = %+v", dto.Proposal)
	}
	out, _ := json.Marshal(dto)
	var wire map[string]map[string]interface{}
	_ = json.Unmarshal(out, &wire)
	if _, leaked := wire["proposal"]["args"]; leaked {
		t.Fatal("raw tool arguments must not reach the browser")
	}
}

func TestMessageDTOWithoutProposal(t *testing.T) {
	if dto := toMessageDTO(&aichat.Message{ID: "m1", Role: aichat.RoleAssistant}); dto.Proposal != nil {
		t.Fatalf("proposal = %+v", dto.Proposal)
	}
}
