package shortlink_usecase

import (
	"context"
	"log"
	"time"

	"vozko/domain/shortlink"
)

type purgeClicksUseCase struct {
	clickRepo     shortlink.ClickRepository
	retentionDays int
}

func NewPurgeClicksUseCase(clickRepo shortlink.ClickRepository, retentionDays int) shortlink.PurgeClicksUseCase {
	return &purgeClicksUseCase{clickRepo: clickRepo, retentionDays: retentionDays}
}

func (uc *purgeClicksUseCase) Execute(ctx context.Context) error {
	if uc.retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -uc.retentionDays)

	clicks, err := uc.clickRepo.PurgeClicksBefore(ctx, cutoff)
	if err != nil {
		return err
	}
	stats, err := uc.clickRepo.PurgeDailyStatsBefore(ctx, cutoff)
	if err != nil {
		return err
	}
	log.Printf("[shortlink] retention purge removed %d clicks and %d daily stat rows before %s", clicks, stats, cutoff.Format(time.RFC3339))
	return nil
}
