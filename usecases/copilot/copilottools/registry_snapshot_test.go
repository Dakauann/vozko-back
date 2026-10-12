package copilottools_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vozko/domain/copilot"
	copilot_usecase "vozko/usecases/copilot"
	"vozko/usecases/copilot/copilottools/adtools"
	"vozko/usecases/copilot/copilottools/attendancetools"
	"vozko/usecases/copilot/copilottools/crmtools"
	"vozko/usecases/copilot/copilottools/leadtools"
	"vozko/usecases/copilot/copilottools/mediatools"
	"vozko/usecases/copilot/copilottools/platformtools"
	"vozko/usecases/copilot/copilottools/skilltools"
	"vozko/usecases/copilot/copilottools/studiotools"
	"vozko/usecases/copilot/copilottools/telephonytools"
	"vozko/usecases/copilot/skills"
)

type snapshotView struct {
	name string
	view copilot.View
}

var snapshotViews = []snapshotView{
	{name: "panel", view: copilot.View{}},
	{name: "attendance", view: copilot.View{Surface: copilot.SurfaceAttendance}},
	{name: "leads", view: copilot.View{Surface: copilot.SurfaceLeads}},
	{name: "studio-image", view: copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: copilot.StudioImage}},
	{name: "studio-video", view: copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: copilot.StudioVideo}},
}

