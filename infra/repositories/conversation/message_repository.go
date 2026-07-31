package conversation_repository

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	crmfiltersql "vozko/infra/repositories/crmfilter"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func inboundTypesSQL() string {
	types := conversation.InboundMessageTypeStrings()
	quoted := make([]string, len(types))
	for i, t := range types {
		quoted[i] = fmt.Sprintf("'%s'", t)
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

func departmentScopeClause(deptColumn, entryIDColumn string, departmentIDs []string, restrict bool, assigneeUserID string) (string, []interface{}) {
	if !restrict {
		return "", nil
	}
	if assigneeUserID != "" && entryIDColumn != "" {
		if len(departmentIDs) == 0 {
			clause := fmt.Sprintf(
				" AND EXISTS (SELECT 1 FROM inbox_assignments ia_d WHERE ia_d.entry_id = %s AND ia_d.assigned_user_id = ?)",
				entryIDColumn,
			)
			return clause, []interface{}{assigneeUserID}
		}
		clause := fmt.Sprintf(
			" AND (%s = ANY(?::uuid[]) OR EXISTS (SELECT 1 FROM inbox_assignments ia_d WHERE ia_d.entry_id = %s AND ia_d.assigned_user_id = ?))",
			deptColumn, entryIDColumn,
		)
		return clause, []interface{}{pq.Array(departmentIDs), assigneeUserID}
	}
	if len(departmentIDs) == 0 {
		return " AND 1 = 0", nil
	}
	return fmt.Sprintf(" AND %s = ANY(?::uuid[])", deptColumn), []interface{}{pq.Array(departmentIDs)}
}

// assignedSelfClause restricts an entry family to conversations the given user
// owns OR that are unassigned (the shared pool). It is the board/list equivalent
// of the inbox's self-scope (see SearchInboxEntries' AssignedUserID clause): a
// member who lacks conversations:view_others must never see entries assigned to
// OTHER members. Empty userID or column yields no clause (admins/owners/members
// who can view others). The identical predicate to the inbox guarantees the
// table, kanban, and inbox all show the same set for the same user.
func assignedSelfClause(entryIDColumn, assignedUserID string) (string, []interface{}) {
	if assignedUserID == "" || entryIDColumn == "" {
		return "", nil
	}
	clause := fmt.Sprintf(
		" AND (NOT EXISTS (SELECT 1 FROM inbox_assignments ia_s WHERE ia_s.entry_id = %s)"+
			" OR EXISTS (SELECT 1 FROM inbox_assignments ia_s2 WHERE ia_s2.entry_id = %s AND ia_s2.assigned_user_id = ?))",
		entryIDColumn, entryIDColumn,
	)
	return clause, []interface{}{assignedUserID}
}

func NewRepository(db *gorm.DB) conversation.MessageRepository {
	return &repository{db: db}
}

// instagramContactJoin projects instagram_contacts into the column shape the
// inbox query expects from its `l` (lead) alias.
//
// The query's search and select clauses reference l.name, l.number,
// l.profile_picture_url and l.blocked. An Instagram contact has no phone number,
// so the @handle fills the number slot — that is what an operator searches by,
// and it keeps every existing predicate working without duplicating the query per
// channel.
const instagramContactJoin = `JOIN (
	SELECT id, name, username AS number, profile_picture_url, blocked, deleted_at
	FROM instagram_contacts
) l ON l.id = igc.contact_id AND l.deleted_at IS NULL`

// entryTableForLastMessage maps a message entry_type to the entry table carrying
// the denormalized last_message_at. Unknown types have no entry row to touch.
func entryTableForLastMessage(entryType string) string {
	switch entryType {
	case string(conversation.MessageChannelWhatsApp):
		return "whatsapp_campaign_entries"
	case string(conversation.MessageChannelSupport):
		return "support_entries"
	case string(conversation.MessageChannelInstagram):
		return "instagram_conversations"
	default:
		return ""
	}
}

// touchEntryLastMessageAt moves the entry's denormalized last_message_at forward to
// at. Monotonic — it never moves backwards, so retries and out-of-order writes are
// safe. Best effort: the message itself is already durable, so a failure here must
// not fail the write; the value self-heals on the next message, and the backfill
// migration re-seeds anything that drifts.
//
// When msgType is known it also advances last_customer_message_at or
// last_agent_message_at in the same UPDATE (one round-trip, no N+1).
func (r *repository) touchEntryLastMessageAt(entryID, entryType string, at time.Time) {
	r.touchEntryMessageClocks(entryID, entryType, "", at)
}

// touchEntryMessageClocks updates last_message_at and, when msgType is set, the
// customer/agent idle-close clocks. Support entries only have last_message_at.
func (r *repository) touchEntryMessageClocks(entryID, entryType string, msgType conversation.MessageType, at time.Time) {
	table := entryTableForLastMessage(entryType)
	if table == "" || entryID == "" || at.IsZero() {
		return
	}

	// support_entries: only last_message_at (no auto-close clocks).
	if table == "support_entries" {
		if err := r.db.Exec(fmt.Sprintf(`
			UPDATE %s SET last_message_at = ?
			WHERE id = ? AND (last_message_at IS NULL OR last_message_at < ?)`, table),
			at, entryID, at).Error; err != nil {
			log.Printf("[conversation] last_message_at bump failed (%s %s): %v", entryType, entryID, err)
		}
		return
	}

	setCustomer := msgType != "" && msgType.IsInbound()
	setAgent := false
	if msgType != "" {
		switch msgType {
		case conversation.MessageTypeOperator, conversation.MessageTypeAIResponse, conversation.MessageTypeTemplate:
			setAgent = true
		}
	}

	// Single UPDATE for all clocks that apply. Monotonic on each column.
	sql := fmt.Sprintf(`UPDATE %s SET last_message_at = CASE
			WHEN last_message_at IS NULL OR last_message_at < ? THEN ? ELSE last_message_at END`, table)
	args := []interface{}{at, at}
	if setCustomer {
		sql += `, last_customer_message_at = CASE
			WHEN last_customer_message_at IS NULL OR last_customer_message_at < ? THEN ? ELSE last_customer_message_at END`
		args = append(args, at, at)
	}
	if setAgent {
		sql += `, last_agent_message_at = CASE
			WHEN last_agent_message_at IS NULL OR last_agent_message_at < ? THEN ? ELSE last_agent_message_at END`
		args = append(args, at, at)
	}
	sql += ` WHERE id = ?`
	args = append(args, entryID)

	if err := r.db.Exec(sql, args...).Error; err != nil {
		log.Printf("[conversation] message clock bump failed (%s %s): %v", entryType, entryID, err)
	}
}

// recomputeEntryLastMessageAt re-derives last_message_at from the surviving
// messages. Needed when messages are removed, where the stored value must be able
// to recede — or become NULL, which correctly drops the entry out of the inbox
// again (mirroring the inner-join semantics the old JOIN LATERAL had).
func (r *repository) recomputeEntryLastMessageAt(entryID, entryType string) {
	table := entryTableForLastMessage(entryType)
	if table == "" || entryID == "" {
		return
	}
	if err := r.db.Exec(fmt.Sprintf(`
		UPDATE %s SET last_message_at = (
			SELECT MAX(created_at) FROM conversation_messages
			WHERE entry_id = ? AND entry_type = ? AND deleted_at IS NULL
		) WHERE id = ?`, table), entryID, entryType, entryID).Error; err != nil {
		log.Printf("[conversation] last_message_at recompute failed (%s %s): %v", entryType, entryID, err)
	}
}

func (r *repository) Create(message *conversation.Message) error {
	if message == nil {
		return conversation.ErrMessageContentRequired
	}

	dbMessage := mapDomainToSchema(message)
	if err := r.db.Create(dbMessage).Error; err != nil {
		return err
	}
	// Keep denormalized clocks current (inbox order + idle auto-close eligibility).
	r.touchEntryMessageClocks(
		dbMessage.EntryID,
		dbMessage.EntryType,
		conversation.MessageType(dbMessage.MessageType),
		dbMessage.CreatedAt,
	)
	return nil
}

func (r *repository) Update(messageID string, message *conversation.Message) error {
	if message == nil {
		return conversation.ErrMessageContentRequired
	}

	updates := map[string]interface{}{}
	if message.EntryID != "" {
		updates["entry_id"] = message.EntryID
	}
	if string(message.EntryType) != "" {
		updates["entry_type"] = string(message.EntryType)
	}
	if message.From != "" {
		updates["from_participant"] = message.From
	}
	if message.To != "" {
		updates["to_participant"] = message.To
	}
	if message.Text != "" {
		updates["text"] = message.Text
	}
	if len(message.Image) > 0 {
		updates["image"] = cloneBytes(message.Image)
	}
	if len(message.Video) > 0 {
		updates["video"] = cloneBytes(message.Video)
	}
	if message.MediaID != nil && *message.MediaID != "" {
		updates["media_id"] = *message.MediaID
	}
	if string(message.MediaType) != "" {
		updates["media_type"] = string(message.MediaType)
	}

	if len(updates) == 0 {
		return nil
	}

	result := r.db.Model(&schema.ConversationMessage{}).
		Where("id = ?", messageID).
		Updates(updates)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return conversation.ErrMessageNotFound
	}
	return nil
}

func (r *repository) Delete(messageID string) error {
	// Read the owning entry before the delete: afterwards the row is soft-deleted
	// and the entry it belonged to can no longer be resolved from it.
	var owner schema.ConversationMessage
	hasOwner := r.db.Select("entry_id", "entry_type").
		Where("id = ?", messageID).First(&owner).Error == nil

	result := r.db.Delete(&schema.ConversationMessage{}, "id = ?", messageID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return conversation.ErrMessageNotFound
	}
	// This may have removed the newest message, so last_message_at has to be
	// re-derived (it can recede, or go NULL if that was the only message).
	if hasOwner {
		r.recomputeEntryLastMessageAt(owner.EntryID, owner.EntryType)
	}
	return nil
}

func (r *repository) ClearAll() error {
	return r.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&schema.ConversationMessage{}).Error
}

