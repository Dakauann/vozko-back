package googlesheet_repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/googlesheet"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

type rowRepository struct {
	db *gorm.DB
}

func NewRowRepository(db *gorm.DB) googlesheet.RowRepository {
	return &rowRepository{db: db}
}

func (r *rowRepository) Insert(ctx context.Context, row *googlesheet.Row) (*googlesheet.Row, bool, error) {
	record, err := toRowSchema(row)
	if err != nil {
		return nil, false, err
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "sheet_id"}, {Name: "idempotency_key"}},
		DoNothing: true,
	}).Create(record)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		row.ID = record.ID
		return row, false, nil
	}
	var existing schema.GoogleSheetRow
	if err := r.db.WithContext(ctx).Where("sheet_id = ? AND idempotency_key = ?", row.SheetID, row.IdempotencyKey).Take(&existing).Error; err != nil {
		return nil, false, err
	}
	stored, err := toRowDomain(&existing)
	return stored, true, err
}

func (r *rowRepository) PendingSheets(ctx context.Context, readyAt time.Time, limit int) ([]googlesheet.PendingSheet, error) {
	var heads []struct {
		SheetID       string
		CreatedAt     time.Time
		NextAttemptAt time.Time
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT sheet_id, created_at, next_attempt_at FROM (
			SELECT DISTINCT ON (r.sheet_id) r.sheet_id, r.created_at, r.next_attempt_at
			FROM google_sheet_rows r
			JOIN google_sheets s ON s.id = r.sheet_id AND s.deleted_at IS NULL AND s.status = 'active'
			WHERE r.status = 'pending'
			ORDER BY r.sheet_id, r.created_at, r.id
		) heads
		WHERE next_attempt_at <= ?
		ORDER BY created_at
		LIMIT ?`, readyAt, limit).Scan(&heads).Error
	if err != nil {
		return nil, err
	}
	out := make([]googlesheet.PendingSheet, len(heads))
	for i, h := range heads {
		out[i] = googlesheet.PendingSheet{SheetID: h.SheetID, OldestPendingAt: h.CreatedAt}
	}
	return out, nil
}

func (r *rowRepository) Claim(ctx context.Context, sheetID, batchID string, limit int, now time.Time) ([]*googlesheet.Row, error) {
	var claimed []schema.GoogleSheetRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var head []schema.GoogleSheetRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("sheet_id = ? AND status = ?", sheetID, string(googlesheet.RowPending)).
			Order("created_at").Order("id").Limit(limit).Find(&head).Error; err != nil {
			return err
		}
		if len(head) == 0 || head[0].NextAttemptAt.After(now) {
			return nil
		}
		ids := make([]string, len(head))
		for i := range head {
			ids[i] = head[i].ID
			head[i].Status = string(googlesheet.RowInFlight)
			head[i].BatchID = &batchID
			head[i].ClaimedAt = &now
			head[i].Attempts++
		}
		if err := tx.Model(&schema.GoogleSheetRow{}).Where("id IN ?", ids).Updates(map[string]any{
			"status":     string(googlesheet.RowInFlight),
			"batch_id":   batchID,
			"claimed_at": now,
			"attempts":   gorm.Expr("attempts + 1"),
		}).Error; err != nil {
			return err
		}
		claimed = head
		return nil
	})
	if err != nil {
		return nil, err
	}
	rows, err := toRowDomains(claimed)
	return inCreationOrder(rows), err
}

func (r *rowRepository) MarkWritten(ctx context.Context, rows []googlesheet.WrittenRow, at time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	values := make([]string, len(rows))
	args := make([]any, 0, len(rows)*2+1)
	args = append(args, at)
	for i, row := range rows {
		values[i] = "(?::uuid, ?::int)"
		args = append(args, row.ID, row.RowNumber)
	}
	return r.db.WithContext(ctx).Exec(`
		UPDATE google_sheet_rows AS r
		SET status = 'written', written_at = ?, row_number = v.n, error_code = '', error_detail = '', batch_id = NULL, claimed_at = NULL
		FROM (VALUES `+strings.Join(values, ", ")+`) AS v(id, n)
		WHERE r.id = v.id`, args...).Error
}

func (r *rowRepository) Release(ctx context.Context, ids []string, nextAttemptAt time.Time, code string) error {
	return r.setStatus(ctx, ids, map[string]any{
		"status":          string(googlesheet.RowPending),
		"next_attempt_at": nextAttemptAt,
		"batch_id":        nil,
		"claimed_at":      nil,
		"error_code":      code,
	})
}

func (r *rowRepository) Unclaim(ctx context.Context, ids []string) error {
	return r.setStatus(ctx, ids, map[string]any{
		"status":     string(googlesheet.RowPending),
		"batch_id":   nil,
		"claimed_at": nil,
		"attempts":   gorm.Expr("GREATEST(attempts - 1, 0)"),
	})
}

func (r *rowRepository) Hold(ctx context.Context, ids []string, code, detail string) error {
	return r.setStatus(ctx, ids, map[string]any{
		"status":       string(googlesheet.RowHeld),
		"batch_id":     nil,
		"claimed_at":   nil,
		"error_code":   code,
		"error_detail": clip(detail, 500),
	})
}

func (r *rowRepository) Fail(ctx context.Context, id, code, detail string) error {
	return r.setStatus(ctx, []string{id}, map[string]any{
		"status":       string(googlesheet.RowFailed),
		"batch_id":     nil,
		"claimed_at":   nil,
		"error_code":   code,
		"error_detail": clip(detail, 500),
	})
}

func (r *rowRepository) setStatus(ctx context.Context, ids []string, updates map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&schema.GoogleSheetRow{}).Where("id IN ?", ids).Updates(updates).Error
}

func (r *rowRepository) RequeueProblems(ctx context.Context, sheetID string, now time.Time) (int, error) {
	result := r.db.WithContext(ctx).Model(&schema.GoogleSheetRow{}).
		Where("sheet_id = ? AND status IN ?", sheetID, []string{string(googlesheet.RowHeld), string(googlesheet.RowFailed)}).
		Updates(map[string]any{
			"status":          string(googlesheet.RowPending),
			"attempts":        0,
			"next_attempt_at": now,
			"error_code":      "",
			"error_detail":    "",
		})
	return int(result.RowsAffected), result.Error
}

func (r *rowRepository) StuckInFlight(ctx context.Context, sheetID string, claimedBefore time.Time) ([]*googlesheet.Row, error) {
	var records []schema.GoogleSheetRow
	err := r.db.WithContext(ctx).
		Where("sheet_id = ? AND status = ? AND claimed_at < ?", sheetID, string(googlesheet.RowInFlight), claimedBefore).
		Order("created_at").Order("id").Find(&records).Error
	if err != nil {
		return nil, err
	}
	return toRowDomains(records)
}

func (r *rowRepository) PurgeWritten(ctx context.Context, writtenBefore time.Time, limit int) (int, error) {
	result := r.db.WithContext(ctx).Exec(`
		DELETE FROM google_sheet_rows WHERE id IN (
			SELECT id FROM google_sheet_rows WHERE status = 'written' AND written_at < ? ORDER BY written_at LIMIT ?
		)`, writtenBefore, limit)
	return int(result.RowsAffected), result.Error
}

func (r *rowRepository) DeleteForLead(ctx context.Context, workspaceID, leadID string) (int, error) {
	result := r.db.WithContext(ctx).Where("workspace_id = ? AND lead_id = ?", workspaceID, leadID).Delete(&schema.GoogleSheetRow{})
	return int(result.RowsAffected), result.Error
}

func (r *rowRepository) DeleteUnwritten(ctx context.Context, sheetID string) error {
	return r.db.WithContext(ctx).Where("sheet_id = ? AND status <> ?", sheetID, string(googlesheet.RowWritten)).
		Delete(&schema.GoogleSheetRow{}).Error
}

func (r *rowRepository) Health(ctx context.Context, sheetID string, problems int) (googlesheet.Health, error) {
	var counts []struct {
		Status string
		N      int
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT status, count(*) AS n FROM google_sheet_rows
		WHERE sheet_id = ? AND status IN ('pending', 'in_flight', 'held', 'failed')
		GROUP BY status`, sheetID).Scan(&counts).Error
	if err != nil {
		return googlesheet.Health{}, err
	}
	health := googlesheet.Health{}
	for _, c := range counts {
		switch googlesheet.RowStatus(c.Status) {
		case googlesheet.RowPending:
			health.Pending = c.N
		case googlesheet.RowInFlight:
			health.InFlight = c.N
		case googlesheet.RowHeld:
			health.Held = c.N
		case googlesheet.RowFailed:
			health.Failed = c.N
		}
	}
	if err := r.db.WithContext(ctx).Raw(`
		SELECT count(*) FROM (SELECT 1 FROM google_sheet_rows WHERE sheet_id = ? AND status = 'written' LIMIT ?) capped`,
		sheetID, googlesheet.HealthCountCap).Scan(&health.Written).Error; err != nil {
		return googlesheet.Health{}, err
	}
	var oldest sql.NullTime
	if err := r.db.WithContext(ctx).Raw(`
		SELECT min(created_at) FROM google_sheet_rows WHERE sheet_id = ? AND status = 'pending'`, sheetID).Row().Scan(&oldest); err != nil {
		return googlesheet.Health{}, err
	}
	if oldest.Valid {
		at := oldest.Time
		health.OldestPendingAt = &at
	}
	var records []schema.GoogleSheetRow
	if err := r.db.WithContext(ctx).
		Where("sheet_id = ? AND status IN ?", sheetID, []string{string(googlesheet.RowHeld), string(googlesheet.RowFailed)}).
		Order("created_at DESC").Limit(problems).Find(&records).Error; err != nil {
		return googlesheet.Health{}, err
	}
	rows, err := toRowDomains(records)
	if err != nil {
		return googlesheet.Health{}, err
	}
	health.Problems = rows
	return health, nil
}

