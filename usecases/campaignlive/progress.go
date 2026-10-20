package campaignlive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/cache"
	"vozko/domain/campaign"
)

const progressBaselineTTL = 10 * time.Minute

var (
	ErrUnavailable     = errors.New("campaign live: its lookups are not wired")
	ErrCampaignMissing = errors.New("campaign live: no campaign to read")
)

type Counter interface {
	CountByStatus(campaignID string) (*campaign.Counts, error)
}

type Samples interface {
	GetString(key string) (string, error)
	SetString(key string, value string, ttl time.Duration) error
}

type ProgressDeps struct {
	Namespace string
	Counter   Counter
	Memo      cache.Memo
	Samples   Samples
	Clock     func() time.Time
}

type progressUseCase struct {
	deps ProgressDeps
}

func NewProgress(deps ProgressDeps) (campaign.ProgressUseCase, error) {
	if deps.Namespace == "" || deps.Counter == nil || deps.Memo == nil || deps.Samples == nil {
		return nil, ErrUnavailable
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &progressUseCase{deps: deps}, nil
}

func (uc *progressUseCase) Execute(ctx context.Context, live campaign.LiveCampaign) (*campaign.Progress, error) {
	if live.ID == "" {
		return nil, ErrCampaignMissing
	}
	key := fmt.Sprintf("%s:progress:%s:%s", uc.deps.Namespace, live.ID, live.Status)
	progress, err := cache.Remember(ctx, uc.deps.Memo, key, campaign.ProgressFreshness(live.Status), func(context.Context) (campaign.Progress, error) {
		return uc.measure(live)
	})
	if err != nil {
		return nil, err
	}
	return &progress, nil
}

func (uc *progressUseCase) measure(live campaign.LiveCampaign) (campaign.Progress, error) {
	counts, err := uc.deps.Counter.CountByStatus(live.ID)
	if err != nil {
		return campaign.Progress{}, err
	}
	at := uc.deps.Clock().UTC()
	baseline := uc.baseline(live.ID)
	progress := campaign.NewProgress(live.Status, counts, at, baseline)
	uc.keepBaseline(live.ID, campaign.NextProgressBaseline(baseline, campaign.ProgressSample{Processed: progress.Metrics.Processed, At: at}))
	return progress, nil
}

func (uc *progressUseCase) baselineKey(campaignID string) string {
	return uc.deps.Namespace + ":progress_baseline:" + campaignID
}

func (uc *progressUseCase) baseline(campaignID string) *campaign.ProgressSample {
	raw, err := uc.deps.Samples.GetString(uc.baselineKey(campaignID))
	if err != nil || raw == "" {
		return nil
	}
	var sample campaign.ProgressSample
	if json.Unmarshal([]byte(raw), &sample) != nil {
		return nil
	}
	return &sample
}

func (uc *progressUseCase) keepBaseline(campaignID string, sample campaign.ProgressSample) {
	raw, err := json.Marshal(sample)
	if err != nil {
		return
	}
	if err := uc.deps.Samples.SetString(uc.baselineKey(campaignID), string(raw), progressBaselineTTL); err != nil {
		log.Printf("[campaign live] could not keep the pace baseline of %s: %v", campaignID, err)
	}
}
