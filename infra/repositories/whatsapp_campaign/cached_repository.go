package whatsapp_campaign_repository

import (
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/shared"
	wc "vozko/domain/whatsapp_campaign"
)

const campaignCacheTTL = 60 * time.Second

type CachedRepository struct {
	inner  wc.Repository
	shared cache.SharedState
	ttl    time.Duration
}

func NewCachedRepository(inner wc.Repository, shared cache.SharedState) wc.Repository {
	return &CachedRepository{inner: inner, shared: shared, ttl: campaignCacheTTL}
}

func cacheKey(campaignID string) string {
	return "wa:campaign:" + campaignID
}

func (r *CachedRepository) invalidate(campaignID string) {
	if r.shared == nil || campaignID == "" {
		return
	}
	_ = r.shared.Del(cacheKey(campaignID))
}

func (r *CachedRepository) FindByID(campaignID string) (*wc.Campaign, error) {
	id := strings.TrimSpace(campaignID)
	if id == "" {
		return r.inner.FindByID(campaignID)
	}
	if r.shared != nil {
		if raw, err := r.shared.GetString(cacheKey(id)); err == nil && raw != "" {
			var c wc.Campaign
			if json.Unmarshal([]byte(raw), &c) == nil && c.ID != "" {
				return &c, nil
			}
		}
	}

	c, err := r.inner.FindByID(id)
	if err != nil || c == nil {
		return c, err
	}
	if r.shared != nil {
		if data, mErr := json.Marshal(c); mErr == nil {
			_ = r.shared.SetString(cacheKey(id), string(data), r.ttl)
		}
	}
	return c, nil
}

func (r *CachedRepository) Create(campaign *wc.Campaign) error {
	err := r.inner.Create(campaign)
	if err == nil && campaign != nil {
		r.invalidate(campaign.ID)
	}
	return err
}

func (r *CachedRepository) Update(campaignID string, campaign *wc.Campaign) error {
	err := r.inner.Update(campaignID, campaign)
	if err == nil {
		r.invalidate(campaignID)
	}
	return err
}

func (r *CachedRepository) Delete(campaignID string) error {
	err := r.inner.Delete(campaignID)
	if err == nil {
		r.invalidate(campaignID)
	}
	return err
}

func (r *CachedRepository) UpdateStatus(campaignID string, status wc.Status, allowed ...wc.Status) (bool, error) {
	ok, err := r.inner.UpdateStatus(campaignID, status, allowed...)
	if err == nil && ok {
		r.invalidate(campaignID)
	}
	return ok, err
}

func (r *CachedRepository) UpdateResetCode(campaignID string, resetCode string) error {
	err := r.inner.UpdateResetCode(campaignID, resetCode)
	if err == nil {
		r.invalidate(campaignID)
	}
	return err
}

func (r *CachedRepository) UpdateClearCode(campaignID string, clearCode string) error {
	err := r.inner.UpdateClearCode(campaignID, clearCode)
	if err == nil {
		r.invalidate(campaignID)
	}
	return err
}

func (r *CachedRepository) FindLatestOrganicByBusinessPhone(workspaceID string, businessPhoneID string) (*wc.Campaign, error) {
	return r.inner.FindLatestOrganicByBusinessPhone(workspaceID, businessPhoneID)
}

func (r *CachedRepository) List(input wc.ListCampaignsInput) (*shared.PaginatedResult[*wc.Campaign], error) {
	return r.inner.List(input)
}

func (r *CachedRepository) ListByStatus(status wc.Status) ([]*wc.Campaign, error) {
	return r.inner.ListByStatus(status)
}

func (r *CachedRepository) ListScheduledToStart(at time.Time, limit int) ([]*wc.Campaign, error) {
	return r.inner.ListScheduledToStart(at, limit)
}
