package call_recording_repository

import (
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/calls/recordings"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) recordings.CallRecordRepository {
	return &repository{db: db}
}

func (r *repository) Save(record *recordings.CallRecord) error {
	if record == nil {
		return errors.New("call record is required")
	}

	if record.CallID == "" {
		record.CallID = uuid.New().String()
	}

	schemaRecord := toSchema(record)
	return r.db.Create(&schemaRecord).Error
}

func (r *repository) GetByCallID(callID string) (*recordings.CallRecord, error) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil, errors.New("call id is required")
	}

	var schemaRecord schema.CallRecording
	if err := r.db.Where("call_id = ?", callID).First(&schemaRecord).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return toDomain(&schemaRecord), nil
}

func (r *repository) GetByLeadID(leadID string) ([]*recordings.CallRecord, error) {
	leadID = strings.TrimSpace(leadID)
	if leadID == "" {
		return nil, errors.New("lead id is required")
	}

	var schemaRecords []schema.CallRecording
	if err := r.db.Where("lead_id = ?", leadID).Order("created_at DESC").Find(&schemaRecords).Error; err != nil {
		return nil, err
	}

	return toDomainList(schemaRecords), nil
}

func (r *repository) GetByEntryID(entryID string) ([]*recordings.CallRecord, error) {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return nil, errors.New("entry id is required")
	}

	var schemaRecords []schema.CallRecording
	if err := r.db.Where("entry_id = ?", entryID).Order("created_at DESC").Find(&schemaRecords).Error; err != nil {
		return nil, err
	}

	return toDomainList(schemaRecords), nil
}

func (r *repository) Update(record *recordings.CallRecord) error {
	if record == nil {
		return errors.New("call record is required")
	}
	if record.CallID == "" {
		return errors.New("call id is required")
	}

	schemaRecord := toSchema(record)
	return r.db.Where("call_id = ?", record.CallID).Updates(&schemaRecord).Error
}

func (r *repository) List(filters recordings.ListFilters) (*shared.PaginatedResult[*recordings.CallRecord], error) {
	filters.Pagination = shared.NormalizePagination(filters.Pagination)

	query := r.db.Model(&schema.CallRecording{})

	if filters.LeadID != "" {
		query = query.Where("lead_id = ?", filters.LeadID)
	}
	if filters.EntryID != "" {
		query = query.Where("entry_id = ?", filters.EntryID)
	}
	if filters.MinDuration != nil {
		query = query.Where("duration_sec >= ?", *filters.MinDuration)
	}
	if filters.MaxDuration != nil {
		query = query.Where("duration_sec <= ?", *filters.MaxDuration)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	sortField := "created_at"
	if filters.SortBy != "" {
		switch filters.SortBy {
		case "duration":
			sortField = "duration_sec"
		case "call_start":
			sortField = "call_start"
		case "created_at":
			sortField = "created_at"
		}
	}

	sortDirection := "DESC"
	if !filters.SortDesc {
		sortDirection = "ASC"
	}
	orderClause := sortField + " " + sortDirection

	offset := (filters.Pagination.Page - 1) * filters.Pagination.PageSize
	var schemaRecords []schema.CallRecording
	if err := query.Offset(offset).Limit(filters.Pagination.PageSize).Order(orderClause).Find(&schemaRecords).Error; err != nil {
		return nil, err
	}

	items := toDomainList(schemaRecords)
	return shared.NewPaginatedResult(items, filters.Pagination, total), nil
}

func toSchema(record *recordings.CallRecord) *schema.CallRecording {
	var callRecordID *string
	if strings.TrimSpace(record.CallRecordID) != "" {
		id := record.CallRecordID
		callRecordID = &id
	}
	var entryID *string
	if strings.TrimSpace(record.EntryID) != "" {
		id := record.EntryID
		entryID = &id
	}
	var leadID *string
	if strings.TrimSpace(record.LeadID) != "" {
		id := record.LeadID
		leadID = &id
	}
	return &schema.CallRecording{
		CallID:       record.CallID,
		CallRecordID: callRecordID,
		WorkspaceID:  record.WorkspaceID,
		EntryID:      entryID,
		LeadID:       leadID,
		RecordingURL: record.RecordingURL,
		DurationSec:  record.DurationSec,
		FileSize:     record.FileSize,
		CallStart:    record.CallStart,
		CallEnd:      record.CallEnd,
		CreatedAt:    record.CreatedAt,
		UpdatedAt:    record.LastUpdated,
	}
}

func toDomain(schemaRecord *schema.CallRecording) *recordings.CallRecord {
	var callRecordID string
	if schemaRecord.CallRecordID != nil {
		callRecordID = *schemaRecord.CallRecordID
	}
	var entryID string
	if schemaRecord.EntryID != nil {
		entryID = *schemaRecord.EntryID
	}
	var leadID string
	if schemaRecord.LeadID != nil {
		leadID = *schemaRecord.LeadID
	}
	return &recordings.CallRecord{
		CallID:       schemaRecord.CallID,
		CallRecordID: callRecordID,
		WorkspaceID:  schemaRecord.WorkspaceID,
		EntryID:      entryID,
		LeadID:       leadID,
		RecordingURL: schemaRecord.RecordingURL,
		DurationSec:  schemaRecord.DurationSec,
		FileSize:     schemaRecord.FileSize,
		CallStart:    schemaRecord.CallStart,
		CallEnd:      schemaRecord.CallEnd,
		CreatedAt:    schemaRecord.CreatedAt,
		LastUpdated:  schemaRecord.UpdatedAt,
	}
}

func toDomainList(schemaRecords []schema.CallRecording) []*recordings.CallRecord {
	records := make([]*recordings.CallRecord, len(schemaRecords))
	for i, schemaRecord := range schemaRecords {
		records[i] = toDomain(&schemaRecord)
	}
	return records
}
