package unofficial_whatsapp_campaign_repository

import (
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/campaign"
	"vozko/domain/shared"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/campaignstamp"
)

type entryRepository struct{ db *gorm.DB }

func NewEntryRepository(db *gorm.DB) uwc.EntryRepository { return &entryRepository{db: db} }

func (r *entryRepository) CreateMany(entries []uwc.Entry) ([]uwc.Entry, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	rows := make([]schema.UnofficialWhatsAppCampaignEntry, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, entryToRow(e))
	}

	if err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "campaign_id"}, {Name: "lead_id"}},
		DoNothing: true,
	}).CreateInBatches(&rows, 500).Error; err != nil {
		return nil, err
	}

	out := make([]uwc.Entry, 0, len(rows))
	for i := range rows {
		out = append(out, *entryToDomain(&rows[i]))
	}
	return out, nil
}

func (r *entryRepository) FindByID(entryID string) (*uwc.Entry, error) {
	var row schema.UnofficialWhatsAppCampaignEntry
	if err := r.db.Where("id = ?", entryID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uwc.ErrEntryNotFound
		}
		return nil, err
	}
	return entryToDomain(&row), nil
}

func (r *entryRepository) FindByProviderMessageID(providerMessageID string) (*uwc.Entry, error) {
	id := strings.TrimSpace(providerMessageID)
	if id == "" {
		return nil, uwc.ErrEntryNotFound
	}
	var row schema.UnofficialWhatsAppCampaignEntry
	if err := r.db.Where("provider_message_id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uwc.ErrEntryNotFound
		}
		return nil, err
	}
	return entryToDomain(&row), nil
}

func (r *entryRepository) FindLatestByConversationID(conversationID string) (*uwc.Entry, error) {
	id := strings.TrimSpace(conversationID)
	if id == "" {
		return nil, uwc.ErrEntryNotFound
	}
	var row schema.UnofficialWhatsAppCampaignEntry
	err := r.db.
		Where("conversation_id = ?", id).
		Order("sent_at DESC NULLS LAST, updated_at DESC").
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uwc.ErrEntryNotFound
		}
		return nil, err
	}
	return entryToDomain(&row), nil
}

func (r *entryRepository) FindByCampaignAndLead(campaignID, leadID string) (*uwc.Entry, error) {
	var row schema.UnofficialWhatsAppCampaignEntry
	if err := r.db.Where("campaign_id = ? AND lead_id = ?", campaignID, leadID).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uwc.ErrEntryNotFound
		}
		return nil, err
	}
	return entryToDomain(&row), nil
}

func (r *entryRepository) Delete(entryID string) error {
	return r.db.Where("id = ?", entryID).
		Delete(&schema.UnofficialWhatsAppCampaignEntry{}).Error
}

func (r *entryRepository) DeleteByCampaignID(campaignID string) error {
	return r.db.Where("campaign_id = ?", campaignID).
		Delete(&schema.UnofficialWhatsAppCampaignEntry{}).Error
}

func (r *entryRepository) List(input uwc.ListEntriesInput) (*shared.PaginatedResult[*uwc.EntryWithLead], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	base := func() *gorm.DB {
		q := r.db.Model(&schema.UnofficialWhatsAppCampaignEntry{}).
			Where("unofficial_whatsapp_campaign_entries.campaign_id = ?", input.CampaignID)
		if input.Status != "" {
			q = q.Where("unofficial_whatsapp_campaign_entries.status = ?", string(input.Status))
		}
		if input.Number != "" {
			q = q.Where("unofficial_whatsapp_campaign_entries.number = ?", input.Number)
		}
		if input.ErrorCode != 0 {
			q = q.Where("unofficial_whatsapp_campaign_entries.error_code = ?", input.ErrorCode)
		}
		if search := strings.TrimSpace(input.Search); search != "" {
			like := "%" + search + "%"
			q = q.Where(
				"unofficial_whatsapp_campaign_entries.number ILIKE ? OR unofficial_whatsapp_campaign_entries.name ILIKE ?",
				like, like)
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, err
	}

	var rows []schema.UnofficialWhatsAppCampaignEntry
	if err := base().
		Order("unofficial_whatsapp_campaign_entries.created_at DESC").
		Offset(pagination.Offset()).Limit(pagination.PageSize).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]*uwc.EntryWithLead, 0, len(rows))
	for i := range rows {
		e := entryToDomain(&rows[i])
		items = append(items, &uwc.EntryWithLead{
			Entry:    e,
			LeadID:   e.LeadID,
			Number:   e.Number,
			Name:     e.Name,
			Metadata: e.Metadata,
		})
	}

	r.hydrateConversations(items)

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *entryRepository) hydrateConversations(items []*uwc.EntryWithLead) {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		if it.Entry != nil && it.Entry.ConversationID != "" {
			ids = append(ids, it.Entry.ConversationID)
		}
	}
	if len(ids) == 0 {
		return
	}

	type convRow struct {
		ID                 string
		ConversationStatus string
		LastMessageAt      *time.Time
		AutomationEnabled  *bool
	}
	var rows []convRow
	if err := r.db.Table("unofficial_whatsapp_conversations").
		Select("id, conversation_status, last_message_at, automation_enabled").
		Where("id IN ? AND deleted_at IS NULL", ids).
		Scan(&rows).Error; err != nil {
		return
	}

	byID := make(map[string]convRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	for _, it := range items {
		if it.Entry == nil {
			continue
		}
		if row, ok := byID[it.Entry.ConversationID]; ok {
			it.ConversationStatus = row.ConversationStatus
			it.LastMessageAt = row.LastMessageAt
			it.AutomationEnabled = row.AutomationEnabled
		}
	}
}