func registeredTools(t *testing.T) []copilot.Tool {
	t.Helper()
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	attendance := attendancetools.AttendanceDeps{}
	agents := platformtools.AgentDeps{}
	workspace := platformtools.WorkspaceAdminDeps{}
	access := platformtools.AccessDeps{}
	lines := telephonytools.PhoneLineDeps{}
	queues := telephonytools.CallQueueDeps{}
	history := telephonytools.CallHistoryDeps{}
	toolset := []copilot.Tool{
		platformtools.NewListAgentsTool(nil),
		platformtools.NewCountAgentsTool(nil),
		platformtools.NewGetAgentTool(nil, nil),
		platformtools.NewCreateAgentTool(nil, agents),
		platformtools.NewUpdateAgentTool(nil, nil, agents),
		platformtools.NewDeleteAgentTool(nil, nil),
		platformtools.NewListDepartmentsTool(nil),
		platformtools.NewOfferActionTool(nil),
		platformtools.NewAskUserTool(),
		studiotools.NewNewStudioProjectTool(studiotools.StudioDeps{}),
		platformtools.NewListModelsTool(nil),
		platformtools.NewListAgentToolsTool(nil),
		attendancetools.NewAttendanceMetricsTool(attendance),
		attendancetools.NewAttendanceTrendTool(attendance),
		attendancetools.NewAttendanceTeamTool(attendance),
		attendancetools.NewMemberActivityTool(nil),
		attendancetools.NewAttendanceBacklogTool(attendance),
		attendancetools.NewAttendanceStagesTool(attendance),
		attendancetools.NewAttendanceReworkTool(attendance),
		attendancetools.NewAttendanceLiveTool(attendance),
		attendancetools.NewCampaignDispatchTool(nil, attendance),
		attendancetools.NewConversationInsightsTool(nil, nil, nil),
		attendancetools.NewCalculateTool(),
		attendancetools.NewQueryDatasetTool(),
		attendancetools.NewRenderChartTool(),
		skilltools.NewLoadSkillTool(library),
		crmtools.NewSearchConversationsTool(crmtools.ConversationDeps{}),
		crmtools.NewReadConversationTool(crmtools.ConversationDeps{}),
		leadtools.NewSearchLeadsTool(leadtools.LeadDeps{}),
		leadtools.NewGetLeadTool(leadtools.LeadDeps{}),
		leadtools.NewLeadGeoSummaryTool(leadtools.LeadDeps{}),
		leadtools.NewAddLeadMemoryTool(leadtools.LeadMemoryDeps{}),
		leadtools.NewUpdateLeadMemoryTool(leadtools.LeadMemoryDeps{}),
		leadtools.NewPrepareLeadActionTool(leadtools.LeadActionDeps{}),
		leadtools.NewStartLeadSendTool(leadtools.LeadActionDeps{}),
		leadtools.NewCancelLeadSendTool(leadtools.LeadActionDeps{}),
		crmtools.NewListKnowledgeBasesTool(crmtools.KnowledgeDeps{}),
		crmtools.NewSearchKnowledgeTool(crmtools.KnowledgeDeps{}),
		crmtools.NewCreateKnowledgeBaseTool(crmtools.KnowledgeWriteDeps{}),
		crmtools.NewAddKnowledgeDocumentTool(crmtools.KnowledgeWriteDeps{}),
		crmtools.NewListTemplatesTool(crmtools.CatalogDeps{}),
		crmtools.NewListPipelinesTool(crmtools.CatalogDeps{}),
		crmtools.NewListLabelsTool(crmtools.CatalogDeps{}),
		crmtools.NewListCalendarEventsTool(crmtools.CatalogDeps{}),
		crmtools.NewListWorkflowsTool(crmtools.CatalogDeps{}),
		crmtools.NewMoveConversationStageTool(crmtools.StageMoveDeps{}),
		crmtools.NewMoveConversationFunnelTool(crmtools.StageMoveDeps{}),
		crmtools.NewApplyLabelTool(crmtools.LabelDeps{}),
		crmtools.NewRemoveLabelTool(crmtools.LabelDeps{}),
		crmtools.NewCreateLabelTool(crmtools.LabelDeps{}),
		crmtools.NewSendMessageTool(crmtools.ConversationActionDeps{}),
		crmtools.NewScheduleMessageTool(crmtools.ConversationActionDeps{}),
		crmtools.NewCancelScheduledMessageTool(crmtools.ConversationActionDeps{}),
		crmtools.NewSendTemplateTool(crmtools.TemplateSendDeps{}),
		crmtools.NewListAssignableMembersTool(crmtools.AssignmentDeps{}),
		crmtools.NewAssignConversationTool(crmtools.AssignmentDeps{}),
		crmtools.NewTransferConversationTool(crmtools.AssignmentDeps{}),
		crmtools.NewCreatePipelineTool(crmtools.FunnelDeps{}),
		crmtools.NewCreateStageTool(crmtools.FunnelDeps{}),
		crmtools.NewRenameStageTool(crmtools.FunnelDeps{}),
		crmtools.NewReorderStagesTool(crmtools.FunnelDeps{}),
		crmtools.NewSetInitialStageTool(crmtools.FunnelDeps{}),
		crmtools.NewListDealPipelinesTool(crmtools.DealDeps{}),
		crmtools.NewListDealsTool(crmtools.DealDeps{}),
		crmtools.NewCreateDealTool(crmtools.DealDeps{}),
		crmtools.NewMoveDealTool(crmtools.DealDeps{}),
		crmtools.NewLinkDealTool(crmtools.DealDeps{}),
		crmtools.NewListBusinessPhonesTool(crmtools.TemplateCreateDeps{}),
		crmtools.NewCreateTemplateTool(crmtools.TemplateCreateDeps{}),
		crmtools.NewPreviewCampaignImportTool(crmtools.CampaignDeps{}),
		crmtools.NewCreateCampaignTool(crmtools.CampaignDeps{}),
		crmtools.NewStartCampaignTool(crmtools.CampaignDeps{}),
		crmtools.NewListUnofficialNumbersTool(crmtools.UnofficialCampaignDeps{}),
		crmtools.NewPreviewUnofficialImportTool(crmtools.UnofficialCampaignDeps{}),
		crmtools.NewCreateUnofficialCampaignTool(crmtools.UnofficialCampaignDeps{}),
		crmtools.NewStartUnofficialCampaignTool(crmtools.UnofficialCampaignDeps{}),
		platformtools.NewListWorkspaceMembersTool(workspace),
		platformtools.NewListWorkspaceInvitesTool(workspace),
		platformtools.NewGetMemberPermissionsTool(workspace),
		platformtools.NewListPermissionCatalogTool(workspace),
		platformtools.NewInviteMemberTool(workspace),
		platformtools.NewCancelInviteTool(workspace),
		platformtools.NewRemoveMemberTool(workspace),
		platformtools.NewChangeMemberRoleTool(workspace),
		platformtools.NewUpdateMemberPermissionsTool(workspace),
		platformtools.NewListRolesTool(workspace),
		platformtools.NewCreateRoleTool(workspace),
		platformtools.NewUpdateRoleTool(workspace),
		platformtools.NewDeleteRoleTool(workspace),
		platformtools.NewListDepartmentMembersTool(workspace),
		platformtools.NewCreateDepartmentTool(workspace),
		platformtools.NewUpdateDepartmentTool(workspace),
		platformtools.NewDeleteDepartmentTool(workspace),
		platformtools.NewAddDepartmentMemberTool(workspace),
		platformtools.NewRemoveDepartmentMemberTool(workspace),
		platformtools.NewExplainPermissionTool(),
		platformtools.NewDiagnoseAccessTool(access),
		platformtools.NewOpenScreenTool(access),
		telephonytools.NewPlaceCallTool(telephonytools.CallDeps{}),
		telephonytools.NewListCallsTool(history),
		telephonytools.NewGetCallTool(history),
		telephonytools.NewListPhoneLinesTool(lines),
		telephonytools.NewCreatePhoneLineTool(lines),
		telephonytools.NewUpdatePhoneLineTool(lines),
		telephonytools.NewChangePhoneLinePasswordTool(lines),
		telephonytools.NewDeletePhoneLineTool(lines),
		telephonytools.NewListCallQueuesTool(queues),
		telephonytools.NewCreateCallQueueTool(queues),
		telephonytools.NewUpdateCallQueueTool(queues),
		telephonytools.NewDeleteCallQueueTool(queues),
		crmtools.NewListTelegramBotsTool(crmtools.TelegramDeps{}),
		crmtools.NewConnectTelegramBotTool(crmtools.TelegramDeps{}),
		mediatools.NewGenerateImageTool(nil),
		mediatools.NewGenerateMusicTool(nil),
		mediatools.NewGenerateVoiceoverTool(nil),
		mediatools.NewRenderVideoTool(nil),
		crmtools.NewCreateCalendarEventTool(nil),
		crmtools.NewPauseWorkflowTool(nil),
		crmtools.NewActivateWorkflowTool(nil),
		platformtools.NewNumberAutomationTool(platformtools.NumberAutomationDeps{}),
		platformtools.NewConfigureNumberAutomationTool(platformtools.NumberAutomationDeps{}),
		adtools.NewConnectAdAccountTool(),
	}
	toolset = append(toolset, adtools.AllAdTools(adtools.AdsDeps{}, adtools.AdManageDeps{}, adtools.AdGrowthDeps{})...)
	toolset = append(toolset, studiotools.StudioTools(studiotools.StudioDeps{})...)
	return copilot_usecase.NewRegistry(toolset...).Tools()
}

