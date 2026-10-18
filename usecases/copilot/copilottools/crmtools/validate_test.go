package crmtools

import (
	"errors"
	"strings"
	"testing"

	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func withArgs(base map[string]interface{}, changes map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out
}

func TestTemplatePreflightAcceptsAMetaReadyDraft(t *testing.T) {
	if err := tooltest.Validate(t, NewCreateTemplateTool(templateCreateDeps(&fakeTemplateCreator{})), tooltest.Member(), newTemplateArgs()); err != nil {
		t.Fatalf("valid draft refused: %v", err)
	}
}

func TestTemplatePreflightRefusesWhatWouldFailAfterApproval(t *testing.T) {
	cases := map[string]struct {
		changes map[string]interface{}
		want    string
	}{
		"invented phone":      {map[string]interface{}{"business_phone_id": "0199f6d4-8761-7000-b3a1-example01"}, "nunca invente"},
		"foreign phone":       {map[string]interface{}{"business_phone_id": "9f8e7d6c-5b4a-4392-8170-000000000000"}, "list_business_phones"},
		"meta name rule":      {map[string]interface{}{"name": "Refiliação Convite"}, "regras da Meta"},
		"variable, no body":   {map[string]interface{}{"body_examples": []interface{}{}}, "regras da Meta"},
		"authentication":      {map[string]interface{}{"category": "AUTHENTICATION"}, "MARKETING ou UTILITY"},
		"foreign header":      {map[string]interface{}{"header_media_id": "7e6d5c4b-3a29-4180-9f7e-6d5c4b3a2918"}, "anexado"},
		"marketing, no exit":  {map[string]interface{}{"footer": "Responda quando puder"}, "/sair"},
		"exit word, no slash": {map[string]interface{}{"footer": "Responda SAIR para não receber"}, "/sair"},
	}
	for label, c := range cases {
		err := tooltest.Validate(t, NewCreateTemplateTool(templateCreateDeps(&fakeTemplateCreator{})), tooltest.Member(), withArgs(newTemplateArgs(), c.changes))
		if !errors.Is(err, copilottools.ErrInvalidArgs) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", label, err)
		}
	}
}

func TestTemplatePreflightLetsAUtilityTemplateGoWithoutAnExit(t *testing.T) {
	args := withArgs(newTemplateArgs(), map[string]interface{}{"category": "UTILITY", "footer": "Responda quando puder"})
	if err := tooltest.Validate(t, NewCreateTemplateTool(templateCreateDeps(&fakeTemplateCreator{})), tooltest.Member(), args); err != nil {
		t.Fatalf("utility without exit refused: %v", err)
	}
}

func TestTemplatePreflightExplainsThatANumberMustBeConnectedFirst(t *testing.T) {
	deps := templateCreateDeps(&fakeTemplateCreator{})
	deps.Phones = &fakeWorkspacePhones{none: true}
	err := tooltest.Validate(t, NewCreateTemplateTool(deps), tooltest.Member(), newTemplateArgs())
	if err == nil || !strings.Contains(err.Error(), "conectar um número") {
		t.Fatalf("no-number case = %v", err)
	}
}

func TestCampaignPreflightChecksRowsAndStartability(t *testing.T) {
	f := newCampaignFixture()
	f.deps.Preview = emptyPreview{}
	if err := tooltest.Validate(t, NewCreateCampaignTool(f.deps), tooltest.Member(), createCampaignArgsFor(nil)); err == nil || !strings.Contains(err.Error(), "nenhuma linha válida") {
		t.Fatalf("empty sheet = %v", err)
	}
	if err := tooltest.Validate(t, NewCreateCampaignTool(newCampaignFixture().deps), tooltest.Member(), createCampaignArgsFor(nil)); err != nil {
		t.Fatalf("valid campaign refused: %v", err)
	}
	f = newCampaignFixture()
	f.deps.Access = doneCampaignAccess{}
	if err := tooltest.Validate(t, NewStartCampaignTool(f.deps), tooltest.Member(), map[string]interface{}{"campaign_id": knownCampaign}); err == nil || !strings.Contains(err.Error(), "pendentes") {
		t.Fatalf("processed campaign = %v", err)
	}
	cc := tooltest.Member()
	cc.WorkspaceID = "ws-2"
	if err := tooltest.Validate(t, NewStartCampaignTool(newCampaignFixture().deps), cc, map[string]interface{}{"campaign_id": knownCampaign}); err == nil {
		t.Fatal("a foreign campaign passed preflight")
	}
}

type doneCampaignAccess struct{ fakeCampaignAccess }

func (doneCampaignAccess) Owned(string, *wd.DepartmentFilter, string) (*wc.Campaign, error) {
	return &wc.Campaign{ID: knownCampaign, Metrics: &wc.CampaignMetrics{TotalNumbers: 2, Processed: 2}}, nil
}

func TestConversationPreflightNeedsAVisibleConversation(t *testing.T) {
	entries := &fakeEntries{err: errors.New("not visible")}
	tool := NewMoveConversationStageTool(stageMoveDeps(&fakeMove{}, entries, &fakeStageBroadcast{}))
	args := map[string]interface{}{"entry_id": knownEntry, "entry_type": "whatsapp", "stage_id": knownStage}
	if err := tooltest.Validate(t, tool, tooltest.Member(), args); err == nil || !strings.Contains(err.Error(), "search_conversations") {
		t.Fatalf("hidden conversation = %v", err)
	}
	visible := NewMoveConversationStageTool(stageMoveDeps(&fakeMove{}, &fakeEntries{}, &fakeStageBroadcast{}))
	if err := tooltest.Validate(t, visible, tooltest.Member(), args); err != nil {
		t.Fatalf("visible conversation refused: %v", err)
	}
}

func TestKnowledgeDocumentPreflightChecksBaseAndFile(t *testing.T) {
	deps, _, _ := knowledgeWriteDeps()
	tool := NewAddKnowledgeDocumentTool(deps)
	if err := tooltest.Validate(t, tool, tooltest.Member(), map[string]interface{}{"knowledge_base_id": knownKnowledgeBase, "media_id": knownSheet}); err != nil {
		t.Fatalf("valid document refused: %v", err)
	}
	if err := tooltest.Validate(t, tool, tooltest.Member(), map[string]interface{}{"knowledge_base_id": knownKnowledgeBase, "media_id": knownCampaign}); err == nil {
		t.Fatal("unknown file passed preflight")
	}
}