func (r *repository) GetByID(id string) (*conversation.Message, error) {
	var dbMessage schema.ConversationMessage
	if err := r.db.Where("id = ?", id).First(&dbMessage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, conversation.ErrMessageNotFound
		}
		return nil, err
	}

	return mapSchemaToDomain(&dbMessage), nil
}

func (r *repository) ListByEntry(entryID string, entryType shared.EntryType) ([]*conversation.Message, error) {
	var dbMessages []schema.ConversationMessage
	if err := r.db.Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).
		Order("created_at ASC").
		Find(&dbMessages).Error; err != nil {
		return nil, err
	}

	messages := make([]*conversation.Message, 0, len(dbMessages))
	for i := range dbMessages {
		messages = append(messages, mapSchemaToDomain(&dbMessages[i]))
	}

	return messages, nil
}

func (r *repository) ListByEntryPaginated(input conversation.ListMessagesInput) ([]*conversation.Message, error) {
	var dbMessages []schema.ConversationMessage

	query := r.db.Where("entry_id = ? AND entry_type = ?", input.EntryID, string(input.EntryType))

	if input.Before != nil {
		query = query.Where("created_at < ?", *input.Before)
	}
	if input.After != nil {
		query = query.Where("created_at > ?", *input.After)
	}

	if input.Limit > 0 {
		query = query.Limit(input.Limit)
	}

	if err := query.Order("created_at DESC").Find(&dbMessages).Error; err != nil {
		return nil, err
	}

	messages := make([]*conversation.Message, 0, len(dbMessages))
	for i := range dbMessages {
		messages = append(messages, mapSchemaToDomain(&dbMessages[i]))
	}

	return messages, nil
}

func (r *repository) ListByLeadID(leadID string) ([]*conversation.Message, error) {
	var dbMessages []schema.ConversationMessage

	query := r.db.Where(`
		entry_type = 'whatsapp' AND entry_id IN (
			SELECT id FROM whatsapp_campaign_entries WHERE lead_id = ? AND deleted_at IS NULL
		)
	`, leadID).Order("created_at ASC")

	if err := query.Find(&dbMessages).Error; err != nil {
		return nil, err
	}

	messages := make([]*conversation.Message, 0, len(dbMessages))
	for i := range dbMessages {
		messages = append(messages, mapSchemaToDomain(&dbMessages[i]))
	}

	return messages, nil
}

func (r *repository) MarkAsRead(input conversation.MarkAsReadInput) (int64, error) {
	now := time.Now().UTC()

	query := r.db.Model(&schema.ConversationMessage{}).
		Where("entry_id = ? AND entry_type = ?", input.EntryID, string(input.EntryType)).
		Where("read = ?", false).
		Where("message_type IN ?", conversation.InboundMessageTypeStrings())

	if input.UpToTimestamp != nil {
		query = query.Where("created_at <= ?", *input.UpToTimestamp)
	}
	if len(input.MessageIDs) > 0 {
		query = query.Where("id IN ?", input.MessageIDs)
	}

	result := query.Updates(map[string]interface{}{
		"read":    true,
		"read_at": now,
		"read_by": input.ReadBy,
	})

	if result.Error != nil {
		return 0, result.Error
	}

	return result.RowsAffected, nil
}

func (r *repository) CountUnreadByEntry(entryID string, entryType shared.EntryType) (int64, error) {
	var count int64
	err := r.db.Model(&schema.ConversationMessage{}).
		Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).
		Where("read = ?", false).
		Where("message_type IN ?", conversation.InboundMessageTypeStrings()).
		Count(&count).Error
	return count, err
}

func (r *repository) CountUnreadByEntries(entryIDs []string, entryType shared.EntryType) ([]conversation.UnreadCount, error) {
	if len(entryIDs) == 0 {
		return nil, nil
	}

	type countResult struct {
		EntryID string
		Count   int64
	}

	var results []countResult
	err := r.db.Model(&schema.ConversationMessage{}).
		Select("entry_id, COUNT(*) as count").
		Where("entry_id IN ?", entryIDs).
		Where("entry_type = ?", string(entryType)).
		Where("read = ?", false).
		Where("message_type IN ?", conversation.InboundMessageTypeStrings()).
		Group("entry_id").
		Scan(&results).Error

	if err != nil {
		return nil, err
	}

	counts := make([]conversation.UnreadCount, 0, len(results))
	for _, r := range results {
		counts = append(counts, conversation.UnreadCount{
			EntryID:   r.EntryID,
			EntryType: entryType,
			Count:     r.Count,
		})
	}

	return counts, nil
}

func (r *repository) DeleteByEntry(entryID string, entryType shared.EntryType) error {
	if err := r.db.Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).
		Delete(&schema.ConversationMessage{}).Error; err != nil {
		return err
	}
	// Every message of this entry is gone, so last_message_at must go NULL — which
	// drops the entry out of the inbox, exactly as the old LATERAL did.
	r.recomputeEntryLastMessageAt(entryID, string(entryType))
	return nil
}

func (r *repository) DeleteByCampaignID(campaignID string, entryType shared.EntryType) (int64, error) {
	var result *gorm.DB

	switch entryType {
	case shared.EntryTypeWhatsApp:
		result = r.db.Exec(`
			DELETE FROM conversation_messages 
			WHERE entry_type = ? 
			  AND entry_id::uuid IN (
				SELECT id FROM whatsapp_campaign_entries 
				WHERE campaign_id = ? AND deleted_at IS NULL
			  )
		`, string(entryType), campaignID)
	default:
		return 0, errors.New("invalid entry type for campaign message deletion")
	}

	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *repository) CountByCampaignID(campaignID string, entryType shared.EntryType) (int64, error) {
	var count int64

	switch entryType {
	case shared.EntryTypeWhatsApp:
		err := r.db.Raw(`
			SELECT COUNT(*) FROM conversation_messages 
			WHERE entry_type = ? 
			  AND entry_id::uuid IN (
				SELECT id FROM whatsapp_campaign_entries 
				WHERE campaign_id = ? AND deleted_at IS NULL
			  )
		`, string(entryType), campaignID).Scan(&count).Error
		if err != nil {
			return 0, err
		}
	default:
		return 0, errors.New("invalid entry type for campaign message count")
	}

	return count, nil
}

func mapDomainToSchema(message *conversation.Message) *schema.ConversationMessage {
	if message == nil {
		return nil
	}

	return &schema.ConversationMessage{
		ID:                message.ID,
		EntryID:           message.EntryID,
		EntryType:         string(message.EntryType),
		Channel:           string(message.Channel),
		MessageType:       string(message.MessageType),
		FromParticipant:   message.From,
		ToParticipant:     message.To,
		Text:              message.Text,
		Image:             cloneBytes(message.Image),
		Video:             cloneBytes(message.Video),
		MediaID:           message.MediaID,
		MediaType:         string(message.MediaType),
		Read:              message.Read,
		ReadAt:            message.ReadAt,
		ReadBy:            message.ReadBy,
		WhatsAppMessageID: message.WhatsAppMessageID,
		ExternalMessageID: message.ExternalMessageID,
		ReplyToMessageID:  message.ReplyToMessageID,
		DeliveryStatus:    string(message.DeliveryStatus),
		Metadata:          message.Metadata,
		CreatedAt:         message.CreatedAt,
		UpdatedAt:         message.UpdatedAt,
	}
}

func mapSchemaToDomain(message *schema.ConversationMessage) *conversation.Message {
	if message == nil {
		return nil
	}

	return &conversation.Message{
		ID:                message.ID,
		EntryID:           message.EntryID,
		EntryType:         shared.EntryType(message.EntryType),
		Channel:           conversation.MessageChannel(message.Channel),
		MessageType:       conversation.MessageType(message.MessageType),
		From:              message.FromParticipant,
		To:                message.ToParticipant,
		Text:              message.Text,
		Image:             cloneBytes(message.Image),
		Video:             cloneBytes(message.Video),
		MediaID:           message.MediaID,
		MediaType:         conversation.MediaType(message.MediaType),
		Read:              message.Read,
		ReadAt:            message.ReadAt,
		ReadBy:            message.ReadBy,
		WhatsAppMessageID: message.WhatsAppMessageID,
		ExternalMessageID: message.ExternalMessageID,
		ReplyToMessageID:  message.ReplyToMessageID,
		DeliveryStatus:    conversation.DeliveryStatus(message.DeliveryStatus),
		Metadata:          message.Metadata,
		CreatedAt:         message.CreatedAt,
		UpdatedAt:         message.UpdatedAt,
	}
}

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	cloned := make([]byte, len(src))
	copy(cloned, src)
	return cloned
}

