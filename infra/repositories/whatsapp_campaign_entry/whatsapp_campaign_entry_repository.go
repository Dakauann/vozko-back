package whatsapp_campaign_entry

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/lead"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) wce.Repository {
	return &repository{db: db}
}

func (r *repository) Create(entry *wce.WhatsAppCampaignEntry) error {
	if entry == nil {
		return wce.ErrEntryLeadRequired
	}
	entry.Normalize()
	if err := entry.Validate(); err != nil {
		return err
	}
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	schemaEntry := toSchema(entry)
	if err := r.db.Create(&schemaEntry).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return wce.ErrEntryDuplicate
		}
		return err
	}
	return nil
}

func (r *repository) CreateMany(entries []wce.WhatsAppCampaignEntry) ([]wce.WhatsAppCampaignEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	schemaEntries := make([]schema.WhatsAppCampaignEntry, len(entries))
	for i, entry := range entries {
		entry.Normalize()
		if err := entry.Validate(); err != nil {
			return nil, err
		}
		if entry.ID == "" {
			entry.ID = uuid.New().String()
		}
		entries[i] = entry
		schemaEntries[i] = *toSchema(&entry)
	}

	const batchSize = 500
	if err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "campaign_id"}, {Name: "lead_id"}},
		DoNothing: true,
	}).CreateInBatches(&schemaEntries, batchSize).Error; err != nil {
		return nil, err
	}

	return entries, nil
}

func (r *repository) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, wce.ErrEntryNotFound
	}

	var schemaEntry schema.WhatsAppCampaignEntry
	if err := r.db.Where("id = ?", id).First(&schemaEntry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wce.ErrEntryNotFound
		}
		return nil, err
	}

	return toDomain(&schemaEntry), nil
}

func (r *repository) FindByMessageID(messageID string) (*wce.WhatsAppCampaignEntry, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil, wce.ErrEntryNotFound
	}

	var schemaEntry schema.WhatsAppCampaignEntry
	if err := r.db.Where("message_id = ?", messageID).First(&schemaEntry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wce.ErrEntryNotFound
		}
		return nil, err
	}

	return toDomain(&schemaEntry), nil
}

func (r *repository) FindByIDs(ids []string) ([]*wce.WhatsAppCampaignEntry, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var schemaEntries []schema.WhatsAppCampaignEntry
	if err := r.db.Where("id IN ?", ids).Find(&schemaEntries).Error; err != nil {
		return nil, err
	}
	result := make([]*wce.WhatsAppCampaignEntry, len(schemaEntries))
	for i := range schemaEntries {
		result[i] = toDomain(&schemaEntries[i])
	}
	return result, nil
}

func (r *repository) FindByCampaignAndLead(campaignID, leadID string) (*wce.WhatsAppCampaignEntry, error) {
	campaignID = strings.TrimSpace(campaignID)
	leadID = strings.TrimSpace(leadID)
	if campaignID == "" || leadID == "" {
		return nil, wce.ErrEntryNotFound
	}

	var schemaEntry schema.WhatsAppCampaignEntry
	if err := r.db.Where("campaign_id = ? AND lead_id = ?", campaignID, leadID).First(&schemaEntry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wce.ErrEntryNotFound
		}
		return nil, err
	}

	return toDomain(&schemaEntry), nil
}

func (r *repository) FindByCampaignAndNumber(campaignID, number string) (*wce.WhatsAppCampaignEntry, error) {
	campaignID = strings.TrimSpace(campaignID)
	normalized := lead.NormalizeNumber(number)
	if campaignID == "" || normalized == "" {
		return nil, wce.ErrEntryNotFound
	}

	var schemaEntry schema.WhatsAppCampaignEntry
	if err := r.db.
		Joins("JOIN leads ON leads.id = whatsapp_campaign_entries.lead_id").
		Where("whatsapp_campaign_entries.campaign_id = ? AND leads.number = ?", campaignID, normalized).
		First(&schemaEntry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wce.ErrEntryNotFound
		}
		return nil, err
	}

	return toDomain(&schemaEntry), nil
}

func (r *repository) Delete(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return wce.ErrEntryNotFound
	}
	return r.db.Where("id = ?", id).Delete(&schema.WhatsAppCampaignEntry{}).Error
}

func (r *repository) DeleteByCampaignID(campaignID string) error {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return wce.ErrEntryCampaignRequired
	}
	return r.db.Where("campaign_id = ?", campaignID).Delete(&schema.WhatsAppCampaignEntry{}).Error
}

