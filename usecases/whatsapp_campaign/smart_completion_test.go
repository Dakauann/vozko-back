package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	wc "vozko/domain/whatsapp_campaign"
)

type smartIndex struct {
	managed map[string]bool
	err     error
}

func (s smartIndex) Manages(campaignID string) (bool, error) {
	return s.managed[campaignID], s.err
}

func drainedStore(smart SmartCampaigns) (campaignStatusStore, *mockCampaignRepo) {
	repo := newMockCampaignRepo()
	repo.campaigns["plain"] = &wc.Campaign{ID: "plain", Status: wc.CampaignStatusRunning}
	repo.campaigns["smart"] = &wc.Campaign{ID: "smart", Status: wc.CampaignStatusRunning}
	owner := &messageConsumerUseCase{CampaignRepo: repo}
	owner.SetSmartCampaigns(smart)
	return campaignStatusStore{repo: repo, owner: owner}, repo
}

func TestADrainedQueueStillCompletesACampaignWithoutPolicy(t *testing.T) {
	store, repo := drainedStore(smartIndex{managed: map[string]bool{"smart": true}})
	done, err := store.CompleteCampaign("plain")
	if err != nil || !done || repo.campaigns["plain"].Status != wc.CampaignStatusCompleted {
		t.Fatalf("done=%v err=%v status=%s, want completed as today", done, err, repo.campaigns["plain"].Status)
	}
}

func TestADrainedQueueLeavesASmartCampaignToItsEngine(t *testing.T) {
	store, repo := drainedStore(smartIndex{managed: map[string]bool{"smart": true}})
	done, err := store.CompleteCampaign("smart")
	if err != nil || done || repo.campaigns["smart"].Status != wc.CampaignStatusRunning {
		t.Fatalf("done=%v err=%v status=%s, want the wave engine to decide", done, err, repo.campaigns["smart"].Status)
	}
}

func TestADrainedQueueIsNotCompletedWhenThePolicyCannotBeRead(t *testing.T) {
	store, repo := drainedStore(smartIndex{err: errors.New("db down")})
	done, err := store.CompleteCampaign("plain")
	if err == nil || done || repo.campaigns["plain"].Status != wc.CampaignStatusRunning {
		t.Fatalf("done=%v err=%v, want no completion while unsure", done, err)
	}
}
