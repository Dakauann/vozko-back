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

func TestMessageDTOAsksForTheSecretFieldsTheToolNeeds(t *testing.T) {
	raw, _ := json.Marshal(copilot_domain.PendingAction{ID: "act-1", ToolName: "create_phone_line",
		Secrets: []copilot_domain.SecretField{{Key: "password", Label: "Senha"}}})
	dto := toMessageDTO(&aichat.Message{ID: "m1", Role: aichat.RoleAssistant, ProposalID: "act-1", Proposal: raw, ProposalStatus: aichat.ProposalPending})
	if dto.Proposal == nil || len(dto.Proposal.Secrets) != 1 || dto.Proposal.Secrets[0].Key != "password" {
		t.Fatalf("proposal = %+v", dto.Proposal)
	}
}

func TestMessageDTOKeepsAGeneratedImageForTheReloadedThread(t *testing.T) {
	raw := []byte(`[{"name":"generate_image","summary":"ok","ok":true,"image":{"url":"https://cdn/x.jpg","mediaId":"m-1","alt":"um card"}}]`)
	dto := toMessageDTO(&aichat.Message{ID: "m1", Role: aichat.RoleAssistant, ToolCalls: raw})
	if len(dto.Tools) != 1 || dto.Tools[0].Media == nil || dto.Tools[0].Media.URL != "https://cdn/x.jpg" || dto.Tools[0].Media.MediaID != "m-1" {
		t.Fatalf("tools = %+v", dto.Tools)
	}
}