func (r *repository) List(input wce.ListEntriesInput) (*shared.PaginatedResult[*wce.WhatsAppCampaignEntry], error) {
	input.Options.Pagination = shared.NormalizePagination(input.Options.Pagination)

	query := r.db.Model(&schema.WhatsAppCampaignEntry{})

	if input.CampaignID != "" {
		query = query.Where("campaign_id = ?", input.CampaignID)
	}
	if input.LeadID != "" {
		query = query.Where("lead_id = ?", input.LeadID)
	}
	if input.Status.Valid() {
		query = query.Where("status = ?", string(input.Status))
	}
	if input.Number != "" {
		normalized := lead.NormalizeNumber(input.Number)
		if normalized != "" {
			query = query.Joins("JOIN leads ON leads.id = whatsapp_campaign_entries.lead_id").
				Where("leads.number = ?", normalized)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	offset := (input.Options.Pagination.Page - 1) * input.Options.Pagination.PageSize
	var schemaEntries []schema.WhatsAppCampaignEntry
	if err := query.Offset(offset).Limit(input.Options.Pagination.PageSize).Order("created_at DESC").Find(&schemaEntries).Error; err != nil {
		return nil, err
	}

	items := make([]*wce.WhatsAppCampaignEntry, len(schemaEntries))
	for i, e := range schemaEntries {
		items[i] = toDomain(&e)
	}

	totalPages := int(total) / input.Options.Pagination.PageSize
	if int(total)%input.Options.Pagination.PageSize > 0 {
		totalPages++
	}

	return &shared.PaginatedResult[*wce.WhatsAppCampaignEntry]{
		Items:      items,
		Page:       input.Options.Pagination.Page,
		PageSize:   input.Options.Pagination.PageSize,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

func (r *repository) ListByCampaignID(campaignID string) ([]wce.WhatsAppCampaignEntry, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, wce.ErrEntryCampaignRequired
	}

	var schemaEntries []schema.WhatsAppCampaignEntry
	if err := r.db.Where("campaign_id = ?", campaignID).Order("created_at DESC").Find(&schemaEntries).Error; err != nil {
		return nil, err
	}

	entries := make([]wce.WhatsAppCampaignEntry, len(schemaEntries))
	for i, e := range schemaEntries {
		entries[i] = *toDomain(&e)
	}

	return entries, nil
}

func (r *repository) ListByLeadID(leadID string) ([]wce.WhatsAppCampaignEntry, error) {
	leadID = strings.TrimSpace(leadID)
	if leadID == "" {
		return nil, wce.ErrEntryLeadRequired
	}

	var schemaEntries []schema.WhatsAppCampaignEntry
	if err := r.db.Where("lead_id = ?", leadID).Order("created_at DESC").Find(&schemaEntries).Error; err != nil {
		return nil, err
	}

	entries := make([]wce.WhatsAppCampaignEntry, len(schemaEntries))
	for i, e := range schemaEntries {
		entries[i] = *toDomain(&e)
	}

	return entries, nil
}

func (r *repository) ListRecentlyUpdated(campaignID string, limit int) ([]wce.WhatsAppCampaignEntry, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, wce.ErrEntryCampaignRequired
	}

	var schemaEntries []schema.WhatsAppCampaignEntry
	if err := r.db.Where("campaign_id = ?", campaignID).Order("updated_at DESC").Limit(limit).Find(&schemaEntries).Error; err != nil {
		return nil, err
	}

	entries := make([]wce.WhatsAppCampaignEntry, len(schemaEntries))
	for i, e := range schemaEntries {
		entries[i] = *toDomain(&e)
	}

	return entries, nil
}

func (r *repository) CountByCampaignID(campaignID string) (int64, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return 0, wce.ErrEntryCampaignRequired
	}

	var count int64
	if err := r.db.Model(&schema.WhatsAppCampaignEntry{}).Where("campaign_id = ?", campaignID).Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}

func (r *repository) CountByStatus(campaignID string) (*wce.StatusCounts, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, wce.ErrEntryCampaignRequired
	}

	var results []struct {
		Status string
		Count  int64
	}

	if err := r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Select("status, COUNT(*) as count").
		Where("campaign_id = ?", campaignID).
		Group("status").
		Scan(&results).Error; err != nil {
		return nil, err
	}

	counts := &wce.StatusCounts{}
	for _, r := range results {
		addWAStatusCount(counts, r.Status, r.Count)
	}

	return counts, nil
}

