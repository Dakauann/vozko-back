package copilottools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

func validate(t *testing.T, tool copilot.Tool, cc copilot.Context, args map[string]interface{}) error {
	t.Helper()
	v, ok := tool.(copilot.Validator)
	if !ok {
		t.Fatalf("%s has no preflight", tool.Definition().Name)
	}
	return v.Validate(context.Background(), cc, args)
}

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
	if err := validate(t, NewCreateTemplateTool(templateCreateDeps(&fakeTemplateCreator{})), member(), newTemplateArgs()); err != nil {
		t.Fatalf("valid draft refused: %v", err)
	}
}

func TestTemplatePreflightRefusesWhatWouldFailAfterApproval(t *testing.T) {
	cases := map[string]struct {
		changes map[string]interface{}
		want    string
	}{
		"invented phone":    {map[string]interface{}{"business_phone_id": "0199f6d4-8761-7000-b3a1-example01"}, "nunca invente"},
		"foreign phone":     {map[string]interface{}{"business_phone_id": "9f8e7d6c-5b4a-4392-8170-000000000000"}, "list_business_phones"},
		"meta name rule":    {map[string]interface{}{"name": "Refiliação Convite"}, "regras da Meta"},
		"variable, no body": {map[string]interface{}{"body_examples": []interface{}{}}, "regras da Meta"},
		"authentication":    {map[string]interface{}{"category": "AUTHENTICATION"}, "MARKETING ou UTILITY"},
		"foreign header":    {map[string]interface{}{"header_media_id": "7e6d5c4b-3a29-4180-9f7e-6d5c4b3a2918"}, "anexado"},
	}
	for label, c := range cases {
		err := validate(t, NewCreateTemplateTool(templateCreateDeps(&fakeTemplateCreator{})), member(), withArgs(newTemplateArgs(), c.changes))
		if !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", label, err)
		}
	}
}

func TestTemplatePreflightExplainsThatANumberMustBeConnectedFirst(t *testing.T) {
	deps := templateCreateDeps(&fakeTemplateCreator{})
	deps.Phones = &fakeWorkspacePhones{none: true}
	err := validate(t, NewCreateTemplateTool(deps), member(), newTemplateArgs())
	if err == nil || !strings.Contains(err.Error(), "conectar um número") {
		t.Fatalf("no-number case = %v", err)
	}
}

func TestCampaignPreflightChecksRowsAndStartability(t *testing.T) {
	f := newCampaignFixture()
	f.deps.Preview = emptyPreview{}
	if err := validate(t, NewCreateCampaignTool(f.deps), member(), createCampaignArgsFor(nil)); err == nil || !strings.Contains(err.Error(), "nenhuma linha válida") {
		t.Fatalf("empty sheet = %v", err)
	}
	if err := validate(t, NewCreateCampaignTool(newCampaignFixture().deps), member(), createCampaignArgsFor(nil)); err != nil {
		t.Fatalf("valid campaign refused: %v", err)
	}
	f = newCampaignFixture()
	f.deps.Access = doneCampaignAccess{}
	if err := validate(t, NewStartCampaignTool(f.deps), member(), map[string]interface{}{"campaign_id": knownCampaign}); err == nil || !strings.Contains(err.Error(), "pendentes") {
		t.Fatalf("processed campaign = %v", err)
	}
	cc := member()
	cc.WorkspaceID = "ws-2"
	if err := validate(t, NewStartCampaignTool(newCampaignFixture().deps), cc, map[string]interface{}{"campaign_id": knownCampaign}); err == nil {
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
	if err := validate(t, tool, member(), args); err == nil || !strings.Contains(err.Error(), "search_conversations") {
		t.Fatalf("hidden conversation = %v", err)
	}
	visible := NewMoveConversationStageTool(stageMoveDeps(&fakeMove{}, &fakeEntries{}, &fakeStageBroadcast{}))
	if err := validate(t, visible, member(), args); err != nil {
		t.Fatalf("visible conversation refused: %v", err)
	}
}

func TestKnowledgeDocumentPreflightChecksBaseAndFile(t *testing.T) {
	deps, _, _ := knowledgeWriteDeps()
	tool := NewAddKnowledgeDocumentTool(deps)
	if err := validate(t, tool, member(), map[string]interface{}{"knowledge_base_id": knownKnowledgeBase, "media_id": knownSheet}); err != nil {
		t.Fatalf("valid document refused: %v", err)
	}
	if err := validate(t, tool, member(), map[string]interface{}{"knowledge_base_id": knownKnowledgeBase, "media_id": knownCampaign}); err == nil {
		t.Fatal("unknown file passed preflight")
	}
}