func registeredNames(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, tool := range registeredTools(t) {
		b.WriteString(tool.Definition().Name)
		b.WriteString("\n")
	}
	return b.String()
}

func definitionDigests(t *testing.T, studio bool) string {
	t.Helper()
	registered := registeredTools(t)
	var b strings.Builder
	for _, entry := range snapshotViews {
		for _, tool := range registered {
			name := tool.Definition().Name
			if strings.HasPrefix(name, "studio_") != studio || !entry.view.Offers(tool) {
				continue
			}
			raw, err := json.Marshal(copilot.DefinitionOn(tool, entry.view))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			b.WriteString(entry.name + "\t" + name + "\t" + hex.EncodeToString(sum[:]) + "\n")
		}
	}
	return b.String()
}

func goldenLines(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
}

func compareWithGolden(t *testing.T, name, got string) {
	t.Helper()
	want := goldenLines(t, name)
	have := strings.Split(got, "\n")
	for i := 0; i < len(want) || i < len(have); i++ {
		var w, h string
		if i < len(want) {
			w = want[i]
		}
		if i < len(have) {
			h = have[i]
		}
		if w != h {
			t.Fatalf("%s differs at line %d: want %q, got %q", name, i+1, w, h)
		}
	}
}

func TestTheRegistryKeepsItsToolNamesAndOrder(t *testing.T) {
	compareWithGolden(t, "registry_tool_names.golden", registeredNames(t))
}

func TestNonStudioToolDefinitionsStayByteIdentical(t *testing.T) {
	compareWithGolden(t, "registry_definitions.golden", definitionDigests(t, false))
}

func TestStudioToolDefinitionsMatchTheirSnapshot(t *testing.T) {
	compareWithGolden(t, "registry_studio_definitions.golden", definitionDigests(t, true))
}