// CountByStatusForCampaigns aggregates per-status entry counts for many
// campaigns in a single GROUP BY query (keyed by campaign ID), so list
// endpoints avoid an N+1 of CountByStatus. Soft-deleted rows are excluded by
// GORM automatically; the partial index idx_wce_campaign_status_del
// (campaign_id, status) WHERE deleted_at IS NULL serves this as an index-only
// scan. Campaigns with no entries are absent from the map.
func (r *repository) CountByStatusForCampaigns(campaignIDs []string) (map[string]*wce.StatusCounts, error) {
	ids := dedupeNonEmpty(campaignIDs)
	out := make(map[string]*wce.StatusCounts, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	var results []struct {
		CampaignID string
		Status     string
		Count      int64
	}

	if err := r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Select("campaign_id, status, COUNT(*) as count").
		Where("campaign_id IN ?", ids).
		Group("campaign_id, status").
		Scan(&results).Error; err != nil {
		return nil, err
	}

	for _, row := range results {
		counts := out[row.CampaignID]
		if counts == nil {
			counts = &wce.StatusCounts{}
			out[row.CampaignID] = counts
		}
		addWAStatusCount(counts, row.Status, row.Count)
	}

	return out, nil
}

// CountByStatusForWorkspace rolls entry status counts up to the workspace level
// for every campaign matching the filter, in one JOIN+GROUP BY query. Campaigns
// are filtered by creation date (inclusive, nil = unbounded), type and
// department; soft-deleted campaigns and entries are excluded. The per-campaign
// idx_wce_campaign_status_del partial index serves the entry aggregation.
func (r *repository) CountByStatusForWorkspace(f wce.WorkspaceSummaryFilter) (*wce.StatusCounts, error) {
	counts := &wce.StatusCounts{}
	if strings.TrimSpace(f.WorkspaceID) == "" {
		return counts, nil
	}

	q := r.db.Table("whatsapp_campaign_entries AS e").
		Joins("JOIN whatsapp_campaigns c ON c.id = e.campaign_id").
		Where("c.workspace_id = ?", f.WorkspaceID).
		Where("c.deleted_at IS NULL").
		Where("e.deleted_at IS NULL")

	if t := strings.TrimSpace(f.Type); t != "" {
		q = q.Where("c.type = ?", t)
	}
	if f.CreatedFrom != nil {
		q = q.Where("c.created_at >= ?", *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		q = q.Where("c.created_at <= ?", *f.CreatedTo)
	}
	if len(f.DepartmentIDs) > 0 {
		q = q.Where("c.department_id IN ?", f.DepartmentIDs)
	}

	var results []struct {
		Status string
		Count  int64
	}
	if err := q.Select("e.status AS status, COUNT(*) AS count").
		Group("e.status").
		Scan(&results).Error; err != nil {
		return nil, err
	}

	for _, row := range results {
		addWAStatusCount(counts, row.Status, row.Count)
	}
	return counts, nil
}

// CountDispatchesByCategoryForWorkspace counts billed sends (not PENDING,
// FAILED, or spam-skip) grouped by the campaign's WhatsApp template category.
// Same campaign filter as CountByStatusForWorkspace. Categories come from the
// live whatsapp_templates row (not stored on the campaign).
func (r *repository) CountDispatchesByCategoryForWorkspace(f wce.WorkspaceSummaryFilter) (map[string]int64, error) {
	out := make(map[string]int64)
	if strings.TrimSpace(f.WorkspaceID) == "" {
		return out, nil
	}

	q := r.db.Table("whatsapp_campaign_entries AS e").
		Joins("JOIN whatsapp_campaigns c ON c.id = e.campaign_id").
		Joins("LEFT JOIN whatsapp_templates t ON t.id = c.template_id").
		Where("c.workspace_id = ?", f.WorkspaceID).
		Where("c.deleted_at IS NULL").
		Where("e.deleted_at IS NULL").
		// Billed statuses only — same subtractive set as StatusCounts.Dispatches.
		Where("e.status NOT IN ?", []string{
			string(wce.SendStatusPending),
			string(wce.SendStatusFailed),
			string(wce.SendStatusNotEligiblePossibleSpam),
		})

	if t := strings.TrimSpace(f.Type); t != "" {
		q = q.Where("c.type = ?", t)
	}
	if f.CreatedFrom != nil {
		q = q.Where("c.created_at >= ?", *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		q = q.Where("c.created_at <= ?", *f.CreatedTo)
	}
	if len(f.DepartmentIDs) > 0 {
		q = q.Where("c.department_id IN ?", f.DepartmentIDs)
	}

	var results []struct {
		Category string
		Count    int64
	}
	if err := q.Select("UPPER(COALESCE(NULLIF(TRIM(t.category), ''), 'UNKNOWN')) AS category, COUNT(*) AS count").
		Group("UPPER(COALESCE(NULLIF(TRIM(t.category), ''), 'UNKNOWN'))").
		Scan(&results).Error; err != nil {
		return nil, err
	}

	for _, row := range results {
		cat := strings.TrimSpace(row.Category)
		if cat == "" || cat == "UNKNOWN" {
			continue
		}
		out[cat] += row.Count
	}
	return out, nil
}

// addWAStatusCount folds a single (status, count) pair into a StatusCounts,
// updating both the matching status bucket and the running total. Centralising
// the status->field mapping keeps CountByStatus and CountByStatusForCampaigns
// in lockstep.
func addWAStatusCount(counts *wce.StatusCounts, status string, n int64) {
	switch wce.SendStatus(status) {
	case wce.SendStatusPending:
		counts.Pending += n
	case wce.SendStatusSent:
		counts.Sent += n
	case wce.SendStatusDelivered:
		counts.Delivered += n
	case wce.SendStatusRead:
		counts.Read += n
	case wce.SendStatusFailed:
		counts.Failed += n
	case wce.SendStatusNotEligiblePossibleSpam:
		counts.NotEligiblePossibleSpam += n
	}
	counts.Total += n
}

// dedupeNonEmpty trims, drops blanks and de-duplicates campaign IDs so the
// IN clause stays tight and stable regardless of caller input.
func dedupeNonEmpty(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (r *repository) UpdateStatus(entryID string, status wce.SendStatus, messageID string, errorCode int, errorMessage string) error {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return wce.ErrEntryNotFound
	}
	if !status.Valid() {
		return wce.ErrEntryStatusInvalid
	}

	updates := map[string]interface{}{
		"status": string(status),
	}
	if messageID != "" {
		updates["message_id"] = messageID
	}
	if errorCode != 0 {
		updates["error_code"] = errorCode
	}
	if errorMessage != "" {
		updates["error_message"] = errorMessage
	}

	return r.db.Model(&schema.WhatsAppCampaignEntry{}).Where("id = ?", entryID).Updates(updates).Error
}

func (r *repository) UpdateReceivedBusinessPhone(entryID string, businessPhoneID string) error {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return wce.ErrEntryNotFound
	}
	businessPhoneID = strings.TrimSpace(businessPhoneID)

	return r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Where("id = ?", entryID).
		Update("received_business_phone_id", businessPhoneID).Error
}

func (r *repository) UpdateStatusByNumber(campaignID, number string, status wce.SendStatus, messageID string) error {
	campaignID = strings.TrimSpace(campaignID)
	normalized := lead.NormalizeNumber(number)
	if campaignID == "" || normalized == "" {
		return wce.ErrEntryNotFound
	}
	if !status.Valid() {
		return wce.ErrEntryStatusInvalid
	}

	query := `
		UPDATE whatsapp_campaign_entries 
		SET status = ?, message_id = ?, updated_at = NOW() 
		WHERE campaign_id = ? AND lead_id IN (
			SELECT id FROM leads WHERE number = ?
		)
	`
	return r.db.Exec(query, string(status), messageID, campaignID, normalized).Error
}

func (r *repository) UpdateStatusByMessageID(messageID string, status wce.SendStatus) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return wce.ErrEntryNotFound
	}
	if !status.Valid() {
		return wce.ErrEntryStatusInvalid
	}

	statusPriority := map[string]int{
		string(wce.SendStatusPending):   0,
		string(wce.SendStatusSent):      1,
		string(wce.SendStatusDelivered): 2,
		string(wce.SendStatusRead):      3,
		string(wce.SendStatusFailed):    4,
	}

	newPriority := statusPriority[string(status)]

	query := r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Where("message_id = ?", messageID)

	if status != wce.SendStatusFailed {
		query = query.Where(`
			CASE status 
				WHEN 'PENDING' THEN 0 
				WHEN 'SENT' THEN 1 
				WHEN 'DELIVERED' THEN 2 
				WHEN 'READ' THEN 3 
				WHEN 'FAILED' THEN 4 
				ELSE -1 
			END < ?`, newPriority)
	}

	result := query.Updates(map[string]interface{}{
		"status": string(status),
	})

	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (r *repository) ResetAllStatuses(campaignID string) (int64, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return 0, wce.ErrEntryCampaignRequired
	}

	result := r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Where("campaign_id = ?", campaignID).
		Updates(map[string]interface{}{
			"status":     string(wce.SendStatusPending),
			"message_id": "",
		})

	return result.RowsAffected, result.Error
}

