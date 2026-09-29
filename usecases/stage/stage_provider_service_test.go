package stage_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/stage"
)

type funnelRepo struct {
	stage.Repository
	*funnelData
}

func (r funnelRepo) FindByID(id string) (*stage.Stage, error) { return r.funnelData.FindByID(id) }
func (r funnelRepo) ListByPipeline(ws, pipelineID string) ([]*stage.Stage, error) {
	return r.funnelData.ListByPipeline(ws, pipelineID)
}
func (r funnelRepo) ListByCampaign(ws, campaignID, campaignType string) ([]*stage.Stage, error) {
	return r.funnelData.ListByCampaign(ws, campaignID, campaignType)
}

func TestTheMoveMenuOffersTheFunnelEachConversationIsIn(t *testing.T) {
	provider := NewStageProviderService(funnelRepo{funnelData: twoFunnels()})

	available, err := provider.GetAvailableStages("ws", []conversation.StagePlacement{
		{CurrentStageID: "em-andamento", CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"},
		{CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"},
	})

	if err != nil || len(available) != 2 {
		t.Fatalf("available = %v, err = %v", available, err)
	}
	if available[0][0].Name != "em andamento" || available[0][1].Name != "prospectando" {
		t.Fatalf("a conversation in suporte must be offered suporte: %+v", available[0])
	}
	if available[1][1].StageID != "agendamento" {
		t.Fatalf("a conversation without a stage is offered its campaign funnel: %+v", available[1])
	}
}

func TestTheMoveMenuFailsWhenAStageCannotBeRead(t *testing.T) {
	data := twoFunnels()
	data.err = errors.New("db down")
	provider := NewStageProviderService(funnelRepo{funnelData: data})

	if _, err := provider.GetAvailableStages("ws", []conversation.StagePlacement{{CurrentStageID: "em-andamento"}}); err == nil {
		t.Fatal("an unreadable stage must not silently offer another funnel")
	}
}

func TestAFullInboxPageReadsEachStageAndFunnelOnce(t *testing.T) {
	data := twoFunnels()
	provider := NewStageProviderService(funnelRepo{funnelData: data})
	placements := make([]conversation.StagePlacement, 0, 50)
	for i := 0; i < 25; i++ {
		placements = append(placements,
			conversation.StagePlacement{CurrentStageID: "em-andamento", CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"},
			conversation.StagePlacement{CampaignID: "camp-1", CampaignType: "unofficial_whatsapp"},
		)
	}

	if _, err := provider.GetAvailableStages("ws", placements); err != nil {
		t.Fatal(err)
	}

	total := 0
	for key, n := range data.calls {
		if n != 1 {
			t.Errorf("%s read %d times for one page", key, n)
		}
		total += n
	}
	if total > 3 {
		t.Fatalf("50 rows on one stage and one campaign took %d reads: %v", total, data.calls)
	}
}
