package advertising_repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

const insightBatchSize = 500

type insightRepository struct {
	db *gorm.DB
}

func NewInsightRepository(db *gorm.DB) advertising.InsightRepository {
	return &insightRepository{db: db}
}

func (r *insightRepository) ReplaceDays(ctx context.Context, accountID string, dr advertising.DateRange, rows []advertising.DailyInsight) error {
	if blank(accountID) {
		return errAccountRequired
	}
	if err := dr.Validate(); err != nil {
		return err
	}
	records := make([]*schema.AdInsightDaily, 0, len(rows))
	for _, row := range rows {
		if (row.AdAccountID != "" && row.AdAccountID != accountID) || !dr.Contains(row.Day) || blank(row.AdMetaID) {
			return fmt.Errorf("%w: ad %s on %s", errForeignInsight, row.AdMetaID, row.Day.Format(advertising.DayLayout))
		}
		record, err := toInsightRecord(accountID, row)
		if err != nil {
			return err
		}
		records = append(records, record)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("ad_account_id = ? AND day >= ? AND day <= ?", accountID, dr.Since, dr.Until).
			Delete(&schema.AdInsightDaily{}).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		return tx.CreateInBatches(records, insightBatchSize).Error
	})
}

func (r *insightRepository) Rows(ctx context.Context, accountID string, dr advertising.DateRange) ([]advertising.DailyInsight, error) {
	if blank(accountID) {
		return nil, errAccountRequired
	}
	var records []schema.AdInsightDaily
	if err := r.db.WithContext(ctx).
		Where("ad_account_id = ? AND day >= ? AND day <= ?", accountID, dr.Since, dr.Until).
		Order("day, ad_meta_id").
		Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]advertising.DailyInsight, 0, len(records))
	for i := range records {
		row, err := toInsight(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func (r *insightRepository) AdDay(ctx context.Context, adMetaID string, day time.Time) (*advertising.DailyInsight, error) {
	var record schema.AdInsightDaily
	if err := r.db.WithContext(ctx).First(&record, "ad_meta_id = ? AND day = ?", adMetaID, day).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrObjectNotFound
		}
		return nil, err
	}
	row, err := toInsight(&record)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func toInsightRecord(accountID string, row advertising.DailyInsight) (*schema.AdInsightDaily, error) {
	actions, err := encodeJSON(row.Actions)
	if err != nil {
		return nil, err
	}
	return &schema.AdInsightDaily{
		AdMetaID:       row.AdMetaID,
		Day:            advertising.CivilDay(row.Day, time.UTC),
		AdAccountID:    accountID,
		CampaignMetaID: row.CampaignMetaID,
		AdSetMetaID:    row.AdSetMetaID,
		Currency:       row.Currency,
		SpendMicros:    row.SpendMicros,
		Impressions:    row.Impressions,
		Clicks:         row.Clicks,
		LinkClicks:     row.LinkClicks,
		Actions:        actions,
		FetchedAt:      row.FetchedAt,
	}, nil
}

func toInsight(r *schema.AdInsightDaily) (advertising.DailyInsight, error) {
	row := advertising.DailyInsight{
		AdAccountID:    r.AdAccountID,
		CampaignMetaID: r.CampaignMetaID,
		AdSetMetaID:    r.AdSetMetaID,
		AdMetaID:       r.AdMetaID,
		Day:            advertising.CivilDay(r.Day, time.UTC),
		Currency:       r.Currency,
		SpendMicros:    r.SpendMicros,
		Impressions:    r.Impressions,
		Clicks:         r.Clicks,
		LinkClicks:     r.LinkClicks,
		FetchedAt:      r.FetchedAt,
	}
	if err := decodeJSON(r.Actions, &row.Actions); err != nil {
		return advertising.DailyInsight{}, err
	}
	return row, nil
}