func (r *repository) ReplaceCampaignEntries(campaignID string, entries []wce.WhatsAppCampaignEntry) error {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return wce.ErrEntryCampaignRequired
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("campaign_id = ?", campaignID).Delete(&schema.WhatsAppCampaignEntry{}).Error; err != nil {
			return err
		}

		if len(entries) == 0 {
			return nil
		}

		schemaEntries := make([]schema.WhatsAppCampaignEntry, len(entries))
		for i, entry := range entries {
			entry.Normalize()
			entry.CampaignID = campaignID
			if entry.ID == "" {
				entry.ID = uuid.New().String()
			}
			schemaEntries[i] = *toSchema(&entry)
		}

		return tx.Create(&schemaEntries).Error
	})
}

func (r *repository) UpsertCampaignEntries(campaignID string, entries []wce.WhatsAppCampaignEntry) error {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return wce.ErrEntryCampaignRequired
	}

	if len(entries) == 0 {
		return nil
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		var existingEntries []schema.WhatsAppCampaignEntry
		if err := tx.Where("campaign_id = ? AND deleted_at IS NULL", campaignID).Find(&existingEntries).Error; err != nil {
			return err
		}

		existingLeadIDs := make(map[string]bool)
		for _, e := range existingEntries {
			existingLeadIDs[e.LeadID] = true
		}

		var newEntries []schema.WhatsAppCampaignEntry
		for _, entry := range entries {
			entry.Normalize()
			entry.CampaignID = campaignID

			if !existingLeadIDs[entry.LeadID] {
				if entry.ID == "" {
					entry.ID = uuid.New().String()
				}
				newEntries = append(newEntries, *toSchema(&entry))
			}
		}

		if len(newEntries) > 0 {
			batchSize := 500
			for i := 0; i < len(newEntries); i += batchSize {
				end := i + batchSize
				if end > len(newEntries) {
					end = len(newEntries)
				}
				if err := tx.Create(newEntries[i:end]).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func toSchema(e *wce.WhatsAppCampaignEntry) *schema.WhatsAppCampaignEntry {
	s := &schema.WhatsAppCampaignEntry{
		ID:                 e.ID,
		CampaignID:         e.CampaignID,
		LeadID:             e.LeadID,
		Status:             string(e.Status),
		MessageID:          e.MessageID,
		ErrorCode:          e.ErrorCode,
		ErrorMessage:       e.ErrorMessage,
		Variables:          pq.StringArray(e.Variables),
		AutomationEnabled: e.AutomationEnabled,
		Metadata:              schema.LeadMetadata(e.Metadata),
		ConversationStatus:    e.ConversationStatus,
		CloseSource:           e.CloseSource,
		CloseReason:           e.CloseReason,
		ClosedAt:              e.ClosedAt,
		LastMessageAt:         e.LastMessageAt,
		LastCustomerMessageAt: e.LastCustomerMessageAt,
		LastAgentMessageAt:    e.LastAgentMessageAt,
	}
	if e.ReceivedBusinessPhoneID != "" {
		s.ReceivedBusinessPhoneID = &e.ReceivedBusinessPhoneID
	}
	return s
}

func toDomain(e *schema.WhatsAppCampaignEntry) *wce.WhatsAppCampaignEntry {
	entry := &wce.WhatsAppCampaignEntry{
		ID:                    e.ID,
		CampaignID:            e.CampaignID,
		LeadID:                e.LeadID,
		Status:                wce.SendStatus(e.Status),
		MessageID:             e.MessageID,
		ErrorCode:             e.ErrorCode,
		ErrorMessage:          e.ErrorMessage,
		Variables:             []string(e.Variables),
		AutomationEnabled:    e.AutomationEnabled,
		ConversationStatus:    e.ConversationStatus,
		CloseSource:           e.CloseSource,
		CloseReason:           e.CloseReason,
		ClosedAt:              e.ClosedAt,
		LastMessageAt:         e.LastMessageAt,
		LastCustomerMessageAt: e.LastCustomerMessageAt,
		LastAgentMessageAt:    e.LastAgentMessageAt,
		Metadata:              map[string]interface{}(e.Metadata),
		CreatedAt:             e.CreatedAt,
		UpdatedAt:             e.UpdatedAt,
	}
	if e.ReceivedBusinessPhoneID != nil {
		entry.ReceivedBusinessPhoneID = *e.ReceivedBusinessPhoneID
	}
	return entry
}

func (r *repository) ListByStatus(campaignID string, status wce.SendStatus, limit int) ([]wce.WhatsAppCampaignEntry, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, wce.ErrEntryCampaignRequired
	}

	type entryWithLeadRow struct {
		schema.WhatsAppCampaignEntry
		LeadNumber string `gorm:"column:lead_number"`
		LeadName   string `gorm:"column:lead_name"`
		LeadAge    *int   `gorm:"column:lead_age"`
	}

	query := r.db.Table("whatsapp_campaign_entries").
		Select("whatsapp_campaign_entries.*, leads.number as lead_number, leads.name as lead_name, leads.age as lead_age").
		Joins("JOIN leads ON leads.id = whatsapp_campaign_entries.lead_id").
		Where("whatsapp_campaign_entries.campaign_id = ? AND whatsapp_campaign_entries.status = ? AND whatsapp_campaign_entries.deleted_at IS NULL", campaignID, string(status)).
		Order("whatsapp_campaign_entries.created_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}

	var rows []entryWithLeadRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}

	entries := make([]wce.WhatsAppCampaignEntry, len(rows))
	for i, row := range rows {
		entries[i] = wce.WhatsAppCampaignEntry{
			ID:         row.ID,
			CampaignID: row.CampaignID,
			LeadID:     row.LeadID,
			Lead: &lead.Lead{
				ID:     row.LeadID,
				Number: row.LeadNumber,
				Name:   row.LeadName,
				Age:    row.LeadAge,
			},
			Status:    wce.SendStatus(row.Status),
			MessageID: row.MessageID,
			Variables: []string(row.Variables),
			Metadata:  map[string]interface{}(row.WhatsAppCampaignEntry.Metadata),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		}
	}

	return entries, nil
}

func (r *repository) ListEntriesWithLeads(input wce.ListEntriesInput) (*shared.PaginatedResult[*wce.EntryWithLead], error) {
	input.Options.Pagination = shared.NormalizePagination(input.Options.Pagination)

	selectFields := "whatsapp_campaign_entries.*, leads.number as lead_number, leads.name as lead_name, leads.age as lead_age"
	if input.BusinessPhoneID != "" {
		selectFields += ", lmw.last_message_at as window_last_message_at"
	}

	query := r.db.Table("whatsapp_campaign_entries").
		Select(selectFields).
		Joins("JOIN leads ON leads.id = whatsapp_campaign_entries.lead_id").
		Where("whatsapp_campaign_entries.deleted_at IS NULL")

	if input.BusinessPhoneID != "" {
		query = query.Joins("LEFT JOIN lead_message_windows lmw ON lmw.lead_id = leads.id AND lmw.business_phone_id = ?", input.BusinessPhoneID)
	}

	if input.CampaignID != "" {
		query = query.Where("whatsapp_campaign_entries.campaign_id = ?", input.CampaignID)
	}
	if input.LeadID != "" {
		query = query.Where("whatsapp_campaign_entries.lead_id = ?", input.LeadID)
	}
	if input.Status.Valid() {
		query = query.Where("whatsapp_campaign_entries.status = ?", string(input.Status))
	}
	if input.Number != "" {
		normalized := lead.NormalizeNumber(input.Number)
		if normalized != "" {
			query = query.Where("leads.number = ?", normalized)
		}
	}

	if input.StageID != "" {
		query = query.Joins("JOIN entry_stages ON entry_stages.entry_id = whatsapp_campaign_entries.id AND entry_stages.entry_type = 'whatsapp'").
			Where("entry_stages.stage_id = ?", input.StageID)
	}

	if input.ErrorCode != 0 {
		query = query.Where("whatsapp_campaign_entries.error_code = ?", input.ErrorCode)
	}

	query = r.applyConversationFilter(query, input.ConversationFilter, "whatsapp")

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	offset := (input.Options.Pagination.Page - 1) * input.Options.Pagination.PageSize

	type entryWithLeadRow struct {
		schema.WhatsAppCampaignEntry
		LeadNumber          string     `gorm:"column:lead_number"`
		LeadName            string     `gorm:"column:lead_name"`
		LeadAge             *int       `gorm:"column:lead_age"`
		WindowLastMessageAt *time.Time `gorm:"column:window_last_message_at"`
	}

	var rows []entryWithLeadRow
	if err := query.
		Offset(offset).
		Limit(input.Options.Pagination.PageSize).
		Order("whatsapp_campaign_entries.created_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	windowDuration := 24 * time.Hour
	now := time.Now().UTC()

	items := make([]*wce.EntryWithLead, len(rows))
	for i, row := range rows {
		item := &wce.EntryWithLead{
			Entry: &wce.WhatsAppCampaignEntry{
				ID:           row.ID,
				CampaignID:   row.CampaignID,
				LeadID:       row.LeadID,
				Status:       wce.SendStatus(row.Status),
				MessageID:    row.MessageID,
				ErrorCode:    row.ErrorCode,
				ErrorMessage: row.ErrorMessage,
				Variables:    []string(row.Variables),
				CreatedAt:    row.CreatedAt,
				UpdatedAt:    row.UpdatedAt,
			},
			LeadID:        row.LeadID,
			Number:        row.LeadNumber,
			Name:          row.LeadName,
			Age:           row.LeadAge,
			Metadata:      map[string]interface{}(row.WhatsAppCampaignEntry.Metadata),
			LastMessageAt: row.WindowLastMessageAt,
		}
		if row.WindowLastMessageAt != nil && now.Sub(*row.WindowLastMessageAt) < windowDuration {
			item.CanReceiveNormalMessages = true
		}
		items[i] = item
	}

	totalPages := int(total) / input.Options.Pagination.PageSize
	if int(total)%input.Options.Pagination.PageSize > 0 {
		totalPages++
	}

	return &shared.PaginatedResult[*wce.EntryWithLead]{
		Items:      items,
		Page:       input.Options.Pagination.Page,
		PageSize:   input.Options.Pagination.PageSize,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

func (r *repository) FindByNumber(number string) (*wce.WhatsAppCampaignEntry, error) {
	normalized := lead.NormalizeNumber(number)
	if normalized == "" {
		return nil, wce.ErrEntryNotFound
	}

	phoneFormats := []string{normalized}
	if alternate := lead.GetAlternatePhoneFormat(normalized); alternate != "" {
		phoneFormats = append(phoneFormats, alternate)
	}

	var schemaEntry schema.WhatsAppCampaignEntry
	if err := r.db.
		Joins("JOIN leads ON leads.id = whatsapp_campaign_entries.lead_id").
		Joins("JOIN whatsapp_campaigns ON whatsapp_campaigns.id = whatsapp_campaign_entries.campaign_id").
		Where("leads.number IN ?", phoneFormats).
		Where("whatsapp_campaign_entries.status <> ?", string(wce.SendStatusNotEligiblePossibleSpam)).
		Order("whatsapp_campaign_entries.created_at DESC").
		First(&schemaEntry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wce.ErrEntryNotFound
		}
		return nil, err
	}

	return toDomain(&schemaEntry), nil
}

func (r *repository) FindByNumberAndBusinessPhone(number string, businessPhoneID string) (*wce.WhatsAppCampaignEntry, error) {
	normalized := lead.NormalizeNumber(number)
	if normalized == "" {
		return nil, wce.ErrEntryNotFound
	}

	businessPhoneID = strings.TrimSpace(businessPhoneID)
	if businessPhoneID == "" {

		return nil, wce.ErrEntryNotFound
	}

	phoneFormats := []string{normalized}
	if alternate := lead.GetAlternatePhoneFormat(normalized); alternate != "" {
		phoneFormats = append(phoneFormats, alternate)
	}

	var schemaEntry schema.WhatsAppCampaignEntry
	if err := r.db.
		Joins("JOIN leads ON leads.id = whatsapp_campaign_entries.lead_id").
		Joins("JOIN whatsapp_campaigns ON whatsapp_campaigns.id = whatsapp_campaign_entries.campaign_id").
		Where("leads.number IN ?", phoneFormats).
		Where("whatsapp_campaigns.business_phone_id = ?", businessPhoneID).
		Where("whatsapp_campaign_entries.status <> ?", string(wce.SendStatusNotEligiblePossibleSpam)).
		Order("whatsapp_campaign_entries.created_at DESC").
		First(&schemaEntry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wce.ErrEntryNotFound
		}
		return nil, err
	}

	return toDomain(&schemaEntry), nil
}

func (r *repository) applyConversationFilter(query *gorm.DB, filter wce.ConversationFilter, entryType string) *gorm.DB {
	if !filter.HasAnyFilter() {
		return query
	}

	msgQuery := r.db.Table("conversation_messages").
		Select("DISTINCT entry_id").
		Where("entry_type = ?", entryType)

	if filter.HasToolCalls != nil {
		if *filter.HasToolCalls {
			msgQuery = msgQuery.Where("message_type = ?", "tool_call")
		}
	}

	if filter.MessageType != "" {
		msgQuery = msgQuery.Where("message_type = ?", filter.MessageType)
	}

	if filter.ToolName != "" {
		toolPattern := "[Tool Call] " + filter.ToolName + ":"
		msgQuery = msgQuery.Where("message_type = ? AND text LIKE ?", "tool_call", toolPattern+"%")
	}

	if filter.HasToolCalls != nil && !*filter.HasToolCalls {
		notExistsQuery := r.db.Table("conversation_messages").
			Select("1").
			Where("conversation_messages.entry_id = whatsapp_campaign_entries.id").
			Where("entry_type = ?", entryType).
			Where("message_type = ?", "tool_call")

		query = query.Where("NOT EXISTS (?)", notExistsQuery)
	} else if filter.HasAnyFilter() {
		query = query.Where("whatsapp_campaign_entries.id IN (?)", msgQuery)
	}

	if filter.MinMessageCount != nil {
		countSubquery := r.db.Table("conversation_messages").
			Select("entry_id").
			Where("entry_type = ?", entryType).
			Group("entry_id").
			Having("COUNT(*) >= ?", *filter.MinMessageCount)
		query = query.Where("whatsapp_campaign_entries.id IN (?)", countSubquery)
	}

	if filter.MaxMessageCount != nil {
		countSubquery := r.db.Table("conversation_messages").
			Select("entry_id").
			Where("entry_type = ?", entryType).
			Group("entry_id").
			Having("COUNT(*) <= ?", *filter.MaxMessageCount)
		query = query.Where("whatsapp_campaign_entries.id IN (?)", countSubquery)
	}

	return query
}

func (r *repository) ListEntriesWithLeadsForUser(input wce.ListEntriesForUserInput) ([]*wce.EntryWithLead, int64, error) {
	userID := strings.TrimSpace(input.UserID)
	if userID == "" {
		return nil, 0, wce.ErrEntryNotFound
	}

	page := input.Page
	if page < 1 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize < 1 {
		pageSize = 50
	}

	subquery := r.db.Table("whatsapp_campaigns").
		Select("id").
		Where("workspace_id = ? AND deleted_at IS NULL", userID)

	query := r.db.Table("whatsapp_campaign_entries").
		Select(`whatsapp_campaign_entries.*, leads.number as lead_number, leads.name as lead_name, leads.age as lead_age,
			lmw.last_message_at as window_last_message_at,
			(SELECT MAX(created_at) FROM conversation_messages WHERE entry_id = whatsapp_campaign_entries.id AND entry_type = 'whatsapp') as last_conversation_at`).
		Joins("JOIN leads ON leads.id = whatsapp_campaign_entries.lead_id").
		Joins("JOIN whatsapp_campaigns ON whatsapp_campaigns.id = whatsapp_campaign_entries.campaign_id").
		Joins("LEFT JOIN lead_message_windows lmw ON lmw.lead_id = leads.id").
		Where("whatsapp_campaign_entries.campaign_id IN (?)", subquery).
		Where("whatsapp_campaign_entries.deleted_at IS NULL")

	if input.Search != "" {
		searchPattern := "%" + input.Search + "%"
		query = query.Where("leads.number LIKE ? OR leads.name LIKE ?", searchPattern, searchPattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type entryWithLeadRow struct {
		schema.WhatsAppCampaignEntry
		LeadNumber          string     `gorm:"column:lead_number"`
		LeadName            string     `gorm:"column:lead_name"`
		LeadAge             *int       `gorm:"column:lead_age"`
		WindowLastMessageAt *time.Time `gorm:"column:window_last_message_at"`
		LastConversationAt  *time.Time `gorm:"column:last_conversation_at"`
	}

	offset := (page - 1) * pageSize
	var rows []entryWithLeadRow
	if err := query.
		Order("COALESCE((SELECT MAX(created_at) FROM conversation_messages WHERE entry_id = whatsapp_campaign_entries.id AND entry_type = 'whatsapp'), whatsapp_campaign_entries.updated_at) DESC").
		Offset(offset).
		Limit(pageSize).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	windowDuration := 24 * time.Hour
	now := time.Now().UTC()

	items := make([]*wce.EntryWithLead, len(rows))
	for i, row := range rows {
		item := &wce.EntryWithLead{
			Entry: &wce.WhatsAppCampaignEntry{
				ID:           row.ID,
				CampaignID:   row.CampaignID,
				LeadID:       row.LeadID,
				Status:       wce.SendStatus(row.Status),
				MessageID:    row.MessageID,
				ErrorCode:    row.ErrorCode,
				ErrorMessage: row.ErrorMessage,
				Variables:    []string(row.Variables),
				CreatedAt:    row.CreatedAt,
				UpdatedAt:    row.UpdatedAt,
			},
			LeadID:        row.LeadID,
			Number:        row.LeadNumber,
			Name:          row.LeadName,
			Age:           row.LeadAge,
			Metadata:      map[string]interface{}(row.WhatsAppCampaignEntry.Metadata),
			LastMessageAt: row.WindowLastMessageAt,
		}
		if row.WindowLastMessageAt != nil && now.Sub(*row.WindowLastMessageAt) < windowDuration {
			item.CanReceiveNormalMessages = true
		}
		items[i] = item
	}

	return items, total, nil
}

func (r *repository) CanUserAccessEntry(workspaceID, entryID string, isAdmin bool) (bool, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	entryID = strings.TrimSpace(entryID)
	if workspaceID == "" || entryID == "" {
		return false, nil
	}

	var count int64
	query := r.db.Table("whatsapp_campaign_entries").
		Joins("JOIN whatsapp_campaigns ON whatsapp_campaigns.id = whatsapp_campaign_entries.campaign_id").
		Where("whatsapp_campaign_entries.id = ?", entryID).
		Where("whatsapp_campaign_entries.deleted_at IS NULL").
		Where("whatsapp_campaigns.deleted_at IS NULL")

	if !isAdmin {
		query = query.Where("whatsapp_campaigns.workspace_id = ?", workspaceID)
	}

	err := query.Count(&count).Error
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (r *repository) GetAccessibleEntryIDs(workspaceID string, isAdmin bool) ([]string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, nil
	}

	var entryIDs []string

	if isAdmin {
		err := r.db.Table("whatsapp_campaign_entries").
			Select("whatsapp_campaign_entries.id").
			Joins("JOIN whatsapp_campaigns ON whatsapp_campaigns.id = whatsapp_campaign_entries.campaign_id").
			Where("whatsapp_campaign_entries.deleted_at IS NULL").
			Where("whatsapp_campaigns.deleted_at IS NULL").
			Pluck("id", &entryIDs).Error
		if err != nil {
			return nil, err
		}
	} else {
		subquery := r.db.Table("whatsapp_campaigns").
			Select("id").
			Where("workspace_id = ? AND deleted_at IS NULL", workspaceID)

		err := r.db.Table("whatsapp_campaign_entries").
			Select("whatsapp_campaign_entries.id").
			Where("whatsapp_campaign_entries.campaign_id IN (?)", subquery).
			Where("whatsapp_campaign_entries.deleted_at IS NULL").
			Pluck("id", &entryIDs).Error
		if err != nil {
			return nil, err
		}
	}

	return entryIDs, nil
}

func (r *repository) GetEntryIDsByCampaign(campaignID string) ([]string, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, nil
	}

	var entryIDs []string
	err := r.db.Table("whatsapp_campaign_entries").
		Select("id").
		Where("campaign_id = ?", campaignID).
		Where("deleted_at IS NULL").
		Pluck("id", &entryIDs).Error

	if err != nil {
		return nil, err
	}

	return entryIDs, nil
}

func (r *repository) GetCampaignForEntry(entryID string) (*wce.EntryCampaignInfo, error) {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return nil, wce.ErrEntryNotFound
	}

	type campaignInfo struct {
		CampaignID      string
		WorkspaceID     string
		BusinessPhoneID string
	}

	var info campaignInfo
	err := r.db.Table("whatsapp_campaign_entries e").
		Select("e.campaign_id, wc.workspace_id, wc.business_phone_id").
		Joins("JOIN whatsapp_campaigns wc ON wc.id = e.campaign_id").
		Where("e.id = ?", entryID).
		Where("e.deleted_at IS NULL").
		Scan(&info).Error

	if err != nil {
		return nil, err
	}

	if info.CampaignID == "" {
		return nil, wce.ErrEntryNotFound
	}

	return &wce.EntryCampaignInfo{
		CampaignID:      info.CampaignID,
		WorkspaceID:     info.WorkspaceID,
		BusinessPhoneID: info.BusinessPhoneID,
	}, nil
}

func (r *repository) UpdateAutomationEnabled(entryID string, enabled *bool) error {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return wce.ErrEntryNotFound
	}

	result := r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Where("id = ? AND deleted_at IS NULL", entryID).
		Update("automation_enabled", enabled)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wce.ErrEntryNotFound
	}

	return nil
}

func (r *repository) UpdateMetadata(entryID string, metadata map[string]interface{}) error {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return wce.ErrEntryNotFound
	}

	result := r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Where("id = ? AND deleted_at IS NULL", entryID).
		Update("metadata", schema.LeadMetadata(metadata))

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wce.ErrEntryNotFound
	}

	return nil
}