func toRowSchema(row *googlesheet.Row) (*schema.GoogleSheetRow, error) {
	cells, err := json.Marshal(row.Cells)
	if err != nil {
		return nil, fmt.Errorf("googlesheet: encode cells: %w", err)
	}
	record := &schema.GoogleSheetRow{
		ID:             row.ID,
		WorkspaceID:    row.WorkspaceID,
		SheetID:        row.SheetID,
		IdempotencyKey: row.IdempotencyKey,
		Cells:          piigorm.NewEncrypted(string(cells)),
		SheetVersion:   row.SheetVersion,
		OriginKind:     string(row.Origin.Kind),
		AgentID:        row.Origin.AgentID,
		WorkflowID:     row.Origin.WorkflowID,
		RunID:          row.Origin.RunID,
		ActorID:        row.Origin.ActorID,
		EntryID:        row.Origin.EntryID,
		EntryType:      row.Origin.EntryType,
		Status:         string(row.Status),
		Attempts:       row.Attempts,
		NextAttemptAt:  row.NextAttemptAt,
		CreatedAt:      row.CreatedAt,
	}
	if row.Origin.LeadID != "" {
		record.LeadID = &row.Origin.LeadID
	}
	return record, nil
}

func toRowDomain(record *schema.GoogleSheetRow) (*googlesheet.Row, error) {
	cells := googlesheet.Cells{}
	if record.Cells.Valid && record.Cells.Plain != "" {
		if err := json.Unmarshal([]byte(record.Cells.Plain), &cells); err != nil {
			return nil, fmt.Errorf("googlesheet: decode cells of %s: %w", record.ID, err)
		}
	}
	row := &googlesheet.Row{
		ID:             record.ID,
		WorkspaceID:    record.WorkspaceID,
		SheetID:        record.SheetID,
		IdempotencyKey: record.IdempotencyKey,
		Cells:          cells,
		SheetVersion:   record.SheetVersion,
		Origin: googlesheet.Origin{
			Kind:       googlesheet.OriginKind(record.OriginKind),
			AgentID:    record.AgentID,
			WorkflowID: record.WorkflowID,
			RunID:      record.RunID,
			ActorID:    record.ActorID,
			EntryID:    record.EntryID,
			EntryType:  record.EntryType,
		},
		Status:        googlesheet.RowStatus(record.Status),
		Attempts:      record.Attempts,
		NextAttemptAt: record.NextAttemptAt,
		ErrorCode:     record.ErrorCode,
		ErrorDetail:   record.ErrorDetail,
		CreatedAt:     record.CreatedAt,
		WrittenAt:     record.WrittenAt,
	}
	if record.LeadID != nil {
		row.Origin.LeadID = *record.LeadID
	}
	if record.BatchID != nil {
		row.BatchID = *record.BatchID
	}
	if record.RowNumber != nil {
		row.RowNumber = *record.RowNumber
	}
	return row, nil
}

func toRowDomains(records []schema.GoogleSheetRow) ([]*googlesheet.Row, error) {
	out := make([]*googlesheet.Row, 0, len(records))
	for i := range records {
		row, err := toRowDomain(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func inCreationOrder(rows []*googlesheet.Row) []*googlesheet.Row {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
	return rows
}