func (r *repository) GetEntriesWithMessages(campaignID string, entryIDs []string, entryType shared.EntryType, page, pageSize int, assignedUserID string) ([]conversation.EntryWithLastMessage, int64, error) {
	useCampaignFilter := campaignID != ""

	var filteredIDs []string
	if !useCampaignFilter {
		if len(entryIDs) == 0 {
			return nil, 0, nil
		}
		filteredIDs = make([]string, 0, len(entryIDs))
		for _, id := range entryIDs {
			if id != "" {
				filteredIDs = append(filteredIDs, id)
			}
		}
		if len(filteredIDs) == 0 {
			return nil, 0, nil
		}
	}

	et := string(entryType)

	assignmentFilterFor := func(col string) string {
		if assignedUserID == "" {
			return ""
		}
		return fmt.Sprintf(` AND (NOT EXISTS (SELECT 1 FROM inbox_assignments ia WHERE ia.entry_id = %s) OR EXISTS (SELECT 1 FROM inbox_assignments ia2 WHERE ia2.entry_id = %s AND ia2.assigned_user_id = ?))`, col, col)
	}
	var assignmentArgs []interface{}
	if assignedUserID != "" {
		assignmentArgs = append(assignmentArgs, assignedUserID)
	}

	var entryCTE string
	var cteArgs []interface{}
	var leadField, bphoneField, campaignIDField, campaignNameField, entryJoin, aiFields string

	switch entryType {
	case "whatsapp":
		leadField = "wce.lead_id::text"
		bphoneField = "COALESCE(wc.business_phone_id::text, '')"
		campaignIDField = "wc.id::text"
		campaignNameField = "wc.name"
		aiFields = "COALESCE(wc.agent_id::text, '') AS agent_id, COALESCE(wc.workflow_id::text, '') AS workflow_id, wc.enable_agent_responses AS agent_responses_enabled, wc.enable_workflow AS workflow_enabled"
		entryJoin = `JOIN whatsapp_campaign_entries wce ON wce.id = e.entry_id AND wce.deleted_at IS NULL
		             JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id`
		if useCampaignFilter {
			entryCTE = `SELECT wce_f.id AS entry_id FROM whatsapp_campaign_entries wce_f WHERE wce_f.campaign_id = ? AND wce_f.deleted_at IS NULL` + assignmentFilterFor("wce_f.id")
			cteArgs = append([]interface{}{campaignID}, assignmentArgs...)
		} else {
			entryCTE = `SELECT u.entry_id FROM unnest(?::uuid[]) AS u(entry_id) WHERE 1=1` + assignmentFilterFor("u.entry_id")
			cteArgs = append([]interface{}{pq.Array(filteredIDs)}, assignmentArgs...)
		}

	case shared.EntryTypeInstagram:
		// The Instagram account plays the role whatsapp_campaigns plays for
		// WhatsApp: it is the container that carries the automation config. There
		// is no campaign, so campaignID here is the account id.
		leadField = "igc.contact_id::text"
		bphoneField = "COALESCE(igc.ig_account_id::text, '')"
		campaignIDField = "iga.id::text"
		campaignNameField = "iga.username"
		aiFields = "COALESCE(iga.agent_id::text, '') AS agent_id, COALESCE(iga.workflow_id::text, '') AS workflow_id, iga.enable_agent_responses AS agent_responses_enabled, iga.enable_workflow AS workflow_enabled"
		entryJoin = `JOIN instagram_conversations igc ON igc.id = e.entry_id AND igc.deleted_at IS NULL
		             JOIN instagram_accounts iga ON iga.id = igc.ig_account_id`
		if useCampaignFilter {
			entryCTE = `SELECT igc_f.id AS entry_id FROM instagram_conversations igc_f WHERE igc_f.ig_account_id = ? AND igc_f.deleted_at IS NULL` + assignmentFilterFor("igc_f.id")
			cteArgs = append([]interface{}{campaignID}, assignmentArgs...)
		} else {
			entryCTE = `SELECT u.entry_id FROM unnest(?::uuid[]) AS u(entry_id) WHERE 1=1` + assignmentFilterFor("u.entry_id")
			cteArgs = append([]interface{}{pq.Array(filteredIDs)}, assignmentArgs...)
		}

	default:
		return nil, 0, fmt.Errorf("invalid entry type: %s", et)
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM (%s) AS cnt`, entryCTE)
	var totalCount int64
	if err := r.db.Raw(countQuery, cteArgs...).Scan(&totalCount).Error; err != nil {
		return nil, 0, fmt.Errorf("error counting entries with messages: %w", err)
	}
	if totalCount == 0 {
		return nil, 0, nil
	}

	offset := (page - 1) * pageSize

	type msgResult struct {
		EntryID         string    `gorm:"column:entry_id"`
		EntryType       string    `gorm:"column:entry_type"`
		LeadID          string    `gorm:"column:lead_id"`
		BusinessPhoneID string    `gorm:"column:business_phone_id"`
		CampaignID      string    `gorm:"column:campaign_id"`
		CampaignName    string    `gorm:"column:campaign_name"`
		UnreadCount     int64     `gorm:"column:unread_count"`
		LastMessageText string    `gorm:"column:last_message_text"`
		LastMessageType string    `gorm:"column:last_message_type"`
		LastMessageAt   time.Time `gorm:"column:last_message_at"`
		LastMessageFrom string    `gorm:"column:last_message_from"`
		HasMedia        bool      `gorm:"column:has_media"`
		MediaType       string    `gorm:"column:media_type"`
		AgentID         string    `gorm:"column:agent_id"`
		WorkflowID      string    `gorm:"column:workflow_id"`
		AgentEnabled    bool      `gorm:"column:agent_responses_enabled"`
		WorkflowEnabled bool      `gorm:"column:workflow_enabled"`
	}

	query := fmt.Sprintf(`
		WITH filtered_entries AS (%s),
		entries_with_last_msg AS (
			SELECT e.entry_id, %s AS lead_id, %s AS business_phone_id,
			       %s AS campaign_id, %s AS campaign_name,
			       %s,
			       lm.text AS last_message_text, lm.message_type AS last_message_type,
			       lm.created_at AS last_message_at, lm.from_participant AS last_message_from,
			       (lm.media_id IS NOT NULL) AS has_media, COALESCE(lm.media_type, '') AS media_type
			FROM filtered_entries e
			%s
			JOIN LATERAL (
				SELECT cm.text, cm.message_type, cm.created_at, cm.from_participant, cm.media_id, cm.media_type
				FROM conversation_messages cm
				WHERE cm.entry_id = e.entry_id AND cm.entry_type = ? AND cm.deleted_at IS NULL
				ORDER BY cm.created_at DESC, cm.id DESC LIMIT 1
			) lm ON true
		),
		top_entries AS (
			SELECT * FROM entries_with_last_msg
			ORDER BY last_message_at DESC LIMIT ? OFFSET ?
		),
		unread_counts AS (
			SELECT cm2.entry_id, COUNT(*) AS cnt
			FROM conversation_messages cm2
			WHERE cm2.entry_type = ? AND cm2.read = false
			  AND cm2.message_type IN %s AND cm2.deleted_at IS NULL
			  AND cm2.entry_id IN (SELECT entry_id FROM top_entries)
			GROUP BY cm2.entry_id
		)
		SELECT te.entry_id::text, ? AS entry_type, te.lead_id, te.business_phone_id,
		       te.campaign_id, te.campaign_name,
		       te.agent_id, te.workflow_id, te.agent_responses_enabled, te.workflow_enabled,
		       COALESCE(uc.cnt, 0) AS unread_count,
		       te.last_message_text, te.last_message_type, te.last_message_at,
		       te.last_message_from, te.has_media, te.media_type
		FROM top_entries te
		LEFT JOIN unread_counts uc ON uc.entry_id = te.entry_id
		ORDER BY te.last_message_at DESC
	`, entryCTE, leadField, bphoneField, campaignIDField, campaignNameField, aiFields, entryJoin, inboundTypesSQL())

	queryArgs := append(append([]interface{}{}, cteArgs...), et, pageSize, offset, et, et)

	var results []msgResult
	if err := r.db.Raw(query, queryArgs...).Scan(&results).Error; err != nil {
		return nil, 0, fmt.Errorf("error getting entries with messages: %w", err)
	}

	if len(results) == 0 {
		return nil, totalCount, nil
	}

	entries := make([]conversation.EntryWithLastMessage, 0, len(results))
	for _, r := range results {
		entries = append(entries, conversation.EntryWithLastMessage{
			EntryID:               r.EntryID,
			EntryType:             shared.EntryType(r.EntryType),
			CampaignID:            r.CampaignID,
			CampaignName:          r.CampaignName,
			LeadID:                r.LeadID,
			BusinessPhoneID:       r.BusinessPhoneID,
			UnreadCount:           r.UnreadCount,
			LastMessageText:       r.LastMessageText,
			LastMessageType:       conversation.MessageType(r.LastMessageType),
			LastMessageAt:         r.LastMessageAt,
			LastMessageFrom:       r.LastMessageFrom,
			HasMedia:              r.HasMedia,
			MediaType:             conversation.MediaType(r.MediaType),
			AgentID:               r.AgentID,
			WorkflowID:            r.WorkflowID,
			AgentResponsesEnabled: r.AgentEnabled,
			WorkflowEnabled:       r.WorkflowEnabled,
		})
	}

	return entries, totalCount, nil
}

func (r *repository) SearchEntriesWithMessages(input conversation.SearchEntriesInput) ([]conversation.EntryWithLastMessage, int64, error) {

	useWorkspaceFilter := input.WorkspaceID != "" && input.CampaignID == ""
	useCampaignFilter := input.CampaignID != ""

	if useWorkspaceFilter {
		return r.searchEntriesByWorkspace(input)
	}

	var filteredIDs []string
	if !useCampaignFilter {
		if len(input.EntryIDs) == 0 {
			return nil, 0, nil
		}

		filteredIDs = make([]string, 0, len(input.EntryIDs))
		for _, id := range input.EntryIDs {
			if id != "" {
				filteredIDs = append(filteredIDs, id)
			}
		}
		if len(filteredIDs) == 0 {
			return nil, 0, nil
		}
	}

	et := string(input.EntryType)

	var entryJoin, leadJoin, campaignFilter string
	var campaignFilterArgs []interface{}
	switch input.EntryType {
	case "whatsapp":
		entryJoin = `JOIN whatsapp_campaign_entries wce ON wce.id = entries.entry_id AND wce.deleted_at IS NULL
		             JOIN whatsapp_campaigns wcamp ON wcamp.id = wce.campaign_id`
		leadJoin = `JOIN leads l ON l.id = wce.lead_id AND l.deleted_at IS NULL`
		if useCampaignFilter {
			departmentFilter, departmentArgs := departmentScopeClause("wc_f.department_id", "wce_f.id", input.DepartmentIDs, input.RestrictDepartments, input.AssigneeOverrideUserID)
			campaignFilter = fmt.Sprintf(`cm.entry_id IN (
				SELECT wce_f.id FROM whatsapp_campaign_entries wce_f
				JOIN whatsapp_campaigns wc_f ON wc_f.id = wce_f.campaign_id
				WHERE wce_f.campaign_id = ? AND wce_f.deleted_at IS NULL%s
			)`, departmentFilter)
			campaignFilterArgs = append([]interface{}{input.CampaignID}, departmentArgs...)
		}

	case shared.EntryTypeInstagram:
		entryJoin = `JOIN instagram_conversations igc ON igc.id = entries.entry_id AND igc.deleted_at IS NULL
		             JOIN instagram_accounts iga ON iga.id = igc.ig_account_id`
		// Projected into the same shape the lead join exposes, so every
		// downstream predicate that references l.name / l.number keeps working.
		// An Instagram contact has no phone number, so the @handle takes that
		// slot — it is what an operator actually searches by.
		leadJoin = instagramContactJoin
		if useCampaignFilter {
			// For Instagram the "campaign" filter is the account filter: the
			// account is the container.
			departmentFilter, departmentArgs := departmentScopeClause("iga_f.department_id", "igc_f.id", input.DepartmentIDs, input.RestrictDepartments, input.AssigneeOverrideUserID)
			campaignFilter = fmt.Sprintf(`cm.entry_id IN (
				SELECT igc_f.id::text FROM instagram_conversations igc_f
				JOIN instagram_accounts iga_f ON iga_f.id = igc_f.ig_account_id
				WHERE igc_f.ig_account_id = ? AND igc_f.deleted_at IS NULL%s
			)`, departmentFilter)
			campaignFilterArgs = append([]interface{}{input.CampaignID}, departmentArgs...)
		}

	default:
		return nil, 0, fmt.Errorf("invalid entry type: %s", et)
	}

	var cteConditions []string
	var cteArgs []interface{}

	if useCampaignFilter {
		cteConditions = []string{campaignFilter, "cm.entry_type = ?", "cm.entry_id IS NOT NULL", "cm.deleted_at IS NULL"}
		cteArgs = append(campaignFilterArgs, et)
	} else {
		cteConditions = []string{"cm.entry_id = ANY(?::uuid[])", "cm.entry_type = ?", "cm.entry_id IS NOT NULL", "cm.deleted_at IS NULL"}
		cteArgs = []interface{}{pq.Array(filteredIDs), et}
	}

	if len(input.StageIDs) > 0 && input.StageWorkspaceID != "" && !useCampaignFilter {
		if len(input.StageIDs) == 1 && input.StageIDs[0] == "__unstaged__" {

			cteConditions = append(cteConditions, "cm.entry_id NOT IN (SELECT entry_id FROM entry_stages WHERE workspace_id = ? AND deleted_at IS NULL)")
			cteArgs = append(cteArgs, input.StageWorkspaceID)
		} else {
			cteConditions = append(cteConditions, "cm.entry_id IN (SELECT entry_id FROM entry_stages WHERE stage_id = ANY(?) AND workspace_id = ? AND deleted_at IS NULL)")
			cteArgs = append(cteArgs, pq.Array(input.StageIDs), input.StageWorkspaceID)
		}
	}

	if input.Channel != "" {
		cteConditions = append(cteConditions, "cm.channel = ?")
		cteArgs = append(cteArgs, input.Channel)
	}

	if input.DateFrom != nil {
		cteConditions = append(cteConditions, "cm.created_at >= ?")
		cteArgs = append(cteArgs, *input.DateFrom)
	}
	if input.DateTo != nil {
		cteConditions = append(cteConditions, "cm.created_at <= ?")
		cteArgs = append(cteArgs, *input.DateTo)
	}

	if input.AssignedUserID != "" {
		cteConditions = append(cteConditions, "(NOT EXISTS (SELECT 1 FROM inbox_assignments ia WHERE ia.entry_id = cm.entry_id) OR EXISTS (SELECT 1 FROM inbox_assignments ia2 WHERE ia2.entry_id = cm.entry_id AND ia2.assigned_user_id = ?))")
		cteArgs = append(cteArgs, input.AssignedUserID)
	}

	// User-facing "filter by responsible" (strict, unlike the permission scope above):
	// exactly this owner, or the no-responsible pool. ANDs with every other scope.
	if input.ResponsibleUnassigned {
		cteConditions = append(cteConditions, "NOT EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = cm.entry_id)")
	} else if input.ResponsibleUserID != "" {
		cteConditions = append(cteConditions, "EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = cm.entry_id AND iaf.assigned_user_id = ?)")
		cteArgs = append(cteArgs, input.ResponsibleUserID)
	}

	baseQuery := fmt.Sprintf(`
		WITH entries AS (
			SELECT DISTINCT entry_id
			FROM conversation_messages cm
			WHERE %s
		)
		SELECT entries.entry_id
		FROM entries
		%s
		%s
	`, joinConditions(cteConditions), entryJoin, leadJoin)

	whereConditions := []string{}
	whereArgs := []interface{}{}

	// The default (empty) filter is the "active" view: everything EXCEPT
	// finalized. A concrete status filters to exactly that status. IS DISTINCT
	// FROM keeps entries that have no status yet (never finalized) visible.
	{
		var statusCol string
		switch input.EntryType {
		case shared.EntryTypeWhatsApp:
			statusCol = "wce.conversation_status"
		case shared.EntryTypeInstagram:
			statusCol = "igc.conversation_status"
		}
		if statusCol != "" {
			if input.ConversationStatus == "" {
				whereConditions = append(whereConditions, statusCol+" IS DISTINCT FROM 'finished'")
			} else {
				whereConditions = append(whereConditions, statusCol+" = ?")
				whereArgs = append(whereArgs, string(input.ConversationStatus))
			}
		}
	}

	if useCampaignFilter && len(input.StageIDs) > 0 && input.StageWorkspaceID != "" {
		if len(input.StageIDs) == 1 && input.StageIDs[0] == "__unstaged__" {
			whereConditions = append(whereConditions, "NOT EXISTS (SELECT 1 FROM entry_stages et WHERE et.entry_id = entries.entry_id AND et.workspace_id = ? AND et.deleted_at IS NULL)")
			whereArgs = append(whereArgs, input.StageWorkspaceID)
		} else {
			whereConditions = append(whereConditions, "entries.entry_id IN (SELECT entry_id FROM entry_stages WHERE stage_id = ANY(?) AND workspace_id = ? AND deleted_at IS NULL)")
			whereArgs = append(whereArgs, pq.Array(input.StageIDs), input.StageWorkspaceID)
		}
	}

	if input.Query != "" {
		queryPattern := "%%" + input.Query + "%%"
		if input.MessageSearch == "" {
			if useCampaignFilter {
				whereConditions = append(whereConditions, `(LOWER(l.name) LIKE LOWER(?) OR l.number LIKE ? OR entries.entry_id IN (
					SELECT DISTINCT cm_q.entry_id
					FROM conversation_messages cm_q
					WHERE cm_q.entry_type = ?
					  AND cm_q.deleted_at IS NULL AND LOWER(cm_q.text) LIKE LOWER(?)
				))`)
				whereArgs = append(whereArgs, queryPattern, queryPattern, et, queryPattern)
			} else {
				whereConditions = append(whereConditions, `(LOWER(l.name) LIKE LOWER(?) OR l.number LIKE ? OR entries.entry_id IN (
					SELECT DISTINCT cm_q.entry_id
					FROM conversation_messages cm_q
					WHERE cm_q.entry_id = ANY(?::uuid[]) AND cm_q.entry_type = ?
					  AND cm_q.deleted_at IS NULL AND LOWER(cm_q.text) LIKE LOWER(?)
				))`)
				whereArgs = append(whereArgs, queryPattern, queryPattern, pq.Array(filteredIDs), et, queryPattern)
			}
		} else {
			whereConditions = append(whereConditions, "(LOWER(l.name) LIKE LOWER(?) OR l.number LIKE ?)")
			whereArgs = append(whereArgs, queryPattern, queryPattern)
		}
	}

	if input.MessageSearch != "" {
		if useCampaignFilter {
			whereConditions = append(whereConditions, `entries.entry_id IN (
				SELECT DISTINCT cm2.entry_id
				FROM conversation_messages cm2
				WHERE cm2.entry_type = ?
				  AND cm2.deleted_at IS NULL AND LOWER(cm2.text) LIKE LOWER(?)
			)`)
			whereArgs = append(whereArgs, et, "%%"+input.MessageSearch+"%%")
		} else {
			whereConditions = append(whereConditions, `entries.entry_id IN (
				SELECT DISTINCT cm2.entry_id
				FROM conversation_messages cm2
				WHERE cm2.entry_id = ANY(?::uuid[]) AND cm2.entry_type = ?
				  AND cm2.deleted_at IS NULL AND LOWER(cm2.text) LIKE LOWER(?)
			)`)
			whereArgs = append(whereArgs, pq.Array(filteredIDs), et, "%%"+input.MessageSearch+"%%")
		}
	}

	if input.MinMessageCount != nil || input.MaxMessageCount != nil {
		countSubquery := `(SELECT COUNT(*) FROM conversation_messages cm3 WHERE cm3.entry_id = entries.entry_id AND cm3.entry_type = ? AND cm3.deleted_at IS NULL)`
		if input.MinMessageCount != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("%s >= ?", countSubquery))
			whereArgs = append(whereArgs, et, *input.MinMessageCount)
		}
		if input.MaxMessageCount != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("%s <= ?", countSubquery))
			whereArgs = append(whereArgs, et, *input.MaxMessageCount)
		}
	}

	if input.HasUnread != nil {
		unreadSubquery := `(SELECT COUNT(*) FROM conversation_messages cm4 WHERE cm4.entry_id = entries.entry_id AND cm4.entry_type = ? AND cm4.read = false AND cm4.message_type IN ` + inboundTypesSQL() + ` AND cm4.deleted_at IS NULL)`
		if *input.HasUnread {
			whereConditions = append(whereConditions, fmt.Sprintf("%s > 0", unreadSubquery))
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("%s = 0", unreadSubquery))
		}
		whereArgs = append(whereArgs, et)
	}

	if input.WindowOpen != nil {
		var windowSubquery string
		switch input.EntryType {
		case "whatsapp":
			windowSubquery = `SELECT wce_w.id FROM whatsapp_campaign_entries wce_w
				JOIN whatsapp_campaigns wc_w ON wc_w.id = wce_w.campaign_id
				JOIN lead_message_windows lmw ON lmw.lead_id = wce_w.lead_id AND lmw.business_phone_id = wc_w.business_phone_id
				WHERE wce_w.deleted_at IS NULL AND lmw.last_message_at > NOW() - INTERVAL '24 hours'`
		case "instagram":
			// Instagram's window is anchored on the conversation row itself
			// rather than on a separate window table: last_customer_message_at
			// is the sliding 24h anchor and resets on every inbound message.
			windowSubquery = `SELECT igc_w.id::text FROM instagram_conversations igc_w
				WHERE igc_w.deleted_at IS NULL
				  AND igc_w.last_customer_message_at IS NOT NULL
				  AND igc_w.last_customer_message_at > NOW() - INTERVAL '24 hours'`
		}
		if *input.WindowOpen {
			whereConditions = append(whereConditions, fmt.Sprintf("entries.entry_id IN (%s)", windowSubquery))
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("entries.entry_id NOT IN (%s)", windowSubquery))
		}
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + joinConditions(whereConditions)
	}

	countQuery := baseQuery + " " + whereClause
	countQuery = "SELECT COUNT(*) FROM (" + countQuery + ") AS matching_entries"

	allArgs := append(cteArgs, whereArgs...)
	var totalCount int64
	if err := r.db.Raw(countQuery, allArgs...).Scan(&totalCount).Error; err != nil {
		return nil, 0, fmt.Errorf("error counting search results: %w", err)
	}

	if totalCount == 0 {
		return nil, 0, nil
	}

	page := input.Page
	pageSize := input.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	sortDirection := "DESC"
	if input.SortOrder == "asc" {
		sortDirection = "ASC"
	}

	var matchingQuery string
	var allArgsForMatch []interface{}

	if useCampaignFilter {

		matchingQuery = baseQuery + " " + whereClause
		matchingQuery = fmt.Sprintf(`
			SELECT matching.entry_id FROM (%s) AS matching
			JOIN LATERAL (
				SELECT cm_l.created_at
				FROM conversation_messages cm_l
				WHERE cm_l.entry_id = matching.entry_id AND cm_l.entry_type = ? AND cm_l.deleted_at IS NULL
				ORDER BY cm_l.created_at DESC LIMIT 1
			) AS lm ON true
			ORDER BY lm.created_at %s
			LIMIT ? OFFSET ?
		`, matchingQuery, sortDirection)
		allArgsForMatch = append(allArgs, et, pageSize, offset)
	} else {

		matchingQuery = baseQuery + " " + whereClause
		matchingQuery = fmt.Sprintf(`
			SELECT matching.entry_id FROM (%s) AS matching
			JOIN LATERAL (
				SELECT cm_l.created_at
				FROM conversation_messages cm_l
				WHERE cm_l.entry_id = matching.entry_id AND cm_l.entry_type = ? AND cm_l.deleted_at IS NULL
				ORDER BY cm_l.created_at DESC LIMIT 1
			) AS lm ON true
			ORDER BY lm.created_at %s
			LIMIT ? OFFSET ?
		`, matchingQuery, sortDirection)
		allArgsForMatch = append(allArgs, et, pageSize, offset)
	}

	type idResult struct {
		EntryID string `gorm:"column:entry_id"`
	}
	var matchedIDs []idResult
	if err := r.db.Raw(matchingQuery, allArgsForMatch...).Scan(&matchedIDs).Error; err != nil {
		return nil, 0, fmt.Errorf("error getting search results: %w", err)
	}

	if len(matchedIDs) == 0 {
		return nil, totalCount, nil
	}

	resultIDs := make([]string, len(matchedIDs))
	for i, r := range matchedIDs {
		resultIDs[i] = r.EntryID
	}

	entries, _, err := r.GetEntriesWithMessages("", resultIDs, input.EntryType, 1, len(resultIDs), "")
	if err != nil {
		return nil, 0, fmt.Errorf("error getting entry details: %w", err)
	}

	searchTerm := input.MessageSearch
	if searchTerm == "" && input.Query != "" {
		searchTerm = input.Query
	}

	if searchTerm != "" {
		type matchResult struct {
			MessageID  string    `gorm:"column:message_id"`
			EntryID    string    `gorm:"column:entry_id"`
			Text       string    `gorm:"column:text"`
			From       string    `gorm:"column:from_participant"`
			MsgType    string    `gorm:"column:message_type"`
			Channel    string    `gorm:"column:channel"`
			CreatedAt  time.Time `gorm:"column:created_at"`
			Position   int64     `gorm:"column:position"`
			MatchRank  int       `gorm:"column:match_rank"`
			TotalMatch int       `gorm:"column:total_matches"`
		}

		var matches []matchResult
		matchQuery := `
			WITH ranked AS (
				SELECT
					cm.id AS message_id,
					cm.entry_id,
					cm.text,
					cm.from_participant,
					cm.message_type,
					cm.channel,
					cm.created_at,
					(SELECT COUNT(*) FROM conversation_messages cm2
					 WHERE cm2.entry_id = cm.entry_id AND cm2.entry_type = cm.entry_type
					   AND cm2.deleted_at IS NULL AND cm2.created_at > cm.created_at) AS position,
					ROW_NUMBER() OVER (PARTITION BY cm.entry_id ORDER BY cm.created_at DESC) AS match_rank,
					COUNT(*) OVER (PARTITION BY cm.entry_id) AS total_matches
				FROM conversation_messages cm
				WHERE cm.entry_id = ANY(?::uuid[]) AND cm.entry_type = ?
				  AND cm.deleted_at IS NULL AND LOWER(cm.text) LIKE LOWER(?)
			)
			SELECT message_id, entry_id, text, from_participant, message_type, channel, created_at,
			       position, match_rank, total_matches
			FROM ranked
			WHERE match_rank <= 3
			ORDER BY entry_id, created_at DESC
		`
		r.db.Raw(matchQuery, pq.Array(resultIDs), et, "%%"+searchTerm+"%%").Scan(&matches)

		matchMap := make(map[string][]conversation.MatchedMessageResult)
		totalMap := make(map[string]int)
		for _, m := range matches {
			matchMap[m.EntryID] = append(matchMap[m.EntryID], conversation.MatchedMessageResult{
				MessageID:    m.MessageID,
				EntryID:      m.EntryID,
				Text:         m.Text,
				From:         m.From,
				MsgType:      conversation.MessageType(m.MsgType),
				Channel:      conversation.MessageChannel(m.Channel),
				CreatedAt:    m.CreatedAt,
				Position:     m.Position,
				TotalMatches: m.TotalMatch,
			})
			totalMap[m.EntryID] = m.TotalMatch
		}

		for i := range entries {
			if mm, ok := matchMap[entries[i].EntryID]; ok {
				entries[i].MatchedMessages = mm
				entries[i].TotalMatches = totalMap[entries[i].EntryID]
			}
		}
	}

	return entries, totalCount, nil
}

func (r *repository) searchEntriesByWorkspace(input conversation.SearchEntriesInput) ([]conversation.EntryWithLastMessage, int64, error) {
	wsID := input.WorkspaceID

	var entryParts []string
	var entryArgs []interface{}

	if input.EntryType == "" || input.EntryType == shared.EntryTypeWhatsApp {
		waCampaignTypeFilter := ""
		var waCampaignTypeArgs []interface{}
		convStatusFilter := ""
		var convStatusArgs []interface{}
		departmentFilter, departmentArgs := departmentScopeClause("wc.department_id", "wce.id", input.DepartmentIDs, input.RestrictDepartments, input.AssigneeOverrideUserID)
		if input.WhatsAppCampaignType == "standard" || input.WhatsAppCampaignType == "organic" {
			waCampaignTypeFilter = ` AND wc.type = ?`
			waCampaignTypeArgs = append(waCampaignTypeArgs, input.WhatsAppCampaignType)
		}
		if input.ConversationStatus == "" {
			convStatusFilter = ` AND wce.conversation_status IS DISTINCT FROM 'finished'`
		} else {
			convStatusFilter = ` AND wce.conversation_status = ?`
			convStatusArgs = append(convStatusArgs, string(input.ConversationStatus))
		}
		part := `SELECT wce.id AS entry_id, 'whatsapp'::text AS entry_type, wce.lead_id AS lead_id,
		         COALESCE(wc.business_phone_id::text, '') AS business_phone_id,
		         wce.last_message_at AS lm_created_at
		         FROM whatsapp_campaign_entries wce
		         JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.workspace_id = ?` + waCampaignTypeFilter + `
		         WHERE wce.deleted_at IS NULL AND wce.last_message_at IS NOT NULL` + convStatusFilter + departmentFilter
		entryParts = append(entryParts, part)
		entryArgs = append(entryArgs, wsID)
		entryArgs = append(entryArgs, waCampaignTypeArgs...)
		entryArgs = append(entryArgs, convStatusArgs...)
		entryArgs = append(entryArgs, departmentArgs...)
	}

	includeSupport := (input.EntryType == "" || input.EntryType == shared.EntryTypeSupport) && input.WhatsAppCampaignType == "" && input.ConversationStatus == ""
	if includeSupport {
		part := `SELECT se.id AS entry_id, 'support'::text AS entry_type, NULL::uuid AS lead_id,
		         '' AS business_phone_id,
		         se.last_message_at AS lm_created_at
		         FROM support_entries se
		         JOIN support_inboxes si ON si.id = se.inbox_id AND si.workspace_id = ?
		         WHERE se.deleted_at IS NULL AND se.last_message_at IS NOT NULL`
		entryParts = append(entryParts, part)
		entryArgs = append(entryArgs, wsID)
	}

	if len(entryParts) == 0 {
		return nil, 0, nil
	}

	entryCTE := strings.Join(entryParts, " UNION ALL ")

	whereConditions := []string{}
	whereArgs := []interface{}{}

	if input.Query != "" {
		queryPattern := "%%" + input.Query + "%%"
		whereConditions = append(whereConditions, `(
			LOWER(COALESCE(l.name, '')) LIKE LOWER(?) OR COALESCE(l.number, '') LIKE ?
			OR ae.entry_id IN (
				SELECT DISTINCT cm_q.entry_id
				FROM conversation_messages cm_q
				WHERE cm_q.entry_id = ae.entry_id AND cm_q.entry_type = ae.entry_type
				  AND cm_q.deleted_at IS NULL AND LOWER(cm_q.text) LIKE LOWER(?)
			)
		)`)
		whereArgs = append(whereArgs, queryPattern, queryPattern, queryPattern)
	}

	if len(input.StageIDs) > 0 && input.StageWorkspaceID != "" {
		if len(input.StageIDs) == 1 && input.StageIDs[0] == "__unstaged__" {

			whereConditions = append(whereConditions, "ae.entry_id NOT IN (SELECT entry_id FROM entry_stages WHERE workspace_id = ? AND deleted_at IS NULL)")
			whereArgs = append(whereArgs, input.StageWorkspaceID)
		} else {

			whereConditions = append(whereConditions, "ae.entry_id IN (SELECT entry_id FROM entry_stages WHERE stage_id = ANY(?) AND workspace_id = ? AND deleted_at IS NULL)")
			whereArgs = append(whereArgs, pq.Array(input.StageIDs), input.StageWorkspaceID)
		}
	}

	if input.HasUnread != nil {
		unreadSub := `(SELECT COUNT(*) FROM conversation_messages cm4 WHERE cm4.entry_id = ae.entry_id AND cm4.entry_type = ae.entry_type AND cm4.read = false AND cm4.message_type IN ` + inboundTypesSQL() + ` AND cm4.deleted_at IS NULL)`
		if *input.HasUnread {
			whereConditions = append(whereConditions, fmt.Sprintf("%s > 0", unreadSub))
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("%s = 0", unreadSub))
		}
	}

	if input.Channel != "" {
		whereConditions = append(whereConditions, `EXISTS (
			SELECT 1 FROM conversation_messages cm_ch
			WHERE cm_ch.entry_id = ae.entry_id AND cm_ch.entry_type = ae.entry_type
			  AND cm_ch.channel = ? AND cm_ch.deleted_at IS NULL
		)`)
		whereArgs = append(whereArgs, input.Channel)
	}

	if input.MessageSearch != "" {
		whereConditions = append(whereConditions, `EXISTS (
			SELECT 1 FROM conversation_messages cm_ms
			WHERE cm_ms.entry_id = ae.entry_id AND cm_ms.entry_type = ae.entry_type
			  AND cm_ms.deleted_at IS NULL AND LOWER(cm_ms.text) LIKE LOWER(?)
		)`)
		whereArgs = append(whereArgs, "%%"+input.MessageSearch+"%%")
	}

	if input.AssignedUserID != "" {
		whereConditions = append(whereConditions, "(NOT EXISTS (SELECT 1 FROM inbox_assignments ia WHERE ia.entry_id = ae.entry_id) OR EXISTS (SELECT 1 FROM inbox_assignments ia2 WHERE ia2.entry_id = ae.entry_id AND ia2.assigned_user_id = ?))")
		whereArgs = append(whereArgs, input.AssignedUserID)
	}

	// User-facing "filter by responsible" (strict): exactly this owner, or the
	// no-responsible pool. ANDs with the permission scope + department scope above.
	if input.ResponsibleUnassigned {
		whereConditions = append(whereConditions, "NOT EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = ae.entry_id)")
	} else if input.ResponsibleUserID != "" {
		whereConditions = append(whereConditions, "EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = ae.entry_id AND iaf.assigned_user_id = ?)")
		whereArgs = append(whereArgs, input.ResponsibleUserID)
	}

	if input.WindowOpen != nil {
		windowSubquery := `(
			SELECT wce_w.id FROM whatsapp_campaign_entries wce_w
			JOIN whatsapp_campaigns wc_w ON wc_w.id = wce_w.campaign_id
			JOIN lead_message_windows lmw ON lmw.lead_id = wce_w.lead_id AND lmw.business_phone_id = wc_w.business_phone_id
			WHERE wce_w.deleted_at IS NULL AND lmw.last_message_at > NOW() - INTERVAL '24 hours'
		)`
		if *input.WindowOpen {
			whereConditions = append(whereConditions, fmt.Sprintf("ae.entry_id IN %s", windowSubquery))
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("ae.entry_id NOT IN %s", windowSubquery))
		}
	}

	if input.MinMessageCount != nil || input.MaxMessageCount != nil {
		countSub := `(SELECT COUNT(*) FROM conversation_messages cm3 WHERE cm3.entry_id = ae.entry_id AND cm3.entry_type = ae.entry_type AND cm3.deleted_at IS NULL)`
		if input.MinMessageCount != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("%s >= ?", countSub))
			whereArgs = append(whereArgs, *input.MinMessageCount)
		}
		if input.MaxMessageCount != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("%s <= ?", countSub))
			whereArgs = append(whereArgs, *input.MaxMessageCount)
		}
	}

	if input.DateFrom != nil {
		// Qualify with the all_entries alias: lm_created_at is a column of that CTE
		// (projected from the entry's stored last_message_at), so ae.lm_created_at is
		// addressable here. An unqualified SELECT-list alias would crash with 42703.
		whereConditions = append(whereConditions, "ae.lm_created_at >= ?")
		whereArgs = append(whereArgs, *input.DateFrom)
	}
	if input.DateTo != nil {
		whereConditions = append(whereConditions, "ae.lm_created_at <= ?")
		whereArgs = append(whereArgs, *input.DateTo)
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + joinConditions(whereConditions)
	}

	leadsJoin := ""
	if input.Query != "" {
		leadsJoin = "LEFT JOIN leads l ON l.id = ae.lead_id AND l.deleted_at IS NULL"
	}
	// Last activity comes from the entry's stored last_message_at (projected as
	// lm_created_at by each CTE branch, which also filters out entries that have no
	// messages). This replaces a JOIN LATERAL that ran once per entry and forced
	// every entry in the workspace to be materialized — and the multi-GB entries
	// table sequentially scanned — before pagination. Semantics are unchanged: the
	// LATERAL was an inner join, so entries without messages were already excluded.
	baseCTE := fmt.Sprintf(`
		WITH all_entries AS (%s),
		entries_with_msg AS (
			SELECT ae.entry_id, ae.entry_type, ae.lead_id, ae.business_phone_id,
			       ae.lm_created_at AS lm_created_at
			FROM all_entries ae
			%s
			%s
		)
	`, entryCTE, leadsJoin, whereClause)

	countSQL := baseCTE + " SELECT COUNT(*) FROM entries_with_msg"
	allCountArgs := append(append([]interface{}{}, entryArgs...), whereArgs...)
	var totalCount int64
	if err := r.db.Raw(countSQL, allCountArgs...).Scan(&totalCount).Error; err != nil {
		return nil, 0, fmt.Errorf("error counting workspace search results: %w", err)
	}
	if totalCount == 0 {
		return nil, 0, nil
	}

	page := input.Page
	pageSize := input.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	sortDirection := "DESC"
	if input.SortOrder == "asc" {
		sortDirection = "ASC"
	}

	matchSQL := fmt.Sprintf(`%s
		SELECT entry_id, entry_type FROM entries_with_msg
		ORDER BY lm_created_at %s
		LIMIT ? OFFSET ?
	`, baseCTE, sortDirection)

	type idResult struct {
		EntryID   string `gorm:"column:entry_id"`
		EntryType string `gorm:"column:entry_type"`
	}

	matchArgs := append(append([]interface{}{}, entryArgs...), whereArgs...)
	matchArgs = append(matchArgs, pageSize, offset)
	var matchedIDs []idResult
	if err := r.db.Raw(matchSQL, matchArgs...).Scan(&matchedIDs).Error; err != nil {
		return nil, 0, fmt.Errorf("error searching workspace entries: %w", err)
	}

	if len(matchedIDs) == 0 {
		return nil, totalCount, nil
	}

	voiceIDs := make([]string, 0)
	whatsappIDs := make([]string, 0)
	instagramIDs := make([]string, 0)
	for _, m := range matchedIDs {
		switch m.EntryType {
		case "voice":
			voiceIDs = append(voiceIDs, m.EntryID)
		case "whatsapp":
			whatsappIDs = append(whatsappIDs, m.EntryID)
		case "instagram":
			instagramIDs = append(instagramIDs, m.EntryID)
		}
	}

	var allEntries []conversation.EntryWithLastMessage

	if len(instagramIDs) > 0 {
		igEntries, _, err := r.GetEntriesWithMessages("", instagramIDs, shared.EntryTypeInstagram, 1, len(instagramIDs), "")
		if err == nil {
			allEntries = append(allEntries, igEntries...)
		}
	}

	if len(whatsappIDs) > 0 {
		waEntries, _, err := r.GetEntriesWithMessages("", whatsappIDs, shared.EntryTypeWhatsApp, 1, len(whatsappIDs), "")
		if err == nil {
			allEntries = append(allEntries, waEntries...)
		}
	}

	sortDesc := input.SortOrder != "asc"
	sort.Slice(allEntries, func(i, j int) bool {
		if sortDesc {
			return allEntries[i].LastMessageAt.After(allEntries[j].LastMessageAt)
		}
		return allEntries[i].LastMessageAt.Before(allEntries[j].LastMessageAt)
	})

	return allEntries, totalCount, nil
}

// SearchEntriesByFilter is the additive, workspace-global read path for the
// decoupled CRM board and list view. It reuses the CTE + department-scope +
// pagination/sort shape of searchEntriesByWorkspace, but builds its WHERE from a
// reusable crmfilter.Filter compiled by the infra FilterCompiler instead of the
// hand-written per-field predicates, and reads the last-message timestamp from the
// entry's stored last_message_at rather than a per-entry JOIN LATERAL.
// searchEntriesByWorkspace is intentionally left untouched (it still serves the
// legacy inbox path).
func (r *repository) SearchEntriesByFilter(input conversation.SearchByFilterInput) ([]conversation.EntryWithLastMessage, int64, error) {
	wsID := input.WorkspaceID
	if wsID == "" {
		return nil, 0, fmt.Errorf("SearchEntriesByFilter: workspace id is required")
	}

	// Compile the reusable filter first so an invalid filter fails fast (and so
	// we can plan the leads join only when a full-text query predicate is used).
	desc := crmfiltersql.NewConversationDescriptor()
	desc.WorkspaceID = wsID
	// Last activity now comes from the entry's stored last_message_at, projected by
	// the all_entries CTE, so it is addressable both in WHERE and ORDER BY here.
	desc.LastActivityExpr = "ae.lm_created_at"
	whereSQL, whereArgs, err := crmfiltersql.Compile(input.Filter, desc, 1)
	if err != nil {
		return nil, 0, fmt.Errorf("SearchEntriesByFilter: %w", err)
	}

	// All three entry families, workspace-scoped, each surfacing the columns the
	// compiler reads on the outer row: conversation_status, campaign_id,
	// created_at, updated_at (plus entry_type used as the conversation "source").
	var entryParts []string
	var entryArgs []interface{}

	{
		departmentFilter, departmentArgs := departmentScopeClause("wc.department_id", "wce.id", input.DepartmentIDs, input.RestrictDepartments, input.AssigneeOverrideUserID)
		selfFilter, selfArgs := assignedSelfClause("wce.id", input.AssignedUserID)
		entryParts = append(entryParts, `SELECT wce.id AS entry_id, 'whatsapp'::text AS entry_type, wce.lead_id AS lead_id,
		         COALESCE(wc.business_phone_id::text, '') AS business_phone_id,
		         wce.conversation_status AS conversation_status, wce.campaign_id::text AS campaign_id,
		         wce.created_at AS created_at, wce.updated_at AS updated_at,
		         wce.last_message_at AS lm_created_at
		         FROM whatsapp_campaign_entries wce
		         JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.workspace_id = ?
		         WHERE wce.deleted_at IS NULL AND wce.last_message_at IS NOT NULL`+departmentFilter+selfFilter)
		entryArgs = append(entryArgs, wsID)
		entryArgs = append(entryArgs, departmentArgs...)
		entryArgs = append(entryArgs, selfArgs...)
	}
	{
		// Support entries have no campaign / conversation_status; NULL/'' keep them
		// visible under status filters (IS DISTINCT FROM keeps '' rows).
		selfFilter, selfArgs := assignedSelfClause("se.id", input.AssignedUserID)
		entryParts = append(entryParts, `SELECT se.id AS entry_id, 'support'::text AS entry_type, NULL::uuid AS lead_id,
		         ''::text AS business_phone_id,
		         ''::text AS conversation_status, NULL::text AS campaign_id,
		         se.created_at AS created_at, se.updated_at AS updated_at,
		         se.last_message_at AS lm_created_at
		         FROM support_entries se
		         JOIN support_inboxes si ON si.id = se.inbox_id AND si.workspace_id = ?
		         WHERE se.deleted_at IS NULL AND se.last_message_at IS NOT NULL`+selfFilter)
		entryArgs = append(entryArgs, wsID)
		entryArgs = append(entryArgs, selfArgs...)
	}

	entryCTE := strings.Join(entryParts, " UNION ALL ")

	whereClause := ""
	if strings.TrimSpace(whereSQL) != "" {
		whereClause = "WHERE " + whereSQL
	}

	// Always join the lead row so the board card can render a title (lead name /
	// number) and so a full-text query predicate (which references l.name /
	// l.number) resolves. Mirrors the searchEntriesByWorkspace leads join
	// exactly (same table/alias, LEFT JOIN, COALESCE null handling).
	leadsJoin := "LEFT JOIN leads l ON l.id = ae.lead_id AND l.deleted_at IS NULL"

	// The last message timestamp is read from the entry's stored last_message_at
	// (projected as lm_created_at by each CTE branch, which also filters out entries
	// that have none). This replaces a JOIN LATERAL over conversation_messages that
	// ran once per entry: it forced every entry in the workspace to be materialized
	// — and the multi-GB entries table sequentially scanned — before pagination,
	// costing seconds on large workspaces. Semantics are unchanged: the LATERAL was
	// an inner join, so entries without messages were already excluded, exactly as
	// the IS NOT NULL filter does now.
	baseCTE := fmt.Sprintf(`
		WITH all_entries AS (%s),
		entries_with_msg AS (
			SELECT ae.entry_id, ae.entry_type, ae.lead_id, ae.business_phone_id,
			       ae.created_at AS created_at,
			       COALESCE(l.name, '') AS lead_name, COALESCE(l.number, '') AS lead_number,
			       ae.lm_created_at AS lm_created_at
			FROM all_entries ae
			%s
			%s
		)
	`, entryCTE, leadsJoin, whereClause)

	countSQL := baseCTE + " SELECT COUNT(*) FROM entries_with_msg"
	allCountArgs := append(append([]interface{}{}, entryArgs...), whereArgs...)
	var totalCount int64
	if err := r.db.Raw(countSQL, allCountArgs...).Scan(&totalCount).Error; err != nil {
		return nil, 0, fmt.Errorf("error counting filtered board results: %w", err)
	}
	if totalCount == 0 {
		return nil, 0, nil
	}

	page := input.Page
	pageSize := input.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	sortDirection := "DESC"
	if input.SortOrder == "asc" {
		sortDirection = "ASC"
	}
	sortColumn := "lm_created_at"
	if input.SortField == "created" {
		sortColumn = "created_at"
	}

	matchSQL := fmt.Sprintf(`%s
		SELECT entry_id, entry_type, lead_name, lead_number FROM entries_with_msg
		ORDER BY %s %s
		LIMIT ? OFFSET ?
	`, baseCTE, sortColumn, sortDirection)

	type idResult struct {
		EntryID    string `gorm:"column:entry_id"`
		EntryType  string `gorm:"column:entry_type"`
		LeadName   string `gorm:"column:lead_name"`
		LeadNumber string `gorm:"column:lead_number"`
	}
	matchArgs := append(append([]interface{}{}, entryArgs...), whereArgs...)
	matchArgs = append(matchArgs, pageSize, offset)
	var matchedIDs []idResult
	if err := r.db.Raw(matchSQL, matchArgs...).Scan(&matchedIDs).Error; err != nil {
		return nil, 0, fmt.Errorf("error searching filtered board entries: %w", err)
	}
	if len(matchedIDs) == 0 {
		return nil, totalCount, nil
	}

	// Hydrate per entry type (reusing GetEntriesWithMessages), then restore the
	// DB result order so the requested sort is preserved across the type split.
	order := make(map[string]int, len(matchedIDs))
	type leadIdentity struct{ name, number string }
	leadByEntry := make(map[string]leadIdentity, len(matchedIDs))
	voiceIDs := make([]string, 0)
	whatsappIDs := make([]string, 0)
	instagramIDs := make([]string, 0)
	for i, m := range matchedIDs {
		order[m.EntryID] = i
		leadByEntry[m.EntryID] = leadIdentity{name: m.LeadName, number: m.LeadNumber}
		switch m.EntryType {
		case "voice":
			voiceIDs = append(voiceIDs, m.EntryID)
		case "whatsapp":
			whatsappIDs = append(whatsappIDs, m.EntryID)
		case "instagram":
			instagramIDs = append(instagramIDs, m.EntryID)
		}
	}

	var allEntries []conversation.EntryWithLastMessage
	if len(instagramIDs) > 0 {
		igEntries, _, err := r.GetEntriesWithMessages("", instagramIDs, shared.EntryTypeInstagram, 1, len(instagramIDs), "")
		if err == nil {
			allEntries = append(allEntries, igEntries...)
		}
	}
	if len(whatsappIDs) > 0 {
		waEntries, _, err := r.GetEntriesWithMessages("", whatsappIDs, shared.EntryTypeWhatsApp, 1, len(whatsappIDs), "")
		if err == nil {
			allEntries = append(allEntries, waEntries...)
		}
	}

	// GetEntriesWithMessages hydrates everything except the lead name/number
	// (it never selected them); restore them from the filtered board query so
	// the board card can render a title.
	for i := range allEntries {
		if lead, ok := leadByEntry[allEntries[i].EntryID]; ok {
			allEntries[i].LeadName = lead.name
			allEntries[i].LeadNumber = lead.number
		}
	}

	sort.Slice(allEntries, func(i, j int) bool {
		return order[allEntries[i].EntryID] < order[allEntries[j].EntryID]
	})

	return allEntries, totalCount, nil
}

func joinConditions(conditions []string) string {
	result := ""
	for i, c := range conditions {
		if i > 0 {
			result += " AND "
		}
		result += c
	}
	return result
}

func (r *repository) GetEntryLastMessage(entryID string, entryType shared.EntryType) (*conversation.EntryWithLastMessage, error) {
	if entryID == "" {
		return nil, nil
	}

	var leadID, businessPhoneID, campaignID, campaignName string
	type entryInfo struct {
		LeadID          string `gorm:"column:lead_id"`
		BusinessPhoneID string `gorm:"column:business_phone_id"`
		CampaignID      string `gorm:"column:campaign_id"`
		CampaignName    string `gorm:"column:campaign_name"`
		AgentID         string `gorm:"column:agent_id"`
		WorkflowID      string `gorm:"column:workflow_id"`
		AgentEnabled    bool   `gorm:"column:agent_responses_enabled"`
		WorkflowEnabled bool   `gorm:"column:workflow_enabled"`
	}
	var info entryInfo

	switch entryType {
	case "whatsapp":
		r.db.Raw(`
			SELECT wce.lead_id::text AS lead_id, COALESCE(wc.business_phone_id::text, '') AS business_phone_id,
			       wc.id::text AS campaign_id, wc.name AS campaign_name,
			       COALESCE(wc.agent_id::text, '') AS agent_id, COALESCE(wc.workflow_id::text, '') AS workflow_id,
			       wc.enable_agent_responses AS agent_responses_enabled, wc.enable_workflow AS workflow_enabled
			FROM whatsapp_campaign_entries wce
			JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id
			WHERE wce.id = ?::uuid AND wce.deleted_at IS NULL
			LIMIT 1
		`, entryID).Scan(&info)
	case "instagram":
		// The account is the container, so it fills the campaign slot; the
		// @handle fills the lead-number slot.
		r.db.Raw(`
			SELECT igc.contact_id::text AS lead_id, COALESCE(igc.ig_account_id::text, '') AS business_phone_id,
			       iga.id::text AS campaign_id, iga.username AS campaign_name,
			       COALESCE(iga.agent_id::text, '') AS agent_id, COALESCE(iga.workflow_id::text, '') AS workflow_id,
			       iga.enable_agent_responses AS agent_responses_enabled, iga.enable_workflow AS workflow_enabled
			FROM instagram_conversations igc
			JOIN instagram_accounts iga ON iga.id = igc.ig_account_id
			WHERE igc.id = ?::uuid AND igc.deleted_at IS NULL
			LIMIT 1
		`, entryID).Scan(&info)
	}
	leadID = info.LeadID
	businessPhoneID = info.BusinessPhoneID
	campaignID = info.CampaignID
	campaignName = info.CampaignName

	type msgResult struct {
		EntryID         string    `gorm:"column:entry_id"`
		EntryType       string    `gorm:"column:entry_type"`
		UnreadCount     int64     `gorm:"column:unread_count"`
		LastMessageText string    `gorm:"column:last_message_text"`
		LastMessageType string    `gorm:"column:last_message_type"`
		LastMessageAt   time.Time `gorm:"column:last_message_at"`
		LastMessageFrom string    `gorm:"column:last_message_from"`
		HasMedia        bool      `gorm:"column:has_media"`
		MediaType       string    `gorm:"column:media_type"`
	}

	var result msgResult

	query := fmt.Sprintf(`
		SELECT
			m.entry_id::text AS entry_id,
			m.entry_type AS entry_type,
			(SELECT COUNT(*) FROM conversation_messages cm2
			 WHERE cm2.entry_id = m.entry_id AND cm2.entry_type = m.entry_type
			   AND cm2.read = false AND cm2.message_type IN %s
			   AND cm2.deleted_at IS NULL) AS unread_count,
			m.text AS last_message_text,
			m.message_type AS last_message_type,
			m.created_at AS last_message_at,
			m.from_participant AS last_message_from,
			(m.media_id IS NOT NULL) AS has_media,
			COALESCE(m.media_type, '') AS media_type
		FROM conversation_messages m
		WHERE m.entry_id = ?::uuid AND m.entry_type = ?
		ORDER BY m.created_at DESC
		LIMIT 1
	`, inboundTypesSQL())

	err := r.db.Raw(query, entryID, string(entryType)).Scan(&result).Error

	if err != nil {
		return nil, err
	}

	if result.EntryID == "" {
		return nil, nil
	}

	return &conversation.EntryWithLastMessage{
		EntryID:               result.EntryID,
		EntryType:             shared.EntryType(result.EntryType),
		CampaignID:            campaignID,
		CampaignName:          campaignName,
		LeadID:                leadID,
		BusinessPhoneID:       businessPhoneID,
		UnreadCount:           result.UnreadCount,
		LastMessageText:       result.LastMessageText,
		LastMessageType:       conversation.MessageType(result.LastMessageType),
		LastMessageAt:         result.LastMessageAt,
		LastMessageFrom:       result.LastMessageFrom,
		HasMedia:              result.HasMedia,
		MediaType:             conversation.MediaType(result.MediaType),
		AgentID:               info.AgentID,
		WorkflowID:            info.WorkflowID,
		AgentResponsesEnabled: info.AgentEnabled,
		WorkflowEnabled:       info.WorkflowEnabled,
	}, nil
}

func (r *repository) CountByEntry(entryID string, entryType shared.EntryType) (int64, error) {
	var count int64
	err := r.db.Raw(`
		SELECT COUNT(*)
		FROM conversation_messages
		WHERE entry_id = ?::uuid AND entry_type = ? AND deleted_at IS NULL
	`, entryID, string(entryType)).Scan(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *repository) SearchMessagesByEntry(input conversation.SearchMessagesByEntryInput) ([]*conversation.Message, int64, error) {
	if input.EntryID == "" || input.Query == "" {
		return nil, 0, nil
	}

	page := input.Page
	if page <= 0 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize
	queryPattern := "%%" + input.Query + "%%"

	var totalCount int64
	if err := r.db.Raw(`
		SELECT COUNT(*)
		FROM conversation_messages
		WHERE entry_id = ?::uuid AND entry_type = ? AND deleted_at IS NULL
		  AND LOWER(text) LIKE LOWER(?)
	`, input.EntryID, string(input.EntryType), queryPattern).Scan(&totalCount).Error; err != nil {
		return nil, 0, fmt.Errorf("error counting search results: %w", err)
	}

	if totalCount == 0 {
		return nil, 0, nil
	}

	var dbMessages []schema.ConversationMessage
	if err := r.db.Raw(`
		SELECT *
		FROM conversation_messages
		WHERE entry_id = ?::uuid AND entry_type = ? AND deleted_at IS NULL
		  AND LOWER(text) LIKE LOWER(?)
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, input.EntryID, string(input.EntryType), queryPattern, pageSize, offset).Scan(&dbMessages).Error; err != nil {
		return nil, 0, fmt.Errorf("error searching messages: %w", err)
	}

	messages := make([]*conversation.Message, 0, len(dbMessages))
	for i := range dbMessages {
		messages = append(messages, mapSchemaToDomain(&dbMessages[i]))
	}

	return messages, totalCount, nil
}