func (r *repository) UpdateConversationStatus(entryID string, write wce.ConversationStatusWrite) error {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return wce.ErrEntryNotFound
	}

	updates := map[string]interface{}{
		"conversation_status": write.Status,
	}
	switch {
	case write.ClearCloseMeta:
		updates["close_source"] = ""
		updates["close_reason"] = ""
		updates["closed_at"] = nil
	case write.SetCloseMeta:
		updates["close_source"] = write.CloseSource
		updates["close_reason"] = write.CloseReason
		if write.ClosedAt != nil {
			updates["closed_at"] = *write.ClosedAt
		} else {
			now := time.Now().UTC()
			updates["closed_at"] = now
		}
	}

	result := r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Where("id = ? AND deleted_at IS NULL", entryID).
		Updates(updates)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wce.ErrEntryNotFound
	}

	return nil
}

// ListEligibleForAutoClose: one JOIN, partial-index friendly filters, hard limit.
// Eligibility: open status, workspace auto_close on, last word was agent/AI,
// silence past workspace idle hours. No per-row config lookup (no N+1).
func (r *repository) ListEligibleForAutoClose(limit int) ([]wce.AutoCloseCandidate, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	type row struct {
		EntryID            string    `gorm:"column:entry_id"`
		WorkspaceID        string    `gorm:"column:workspace_id"`
		LastAgentMessageAt time.Time `gorm:"column:last_agent_message_at"`
	}
	var rows []row
	// Per-workspace hours cannot be an Index Cond alone (value from JOIN).
	// Add a constant min-window bound (1h) so the planner can range-scan
	// idx_*_autoclose_agent and stop after LIMIT instead of walking all open rows.
	// Exact per-workspace filter remains in the JOIN Filter.
	err := r.db.Raw(`
		SELECT e.id AS entry_id,
		       c.workspace_id AS workspace_id,
		       e.last_agent_message_at AS last_agent_message_at
		FROM whatsapp_campaign_entries e
		INNER JOIN whatsapp_campaigns c
		  ON c.id = e.campaign_id AND c.deleted_at IS NULL
		INNER JOIN workspace_configs wc
		  ON wc.workspace_id = c.workspace_id
		WHERE e.deleted_at IS NULL
		  AND e.conversation_status IN ('new', 'ongoing')
		  AND wc.auto_close_enabled = TRUE
		  AND e.last_agent_message_at IS NOT NULL
		  AND e.last_agent_message_at < NOW() - INTERVAL '1 hour'
		  AND e.last_agent_message_at < NOW() - (GREATEST(wc.auto_close_idle_after_hours, 1) * INTERVAL '1 hour')
		  AND (e.last_customer_message_at IS NULL
		       OR e.last_customer_message_at < e.last_agent_message_at)
		ORDER BY e.last_agent_message_at ASC
		LIMIT ?
	`, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]wce.AutoCloseCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, wce.AutoCloseCandidate{
			EntryID:            row.EntryID,
			WorkspaceID:        row.WorkspaceID,
			LastAgentMessageAt: row.LastAgentMessageAt,
		})
	}
	return out, nil
}

