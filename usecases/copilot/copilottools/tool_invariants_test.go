package copilottools

import (
	"strings"
	"testing"

	"vozko/domain/copilot"
)

func operationToolset() []copilot.Tool {
	conversations := ConversationDeps{}
	leads := LeadDeps{}
	return []copilot.Tool{
		NewSearchConversationsTool(conversations), NewReadConversationTool(conversations),
		NewSearchLeadsTool(leads), NewGetLeadTool(leads),
		NewAddLeadMemoryTool(LeadMemoryDeps{}), NewUpdateLeadMemoryTool(LeadMemoryDeps{}),
		NewListKnowledgeBasesTool(KnowledgeDeps{}), NewSearchKnowledgeTool(KnowledgeDeps{}),
		NewListTemplatesTool(CatalogDeps{}), NewListPipelinesTool(CatalogDeps{}), NewListLabelsTool(CatalogDeps{}),
		NewListCalendarEventsTool(CatalogDeps{}), NewListWorkflowsTool(CatalogDeps{}),
		NewMoveConversationStageTool(StageMoveDeps{}), NewMoveConversationFunnelTool(StageMoveDeps{}),
		NewApplyLabelTool(LabelDeps{}), NewRemoveLabelTool(LabelDeps{}), NewCreateLabelTool(LabelDeps{}),
		NewSendMessageTool(ConversationActionDeps{}), NewScheduleMessageTool(ConversationActionDeps{}),
		NewCancelScheduledMessageTool(ConversationActionDeps{}), NewSendTemplateTool(TemplateSendDeps{}),
		NewListAssignableMembersTool(AssignmentDeps{}), NewAssignConversationTool(AssignmentDeps{}),
		NewTransferConversationTool(AssignmentDeps{}),
		NewPauseWorkflowTool(nil), NewActivateWorkflowTool(nil),
		NewCreateCalendarEventTool(nil),
		NewCreatePipelineTool(FunnelDeps{}), NewCreateStageTool(FunnelDeps{}), NewRenameStageTool(FunnelDeps{}),
		NewReorderStagesTool(FunnelDeps{}), NewSetInitialStageTool(FunnelDeps{}),
		NewListDealPipelinesTool(DealDeps{}), NewListDealsTool(DealDeps{}), NewCreateDealTool(DealDeps{}),
		NewMoveDealTool(DealDeps{}), NewLinkDealTool(DealDeps{}),
		NewListBusinessPhonesTool(TemplateCreateDeps{}), NewCreateTemplateTool(TemplateCreateDeps{}),
		NewPreviewCampaignImportTool(CampaignDeps{}), NewCreateCampaignTool(CampaignDeps{}), NewStartCampaignTool(CampaignDeps{}),
		NewCreateKnowledgeBaseTool(KnowledgeWriteDeps{}), NewAddKnowledgeDocumentTool(KnowledgeWriteDeps{}),
		NewCreateAgentTool(nil, AgentDeps{}), NewUpdateAgentTool(nil, nil, AgentDeps{}), NewDeleteAgentTool(nil, nil),
		NewListUnofficialNumbersTool(UnofficialCampaignDeps{}), NewPreviewUnofficialImportTool(UnofficialCampaignDeps{}),
		NewCreateUnofficialCampaignTool(UnofficialCampaignDeps{}), NewStartUnofficialCampaignTool(UnofficialCampaignDeps{}),
		NewGenerateImageTool(nil),
	}
}

func allTools() []copilot.Tool {
	tools := append(append(append(append(operationToolset(), adminTools(WorkspaceAdminDeps{})...), accessTools(AccessDeps{})...), callTools(CallDeps{})...), AdsTools(AdsDeps{})...)
	tools = append(tools, AdManageTools(AdManageDeps{}, AdsDeps{})...)
	tools = append(tools, AdGrowthTools(AdGrowthDeps{}, AdsDeps{})...)
	return append(tools, NewConnectAdAccountTool())
}

func TestEveryChangeIsCheckedBeforeTheUserSeesIt(t *testing.T) {
	for _, tool := range allTools() {
		if !tool.Meta().Mutating {
			continue
		}
		if _, ok := tool.(copilot.Validator); !ok {
			t.Errorf("%s proposes changes without a preflight Validate", tool.Definition().Name)
		}
	}
}

func TestNoToolLetsTheModelChooseTheWorkspace(t *testing.T) {
	for _, tool := range allTools() {
		for name := range tool.Definition().Parameters {
			if strings.Contains(strings.ToLower(name), "workspace") {
				t.Errorf("%s takes %q: the workspace must come from the session, never from the model", tool.Definition().Name, name)
			}
		}
	}
}

func TestEveryToolDeclaresAPermission(t *testing.T) {
	for _, tool := range allTools() {
		if m := tool.Meta(); m.Resource == "" || m.Action == "" {
			t.Errorf("%s has no permission", tool.Definition().Name)
		}
	}
}

func TestChangesToAConversationShowWhoTheyTouch(t *testing.T) {
	for _, tool := range allTools() {
		def := tool.Definition()
		_, targetsConversation := def.Parameters["entry_id"]
		if !tool.Meta().Mutating || !targetsConversation {
			continue
		}
		if _, ok := tool.(copilot.Describer); !ok {
			t.Errorf("%s changes a conversation but its approval card cannot name it", def.Name)
		}
	}
}

func TestConversationIdsAlwaysTravelWithTheirChannel(t *testing.T) {
	for _, tool := range allTools() {
		def := tool.Definition()
		if _, ok := def.Parameters["entry_id"]; !ok {
			continue
		}
		if _, ok := def.Parameters["entry_type"]; !ok {
			t.Errorf("%s takes entry_id without entry_type", def.Name)
		}
	}
}

func TestToolNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range allTools() {
		name := tool.Definition().Name
		if seen[name] {
			t.Errorf("duplicate tool %s", name)
		}
		seen[name] = true
	}
}

func TestSecretsNeverTravelThroughTheModel(t *testing.T) {
	for _, tool := range allTools() {
		def := tool.Definition()
		for name := range def.Parameters {
			lower := strings.ToLower(name)
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
				t.Errorf("%s offers the model a %q parameter; ask for it as a protected field instead", def.Name, name)
			}
		}
		asker, ok := tool.(copilot.SecretAsker)
		if !ok {
			continue
		}
		if !tool.Meta().Mutating {
			t.Errorf("%s asks for a secret without an approval card to type it in", def.Name)
		}
		for _, secret := range asker.Secrets(nil) {
			if _, offered := def.Parameters[secret.Key]; offered {
				t.Errorf("%s offers its secret %q to the model", def.Name, secret.Key)
			}
		}
	}
}

func TestEveryChangeNamesWhatItTouchesOnTheCard(t *testing.T) {
	for _, tool := range allTools() {
		if !tool.Meta().Mutating {
			continue
		}
		if _, ok := tool.(copilot.Describer); !ok {
			t.Errorf("%s proposes a change whose approval card falls back to the raw arguments", tool.Definition().Name)
		}
	}
}
