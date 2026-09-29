package stage_usecase

import (
	"errors"
	"testing"

	"vozko/domain/stage"
)

type funnelData struct {
	stages     map[string]*stage.Stage
	byPipeline map[string][]*stage.Stage
	byCampaign map[string][]*stage.Stage
	calls      map[string]int
	err        error
}

func (f *funnelData) count(key string) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[key]++
}

func (f *funnelData) FindByID(id string) (*stage.Stage, error) {
	f.count("find:" + id)
	if f.err != nil {
		return nil, f.err
	}
	return f.stages[id], nil
}

func (f *funnelData) ListByPipeline(_, pipelineID string) ([]*stage.Stage, error) {
	f.count("pipeline:" + pipelineID)
	return f.byPipeline[pipelineID], nil
}

func (f *funnelData) ListByCampaign(_, campaignID, campaignType string) ([]*stage.Stage, error) {
	f.count("campaign:" + campaignType + ":" + campaignID)
	return f.byCampaign[campaignType+":"+campaignID], nil
}

var (
	emAndamento  = &stage.Stage{ID: "em-andamento", WorkspaceID: "ws", PipelineID: "suporte", Name: "em andamento"}
	prospectando = &stage.Stage{ID: "prospectando", WorkspaceID: "ws", PipelineID: "suporte", Name: "prospectando"}
	recebido     = &stage.Stage{ID: "recebido", WorkspaceID: "ws", PipelineID: "atendimento", Name: "recebido"}
	agendamento  = &stage.Stage{ID: "agendamento", WorkspaceID: "ws", PipelineID: "atendimento", Name: "agendamento"}
)

func twoFunnels() *funnelData {
	return &funnelData{
		stages: map[string]*stage.Stage{emAndamento.ID: emAndamento, recebido.ID: recebido},
		byPipeline: map[string][]*stage.Stage{
			"suporte":     {emAndamento, prospectando},
			"atendimento": {recebido, agendamento},
		},
		byCampaign: map[string][]*stage.Stage{"unofficial_whatsapp:camp-1": {recebido, agendamento}},
	}
}

func TestAConversationUsesTheFunnelOfTheStageItIsIn(t *testing.T) {
	data := twoFunnels()

	stages, err := NewEntryFunnel(data).Stages("ws", Placement{CurrentStageID: "em-andamento", CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"})

	if err != nil || len(stages) != 2 || stages[0] != emAndamento || stages[1] != prospectando {
		t.Fatalf("stages = %v, err = %v", stages, err)
	}
}

func TestAConversationWithoutAStageUsesItsCampaignFunnel(t *testing.T) {
	stages, err := NewEntryFunnel(twoFunnels()).Stages("ws", Placement{CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"})

	if err != nil || len(stages) != 2 || stages[1] != agendamento {
		t.Fatalf("stages = %v, err = %v", stages, err)
	}
}

func TestAStageFromAnotherWorkspaceIsRefused(t *testing.T) {
	data := twoFunnels()
	data.stages["foreign"] = &stage.Stage{ID: "foreign", WorkspaceID: "other", PipelineID: "suporte"}

	_, err := NewEntryFunnel(data).Stages("ws", Placement{CurrentStageID: "foreign"})

	if !errors.Is(err, ErrStageOutsideWorkspace) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnUnreadableStageIsAnErrorNotTheDefaultFunnel(t *testing.T) {
	data := twoFunnels()
	data.err = errors.New("db down")

	if _, err := NewEntryFunnel(data).Stages("ws", Placement{CurrentStageID: "em-andamento", CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"}); err == nil {
		t.Fatal("an unreadable current stage must not fall back to another funnel")
	}
}

func TestAStageThatNoLongerExistsFallsBackToTheCampaignFunnel(t *testing.T) {
	stages, err := NewEntryFunnel(twoFunnels()).Stages("ws", Placement{CurrentStageID: "deleted", CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"})

	if err != nil || len(stages) != 2 || stages[0] != recebido {
		t.Fatalf("stages = %v, err = %v", stages, err)
	}
}

func TestOneFunnelIsReadOncePerRequest(t *testing.T) {
	data := twoFunnels()
	funnel := NewEntryFunnel(data)

	for range 3 {
		_, _ = funnel.Stages("ws", Placement{CurrentStageID: "em-andamento"})
		_, _ = funnel.Stages("ws", Placement{CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"})
	}

	if data.calls["find:em-andamento"] != 1 || data.calls["pipeline:suporte"] != 1 || data.calls["campaign:unofficial_whatsapp:camp-1"] != 1 {
		t.Fatalf("calls = %v", data.calls)
	}
}

func TestAnOlderStageWithoutAFunnelUsesItsOwnCampaign(t *testing.T) {
	data := twoFunnels()
	data.stages["legacy"] = &stage.Stage{ID: "legacy", WorkspaceID: "ws", CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"}

	stages, err := NewEntryFunnel(data).Stages("ws", Placement{CurrentStageID: "legacy", CampaignID: "other", CampaignType: "whatsapp"})

	if err != nil || len(stages) != 2 || stages[1] != agendamento {
		t.Fatalf("stages = %v, err = %v", stages, err)
	}
}

type entryStages struct {
	*funnelData
	current *stage.EntryStage
	err     error
}

func (e entryStages) GetEntryStage(string, string, string) (*stage.EntryStage, error) {
	return e.current, e.err
}

func TestAnEntrysStagesAreItsCurrentStageAndThatFunnel(t *testing.T) {
	reader := entryStages{funnelData: twoFunnels(), current: &stage.EntryStage{StageID: "em-andamento", StageName: "em andamento"}}

	got, err := StagesForEntry(reader, "ws", EntryRef{EntryID: "e1", EntryType: "unofficial_whatsapp", CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"})

	if err != nil || got.Current.StageName != "em andamento" || len(got.Stages) != 2 || got.Stages[1] != prospectando {
		t.Fatalf("got = %+v, err = %v", got, err)
	}
}

func TestAnEntryWhoseStageCannotBeReadHasNoStages(t *testing.T) {
	reader := entryStages{funnelData: twoFunnels(), err: errors.New("db down")}

	if _, err := StagesForEntry(reader, "ws", EntryRef{EntryID: "e1", EntryType: "unofficial_whatsapp", CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"}); err == nil {
		t.Fatal("an unreadable stage must not fall back to the campaign funnel")
	}
}