// ListEligibleForMaxAge: absolute inactivity on last_message_at (any side).
// Workspace auto_close_max_age_enabled (default true). Reason max_age, not customer_idle.
func (r *repository) ListEligibleForMaxAge(limit int) ([]wce.AutoCloseCandidate, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	type row struct {
		EntryID            string    `gorm:"column:entry_id"`
		WorkspaceID        string    `gorm:"column:workspace_id"`
		LastAgentMessageAt time.Time `gorm:"column:last_message_at"`
	}
	var rows []row
	// Constant 24h min bound matches MinAutoCloseMaxAgeAfterHours for index range.
	err := r.db.Raw(`
		SELECT e.id AS entry_id,
		       c.workspace_id AS workspace_id,
		       e.last_message_at AS last_message_at
		FROM whatsapp_campaign_entries e
		INNER JOIN whatsapp_campaigns c
		  ON c.id = e.campaign_id AND c.deleted_at IS NULL
		INNER JOIN workspace_configs wc
		  ON wc.workspace_id = c.workspace_id
		WHERE e.deleted_at IS NULL
		  AND e.conversation_status IN ('new', 'ongoing')
		  AND wc.auto_close_max_age_enabled = TRUE
		  AND e.last_message_at IS NOT NULL
		  AND e.last_message_at < NOW() - INTERVAL '24 hours'
		  AND e.last_message_at < NOW() - (GREATEST(wc.auto_close_max_age_after_hours, 24) * INTERVAL '1 hour')
		ORDER BY e.last_message_at ASC
		LIMIT ?
	`, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]wce.AutoCloseCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, wce.AutoCloseCandidate{
			EntryID:            row.EntryID,
			WorkspaceID:        row.WorkspaceID,
			LastAgentMessageAt: row.LastAgentMessageAt,
		})
	}
	return out, nil
}

