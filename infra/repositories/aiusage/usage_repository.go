package aiusage_repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/aiusage"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type UsageRepository struct {
	db *gorm.DB
}

func NewUsageRepository(db *gorm.DB) *UsageRepository {
	return &UsageRepository{db: db}
}

func (r *UsageRepository) Record(record aiusage.Record) error {
	row := schema.AIUsageRecord{
		ReferenceID:      record.ReferenceID,
		WorkspaceID:      record.WorkspaceID,
		Model:            record.Model,
		InputTokens:      record.Tokens.Input,
		OutputTokens:     record.Tokens.Output,
		CacheReadTokens:  record.Tokens.CacheRead,
		CacheWriteTokens: record.Tokens.CacheWrite,
		ReasoningTokens:  record.Tokens.Reasoning,
		Billed:           record.Billed,
	}
	return r.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "reference_id"}}, DoNothing: true}).Create(&row).Error
}

const usageTotalsSQL = `
SELECT
	COUNT(*) FILTER (WHERE input_tokens + output_tokens > 0) AS calls,
	COUNT(*) FILTER (WHERE billed) AS billed,
	COALESCE(SUM(input_tokens), 0) AS input,
	COALESCE(SUM(output_tokens), 0) AS output,
	COALESCE(SUM(cache_read_tokens), 0) AS cache_read,
	COALESCE(SUM(cache_write_tokens), 0) AS cache_write,
	COALESCE(SUM(reasoning_tokens), 0) AS reasoning
FROM ai_usage_records
WHERE workspace_id = ? AND reference_id LIKE ? ESCAPE '\'`

type usageTotalsRow struct {
	Calls      int
	Billed     int
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Reasoning  int64
}

func (r *UsageRepository) TotalsUnder(workspaceID, referencePrefix string) (aiusage.Totals, error) {
	var row usageTotalsRow
	if err := r.db.Raw(usageTotalsSQL, workspaceID, database.LikePrefix(referencePrefix)).Scan(&row).Error; err != nil {
		return aiusage.Totals{}, err
	}
	return aiusage.Totals{
		Calls:  row.Calls,
		Billed: row.Billed,
		Tokens: aiusage.Tokens{Input: row.Input, Output: row.Output, CacheRead: row.CacheRead, CacheWrite: row.CacheWrite, Reasoning: row.Reasoning},
	}, nil
}

var (
	_ aiusage.Recorder = (*UsageRepository)(nil)
	_ aiusage.Totaler  = (*UsageRepository)(nil)
)