func (r *repository) GetByWhatsAppMessageID(wamid string) (*conversation.Message, error) {
	var dbMessage schema.ConversationMessage
	if err := r.db.Where("whatsapp_message_id = ?", wamid).First(&dbMessage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, conversation.ErrMessageNotFound
		}
		return nil, err
	}
	return mapSchemaToDomain(&dbMessage), nil
}

// GetByExternalMessageID is the channel-agnostic dedup lookup. It is scoped by
// entry type because a provider message id is only unique within its own
// channel, matching the partial unique index
// ux_cm_entry_type_external_msgid.
func (r *repository) GetByExternalMessageID(entryType shared.EntryType, externalID string) (*conversation.Message, error) {
	var dbMessage schema.ConversationMessage
	if err := r.db.
		Where("entry_type = ? AND external_message_id = ?", string(entryType), externalID).
		First(&dbMessage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, conversation.ErrMessageNotFound
		}
		return nil, err
	}
	return mapSchemaToDomain(&dbMessage), nil
}

func (r *repository) UpdateDeliveryStatus(wamid string, status conversation.DeliveryStatus) error {
	result := r.db.Model(&schema.ConversationMessage{}).
		Where("whatsapp_message_id = ?", wamid).
		Updates(map[string]interface{}{
			"delivery_status": string(status),
			"updated_at":      time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	return nil
}