func (r *entryRepository) ListByStatus(campaignID string, status campaign.SendStatus, limit int) ([]uwc.Entry, error) {
	var rows []schema.UnofficialWhatsAppCampaignEntry
	if err := r.db.
		Where("campaign_id = ? AND status = ?", campaignID, string(status)).
		Order("created_at ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return mapEntryRows(rows), nil
}

func (r *entryRepository) ListRecentlyUpdated(campaignID string, limit int) ([]uwc.Entry, error) {
	var rows []schema.UnofficialWhatsAppCampaignEntry
	if err := r.db.Where("campaign_id = ?", campaignID).
		Order("updated_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return mapEntryRows(rows), nil
}

func (r *entryRepository) CountByStatus(campaignID string) (*campaign.Counts, error) {
	byCampaign, err := r.CountByStatusForCampaigns([]string{campaignID})
	if err != nil {
		return nil, err
	}
	if counts, ok := byCampaign[campaignID]; ok {
		return counts, nil
	}
	return &campaign.Counts{}, nil
}

func (r *entryRepository) CountByStatusForCampaigns(campaignIDs []string) (map[string]*campaign.Counts, error) {
	out := map[string]*campaign.Counts{}
	if len(campaignIDs) == 0 {
		return out, nil
	}

	type row struct {
		CampaignID string
		Status     string
		Total      int64
	}
	var rows []row
	if err := r.db.Model(&schema.UnofficialWhatsAppCampaignEntry{}).
		Select("campaign_id, status, COUNT(*) AS total").
		Where("campaign_id IN ?", campaignIDs).
		Group("campaign_id, status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, rec := range rows {
		counts, ok := out[rec.CampaignID]
		if !ok {
			counts = &campaign.Counts{}
			out[rec.CampaignID] = counts
		}
		addStatusCount(counts, campaign.SendStatus(rec.Status), rec.Total)
	}
	return out, nil
}

func addStatusCount(c *campaign.Counts, status campaign.SendStatus, n int64) {
	c.Total += n
	switch status {
	case campaign.SendStatusPending:
		c.Pending += n
	case campaign.SendStatusSent:
		c.Sent += n
	case campaign.SendStatusDelivered:
		c.Delivered += n
	case campaign.SendStatusRead:
		c.Read += n
	case campaign.SendStatusFailed:
		c.Failed += n
	case campaign.SendStatusNotEligiblePossibleSpam:
		c.NotEligiblePossibleSpam += n
	case campaign.SendStatusSkippedNotOnWhatsApp:
		c.SkippedNotOnWhatsApp += n
	}
}

func (r *entryRepository) UpdateStatus(entryID string, status campaign.SendStatus, providerMessageID string, errorCode int, errorMessage string) error {
	updates := map[string]interface{}{
		"status":        string(status),
		"error_code":    errorCode,
		"error_message": errorMessage,
		"updated_at":    time.Now().UTC(),
	}
	if providerMessageID != "" {
		updates["provider_message_id"] = providerMessageID
	}
	return r.db.Model(&schema.UnofficialWhatsAppCampaignEntry{}).
		Where("id = ?", entryID).
		Updates(campaignstamp.WithStamp(updates, status, time.Now().UTC())).Error
}

func (r *entryRepository) UpdateStatusByProviderMessageID(providerMessageID string, status campaign.SendStatus) error {
	rank := deliveryRank(status)
	if rank == 0 {
		return nil
	}

	ranked := make([]string, 0, 3)
	for _, s := range []campaign.SendStatus{
		campaign.SendStatusSent, campaign.SendStatusDelivered, campaign.SendStatusRead,
	} {
		if deliveryRank(s) < rank {
			ranked = append(ranked, string(s))
		}
	}
	ranked = append(ranked, string(campaign.SendStatusPending))

	now := time.Now().UTC()
	return r.db.Model(&schema.UnofficialWhatsAppCampaignEntry{}).
		Where("provider_message_id = ? AND status IN ?", providerMessageID, ranked).
		Updates(campaignstamp.WithStamp(map[string]interface{}{
			"status":     string(status),
			"updated_at": now,
		}, status, now)).Error
}

func deliveryRank(s campaign.SendStatus) int {
	switch s {
	case campaign.SendStatusSent:
		return 1
	case campaign.SendStatusDelivered:
		return 2
	case campaign.SendStatusRead:
		return 3
	default:
		return 0
	}
}

func (r *entryRepository) RecordCheck(entryID, jid string, at time.Time, onWhatsApp bool) error {
	updates := map[string]interface{}{
		"checked_at": at.UTC(),
		"updated_at": time.Now().UTC(),
	}
	if onWhatsApp {
		updates["jid"] = jid
	} else {
		updates["jid"] = ""
		updates["status"] = string(campaign.SendStatusSkippedNotOnWhatsApp)
	}
	return r.db.Model(&schema.UnofficialWhatsAppCampaignEntry{}).
		Where("id = ?", entryID).Updates(updates).Error
}

func (r *entryRepository) RecordSend(entryID string, in uwc.RecordSendInput) error {
	return r.db.Model(&schema.UnofficialWhatsAppCampaignEntry{}).
		Where("id = ?", entryID).
		Updates(map[string]interface{}{
			"status":              string(campaign.SendStatusSent),
			"contact_id":          ptr(in.ContactID),
			"conversation_id":     ptr(in.ConversationID),
			"provider_message_id": in.ProviderMessageID,
			"message_id":          ptr(in.MessageID),
			"variant_index":       in.VariantIndex,
			"sent_at":             in.SentAt.UTC(),
			"error_code":          0,
			"error_message":       "",
			"updated_at":          time.Now().UTC(),
		}).Error
}

func (r *entryRepository) UpdateEntryDetails(entryID string, in uwc.UpdateEntryDetails) error {
	meta := schema.LeadMetadata{}
	for k, v := range in.Metadata {
		meta[k] = v
	}

	err := r.db.Model(&schema.UnofficialWhatsAppCampaignEntry{}).
		Where("id = ?", entryID).
		Updates(map[string]interface{}{
			"lead_id":    in.LeadID,
			"number":     in.Number,
			"name":       in.Name,
			"variables":  pq.StringArray(in.Variables),
			"metadata":   meta,
			"updated_at": time.Now().UTC(),
		}).Error

	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return uwc.ErrEntryDuplicate
	}
	return err
}

const uniqueViolation = "23505"

func (r *entryRepository) ResetAllStatuses(campaignID string) (int64, error) {
	updates := campaignstamp.Clear()
	updates["status"] = string(campaign.SendStatusPending)
	updates["provider_message_id"] = ""
	updates["message_id"] = nil
	updates["error_code"] = 0
	updates["error_message"] = ""
	updates["updated_at"] = time.Now().UTC()
	result := r.db.Model(&schema.UnofficialWhatsAppCampaignEntry{}).
		Where("campaign_id = ?", campaignID).
		Updates(updates)
	return result.RowsAffected, result.Error
}

func (r *entryRepository) UpsertEntries(campaignID string, entries []uwc.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	rows := make([]schema.UnofficialWhatsAppCampaignEntry, 0, len(entries))
	for _, e := range entries {
		e.CampaignID = campaignID
		rows = append(rows, entryToRow(e))
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "campaign_id"}, {Name: "lead_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"number", "name", "variables", "metadata", "updated_at"}),
	}).CreateInBatches(&rows, 500).Error
}

func (r *entryRepository) ConversationIDsForCampaign(campaignID string) ([]string, error) {
	var ids []string
	err := r.db.Model(&schema.UnofficialWhatsAppCampaignEntry{}).
		Where("campaign_id = ? AND conversation_id IS NOT NULL", campaignID).
		Distinct().Pluck("conversation_id", &ids).Error
	return ids, err
}

func mapEntryRows(rows []schema.UnofficialWhatsAppCampaignEntry) []uwc.Entry {
	out := make([]uwc.Entry, 0, len(rows))
	for i := range rows {
		out = append(out, *entryToDomain(&rows[i]))
	}
	return out
}
