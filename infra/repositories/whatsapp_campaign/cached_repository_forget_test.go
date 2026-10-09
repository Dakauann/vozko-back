package whatsapp_campaign_repository

import "testing"

func TestForgetDropsTheCachedCampaign(t *testing.T) {
	state := newFakeSharedState()
	state.data[cacheKey("c-1")] = `{"id":"c-1"}`
	repo := &CachedRepository{shared: state, ttl: campaignCacheTTL}
	repo.Forget("c-1")
	if _, cached := state.data[cacheKey("c-1")]; cached {
		t.Fatal("the deleted campaign is still cached")
	}
}
