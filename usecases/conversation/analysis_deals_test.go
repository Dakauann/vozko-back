package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/dealautomation"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
	"vozko/domain/stage"
	toolsdomain "vozko/domain/tools"
	opportunity_usecase "vozko/usecases/opportunity"
	tools_usecase "vozko/usecases/tools"
)

type transcriptStub struct {
	conversation.MessageRepository
}

func (transcriptStub) ListByEntry(string, shared.EntryType) ([]*conversation.Message, error) {
	return []*conversation.Message{{Text: "Fechado, pode mandar o contrato de 1500"}}, nil
}

func (transcriptStub) CountByEntry(string, shared.EntryType) (int64, error) { return 1, nil }

type generateRecorder struct {
	ai.Service
	inputs []ai.GenerateInput
	err    error
}

func (g *generateRecorder) Generate(_ context.Context, input ai.GenerateInput) (*ai.GenerateOutput, error) {
	g.inputs = append(g.inputs, input)
	if g.err != nil {
		return nil, g.err
	}
	return &ai.GenerateOutput{}, nil
}

type registryStub struct {
	toolsdomain.Service
	handlers map[string]toolsdomain.Handler
}

func (r registryStub) Handler(name string) (toolsdomain.Handler, bool) {
	h, ok := r.handlers[name]
	return h, ok
}

type dealSettingsStub struct {
	pipeline string
	err      error
	asked    []dealautomation.Channel
}

func (s *dealSettingsStub) PipelineFor(_ string, channel dealautomation.Channel, _ string) (string, error) {
	s.asked = append(s.asked, channel)
	return s.pipeline, s.err
}

type dealDeskStub struct{}

func (dealDeskStub) PipelineStages(string, string) ([]*stage.Stage, error) {
	return []*stage.Stage{{ID: "st-open", Name: "Proposta"}}, nil
}

func (dealDeskStub) DealsForEntry(string, string, string, string) (opportunity.EntryDeals, error) {
	return opportunity.EntryDeals{{ID: "deal-1", Title: "Plano Pro", StageID: "st-open", Currency: "BRL", Status: opportunity.StatusOpen}}, nil
}

func (dealDeskStub) ManageForEntry(string, opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
	return nil, errors.New("not used")
}

type ownerStub struct{}

func (ownerStub) ConversationOwner(string, string, string) (string, error) { return "user-7", nil }

func dealJob(subject AnalysisSubject, settings *dealSettingsStub) (*analysisDebounceJob, *generateRecorder) {
	recorder := &generateRecorder{}
	job := &analysisDebounceJob{
		messageRepo: transcriptStub{},
		aiService:   recorder,
		toolRegistry: registryStub{handlers: map[string]toolsdomain.Handler{
			tools_usecase.AutoManageOpportunityToolName: tools_usecase.NewAutoManageOpportunityTool(dealDeskStub{}, ownerStub{}),
		}},
		resolvers: map[shared.EntryType]AnalysisSubjectResolver{
			shared.EntryTypeInstagram: func(context.Context, string) (*AnalysisSubject, error) { return &subject, nil },
		},
	}
	job.SetDealAutomation(settings, dealDeskStub{})
	return job, recorder
}

func instagramSubject() AnalysisSubject {
	return AnalysisSubject{
		EntryID: "entry-1", EntryType: shared.EntryTypeInstagram, WorkspaceID: "ws", ContainerID: "acc-1",
		ContainerName: "@loja", ContactLabel: "@maria", LeadID: "lead-1", AgentID: "agent-1",
	}
}

func TestAnalysisManagesDealsWhenTheChannelHasAFunnel(t *testing.T) {
	settings := &dealSettingsStub{pipeline: "deals"}
	job, recorder := dealJob(instagramSubject(), settings)
	if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err != nil {
		t.Fatal(err)
	}
	if len(recorder.inputs) != 1 {
		t.Fatalf("generate calls = %d", len(recorder.inputs))
	}
	input := recorder.inputs[0]
	if len(input.Tools) != 1 || input.Tools[0].Name != tools_usecase.AutoManageOpportunityToolName {
		t.Fatalf("tools = %+v", input.Tools)
	}
	config := input.ToolConfigs[tools_usecase.AutoManageOpportunityToolName]
	if config["pipeline_id"] != "deals" || config["__entry_id"] != "entry-1" || config["__agent_id"] != "agent-1" || config["__workspace_id"] != "ws" {
		t.Fatalf("config = %+v", config)
	}
	if note := lastMessage(input.Messages).Content; !strings.Contains(note, "deal-1") || !strings.Contains(input.SystemPrompt, "auto_manage_opportunity") {
		t.Fatalf("the system prompt must carry the rules and the note the current deals:\n%s\n%s", input.SystemPrompt, note)
	}
	want := dealautomation.Channel{EntryType: shared.EntryTypeInstagram, Kind: conversation.ContainerKindAccount}
	if len(settings.asked) != 1 || settings.asked[0] != want {
		t.Fatalf("asked for %+v", settings.asked)
	}
}

func TestAnalysisLeavesDealsAloneWithoutAFunnel(t *testing.T) {
	job, recorder := dealJob(instagramSubject(), &dealSettingsStub{})
	if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err != nil {
		t.Fatal(err)
	}
	if len(recorder.inputs) != 0 {
		t.Fatal("no work was configured, so no model call")
	}
}

func TestAnalysisRetriesWhenTheDealSettingCannotBeRead(t *testing.T) {
	job, recorder := dealJob(instagramSubject(), &dealSettingsStub{err: errors.New("db down")})
	if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err == nil {
		t.Fatal("a lookup failure must be retried, not silently skipped")
	}
	if len(recorder.inputs) != 0 {
		t.Fatal("no deal decision without the setting")
	}
}

func TestCampaignConversationsUseTheCampaignsSetting(t *testing.T) {
	subject := instagramSubject()
	subject.ContainerKind = conversation.ContainerKindCampaign
	settings := &dealSettingsStub{}
	job, _ := dealJob(subject, settings)
	_ = job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram)
	if len(settings.asked) != 1 || settings.asked[0].Kind != conversation.ContainerKindCampaign {
		t.Fatalf("asked for %+v", settings.asked)
	}
}