func (r *repository) CountByConversationStatus(campaignID string) (map[string]int64, error) {
	type statusCount struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:cnt"`
	}

	var results []statusCount
	if err := r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Select("conversation_status AS status, COUNT(*) AS cnt").
		Where("campaign_id = ? AND deleted_at IS NULL AND conversation_status != ''", campaignID).
		Group("conversation_status").
		Find(&results).Error; err != nil {
		return nil, err
	}

	counts := map[string]int64{"new": 0, "ongoing": 0, "finished": 0}
	for _, r := range results {
		counts[r.Status] += r.Count
	}
	return counts, nil
}

func (r *repository) CountByConversationStatusForWorkspace(workspaceID string) (map[string]int64, error) {
	type statusCount struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:cnt"`
	}

	var results []statusCount
	if err := r.db.Model(&schema.WhatsAppCampaignEntry{}).
		Select("whatsapp_campaign_entries.conversation_status AS status, COUNT(*) AS cnt").
		Joins("JOIN whatsapp_campaigns ON whatsapp_campaigns.id = whatsapp_campaign_entries.campaign_id AND whatsapp_campaigns.deleted_at IS NULL").
		Where("whatsapp_campaigns.workspace_id = ? AND whatsapp_campaign_entries.deleted_at IS NULL AND whatsapp_campaign_entries.conversation_status != ''", workspaceID).
		Group("whatsapp_campaign_entries.conversation_status").
		Find(&results).Error; err != nil {
		return nil, err
	}

	counts := map[string]int64{"new": 0, "ongoing": 0, "finished": 0}
	for _, r := range results {
		counts[r.Status] += r.Count
	}
	return counts, nil
}
