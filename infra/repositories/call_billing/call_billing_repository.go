package call_billing_repository

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/calls/billing"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) billing.Repository {
	return &repository{db: db}
}

func (r *repository) Save(record *billing.CallBillingRecord) error {
	if record == nil {
		return errors.New("billing record is required")
	}
	if record.ID == "" {
		record.ID = uuid.New().String()
	}
	s := toSchema(record)
	return r.db.Create(&s).Error
}

func (r *repository) GetByCallID(callID string) (*billing.CallBillingRecord, error) {
	var s schema.CallBillingRecord
	if err := r.db.Where("call_id = ?", callID).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, billing.ErrBillingRecordNotFound
		}
		return nil, err
	}
	return toDomain(&s), nil
}

func (r *repository) GetByCallIDs(callIDs []string) (map[string]*billing.CallBillingRecord, error) {
	out := make(map[string]*billing.CallBillingRecord, len(callIDs))
	if len(callIDs) == 0 {
		return out, nil
	}
	var records []schema.CallBillingRecord
	if err := r.db.Where("call_id IN ?", callIDs).Find(&records).Error; err != nil {
		return nil, err
	}
	for i := range records {
		out[records[i].CallID] = toDomain(&records[i])
	}
	return out, nil
}

func (r *repository) Update(record *billing.CallBillingRecord) error {
	s := toSchema(record)
	return r.db.Save(&s).Error
}

func (r *repository) UpdateStatus(callID string, status billing.BillingStatus) error {
	return r.db.Model(&schema.CallBillingRecord{}).
		Where("call_id = ?", callID).
		Update("status", string(status)).Error
}

func (r *repository) FindOrphaned(olderThan time.Duration) ([]*billing.CallBillingRecord, error) {
	cutoff := time.Now().Add(-olderThan)
	var records []schema.CallBillingRecord
	if err := r.db.
		Where("status = ? AND created_at < ?", string(billing.StatusInProgress), cutoff).
		Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]*billing.CallBillingRecord, len(records))
	for i := range records {
		result[i] = toDomain(&records[i])
	}
	return result, nil
}

func (r *repository) ListByWorkspaceID(input billing.ListBillingInput) (*shared.PaginatedResult[*billing.CallBillingRecord], error) {
	pagination := shared.NormalizePagination(input.Pagination)

	query := r.db.Model(&schema.CallBillingRecord{}).Where("workspace_id = ?", input.WorkspaceID)
	if input.Status != nil {
		query = query.Where("status = ?", string(*input.Status))
	}
	if input.CallSource != nil {
		query = query.Where("call_source = ?", string(*input.CallSource))
	}
	if input.StartDate != nil {
		query = query.Where("call_start >= ?", *input.StartDate)
	}
	if input.EndDate != nil {
		query = query.Where("call_start <= ?", *input.EndDate)
	}

	var totalItems int64
	if err := query.Count(&totalItems).Error; err != nil {
		return nil, err
	}

	var records []schema.CallBillingRecord
	if err := query.
		Order("call_start DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*billing.CallBillingRecord, len(records))
	for i := range records {
		items[i] = toDomain(&records[i])
	}

	return shared.NewPaginatedResult(items, pagination, totalItems), nil
}

func toSchema(d *billing.CallBillingRecord) *schema.CallBillingRecord {
	return &schema.CallBillingRecord{
		ID:          d.ID,
		CallID:      d.CallID,
		WorkspaceID: d.WorkspaceID,
		AgentID:     d.AgentID,
		LeadID:      d.LeadID,
		CallSource:  string(d.CallSource),
		Status:      string(d.Status),
		CallStart:   d.CallStart,
		CallEnd:     d.CallEnd,
		DurationSec: d.DurationSec,

		TelephonyRevenueMicros: d.TelephonyRevenueMicros,
		TotalRevenueMicros:     d.TotalRevenueMicros,

		TelephonyCostMicros: d.TelephonyCostMicros,
		TotalCostMicros:     d.TotalCostMicros,

		TelephonyProfitMicros: d.TelephonyProfitMicros,
		TotalProfitMicros:     d.TotalProfitMicros,
		TransactionID:         d.TransactionID,
		RecordingURL:          d.RecordingURL,
		CallRecordID:          d.CallRecordID,
	}
}

func toDomain(s *schema.CallBillingRecord) *billing.CallBillingRecord {
	return &billing.CallBillingRecord{
		ID:          s.ID,
		CallID:      s.CallID,
		WorkspaceID: s.WorkspaceID,
		AgentID:     s.AgentID,
		LeadID:      s.LeadID,
		CallSource:  billing.CallSource(s.CallSource),
		Status:      billing.BillingStatus(s.Status),
		CallStart:   s.CallStart,
		CallEnd:     s.CallEnd,
		DurationSec: s.DurationSec,

		TelephonyRevenueMicros: s.TelephonyRevenueMicros,
		TotalRevenueMicros:     s.TotalRevenueMicros,

		TelephonyCostMicros: s.TelephonyCostMicros,
		TotalCostMicros:     s.TotalCostMicros,

		TelephonyProfitMicros: s.TelephonyProfitMicros,
		TotalProfitMicros:     s.TotalProfitMicros,
		TransactionID:         s.TransactionID,
		RecordingURL:          s.RecordingURL,
		CallRecordID:          s.CallRecordID,
		CreatedAt:             s.CreatedAt,
		UpdatedAt:             s.UpdatedAt,
	}
}
