package conversation_repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	crmfiltersql "vozko/infra/repositories/crmfilter"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
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

func entryTableForLastMessage(entryType string) string {
	if ch, ok := channelQueryFor(shared.EntryType(entryType)); ok {
		return ch.EntryTable
	}
	return ""
}

func (r *repository) touchEntryLastMessageAt(entryID, entryType string, at time.Time) {
	r.touchEntryMessageClocks(entryID, entryType, conversation.SentBy{}, at)
}

func (r *repository) touchEntryMessageClocks(entryID, entryType string, sentBy conversation.SentBy, at time.Time) {
	table := entryTableForLastMessage(entryType)
	if table == "" || entryID == "" || at.IsZero() {
		return
	}

	setCustomer := sentBy.IsContact()
	setAgent := sentBy.FromTheBusiness()

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
	if !message.SentBy.Valid() {
		return conversation.ErrMessageSenderRequired
	}

	dbMessage := mapDomainToSchema(message)
	if err := r.db.Create(dbMessage).Error; err != nil {
		if database.IsUniqueViolation(err) {
			if claimed, claimErr := r.ClaimExternalEcho(message); claimErr == nil && claimed {
				return nil
			}
		}
		return err
	}
	r.touchEntryMessageClocks(dbMessage.EntryID, dbMessage.EntryType, message.SentBy, dbMessage.CreatedAt)
	return nil
}

func (r *repository) ClaimExternalEcho(message *conversation.Message) (bool, error) {
	if message == nil || message.ExternalMessageID == nil || !message.SentBy.Claims(conversation.SentExternally()) {
		return false, nil
	}
	result := r.db.Model(&schema.ConversationMessage{}).
		Where("entry_type = ? AND entry_id = ? AND external_message_id = ? AND sender_kind = ?",
			string(message.EntryType), message.EntryID, *message.ExternalMessageID, string(conversation.SenderExternal)).
		Updates(map[string]interface{}{"sender_kind": string(message.SentBy.Kind()), "sender_id": message.SentBy.ID()})
	return result.RowsAffected > 0, result.Error
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
		Where(database.SentByContactSQL(""))

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
		Where(database.SentByContactSQL("")).
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
		Where(database.SentByContactSQL("")).
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
		Direction:         string(message.ResolvedDirection()),
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
		SentVia:           string(message.SentVia),
		SenderKind:        string(message.SentBy.Kind()),
		SenderID:          message.SentBy.ID(),
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
		Direction:         conversation.MessageHistoryDirection(message.Direction),
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
		SentVia:           conversation.MessageTransport(message.SentVia),
		SentBy:            conversation.RestoreSentBy(message.SenderKind, message.SenderID),
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
	return r.getEntriesWithMessages(campaignID, conversation.ContainerKindAccount, entryIDs, entryType, page, pageSize, assignedUserID)
}

func (r *repository) GetEntriesWithMessagesForContainer(campaignID string, containerKind conversation.ContainerKind, entryIDs []string, entryType shared.EntryType, page, pageSize int, assignedUserID string) ([]conversation.EntryWithLastMessage, int64, error) {
	return r.getEntriesWithMessages(campaignID, containerKind, entryIDs, entryType, page, pageSize, assignedUserID)
}

func (r *repository) getEntriesWithMessages(campaignID string, containerKind conversation.ContainerKind, entryIDs []string, entryType shared.EntryType, page, pageSize int, assignedUserID string) ([]conversation.EntryWithLastMessage, int64, error) {
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

	ch, ok := channelQueryFor(entryType)
	if !ok {
		return nil, 0, fmt.Errorf("invalid entry type: %s", et)
	}

	var entryCTE string
	var cteArgs []interface{}

	leadField := contactRefText(ch.EntryType)
	bphoneField := ch.AccountIDField
	campaignIDField := ch.ContainerIDField
	campaignNameField := ch.ContainerNameField
	aiFields := ch.AutomationFields
	if ch.AutomationColumn != "" {
		aiFields += ", " + ch.AutomationColumn + " AS automation_enabled"
	} else {
		aiFields += ", NULL::boolean AS automation_enabled"
	}
	if ch.StatusColumn != "" {
		aiFields += ", " + ch.StatusColumn + " AS conversation_status"
	} else {
		aiFields += ", ''::text AS conversation_status"
	}
	aiFields += ", " + ch.closeFieldsSQL()
	entryJoin := ch.entryJoinOn("e.entry_id")

	if useCampaignFilter {
		entryCTE = ch.cteForKind(containerKind, assignmentFilterFor)
		cteArgs = append([]interface{}{campaignID}, assignmentArgs...)
	} else {
		entryCTE = `SELECT u.entry_id FROM unnest(?::uuid[]) AS u(entry_id) WHERE 1=1` + assignmentFilterFor("u.entry_id")
		cteArgs = append([]interface{}{pq.Array(filteredIDs)}, assignmentArgs...)
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
		EntryID         string     `gorm:"column:entry_id"`
		EntryType       string     `gorm:"column:entry_type"`
		LeadID          string     `gorm:"column:lead_id"`
		BusinessPhoneID string     `gorm:"column:business_phone_id"`
		CampaignID      string     `gorm:"column:campaign_id"`
		CampaignName    string     `gorm:"column:campaign_name"`
		UnreadCount     int64      `gorm:"column:unread_count"`
		LastMessageText string     `gorm:"column:last_message_text"`
		LastMessageType string     `gorm:"column:last_message_type"`
		LastMessageAt   time.Time  `gorm:"column:last_message_at"`
		LastMessageFrom string     `gorm:"column:last_message_from"`
		HasMedia        bool       `gorm:"column:has_media"`
		MediaType       string     `gorm:"column:media_type"`
		AgentID         string     `gorm:"column:agent_id"`
		WorkflowID      string     `gorm:"column:workflow_id"`
		AgentEnabled    bool       `gorm:"column:agent_responses_enabled"`
		WorkflowEnabled bool       `gorm:"column:workflow_enabled"`
		AutomationOn    *bool      `gorm:"column:automation_enabled"`
		ConvStatus      string     `gorm:"column:conversation_status"`
		CloseSource     string     `gorm:"column:close_source"`
		CloseReason     string     `gorm:"column:close_reason"`
		CloseOutcome    string     `gorm:"column:close_outcome"`
		ClosedAt        *time.Time `gorm:"column:closed_at"`
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
			  AND %s AND cm2.deleted_at IS NULL
			  AND cm2.entry_id IN (SELECT entry_id FROM top_entries)
			GROUP BY cm2.entry_id
		)
		SELECT te.entry_id::text, ? AS entry_type, te.lead_id, te.business_phone_id,
		       te.campaign_id, te.campaign_name,
		       te.agent_id, te.workflow_id, te.agent_responses_enabled, te.workflow_enabled,
		       -- Projected by the CTE above for every channel, and dropped here
		       -- until now: this outer list is explicit, so a column added to
		       -- the CTE reaches the scan only if it is also named HERE. That is
		       -- how the inbox ended up rendering "Nova" over conversations the
		       -- database had as ongoing, and why the automation override read
		       -- back as enabled whatever an operator had set.
		       te.automation_enabled, te.conversation_status,
		       te.close_source, te.close_reason, te.close_outcome, te.closed_at,
		       COALESCE(uc.cnt, 0) AS unread_count,
		       te.last_message_text, te.last_message_type, te.last_message_at,
		       te.last_message_from, te.has_media, te.media_type
		FROM top_entries te
		LEFT JOIN unread_counts uc ON uc.entry_id = te.entry_id
		ORDER BY te.last_message_at DESC
	`, entryCTE, leadField, bphoneField, campaignIDField, campaignNameField, aiFields, entryJoin, database.SentByContactSQL("cm2"))

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
			AutomationEnabled:     r.AutomationOn,
			ConversationStatus:    r.ConvStatus,
			Close: conversation.CloseRecord{
				Source:   conversation.CloseSource(r.CloseSource),
				Reason:   conversation.CloseReason(r.CloseReason),
				Outcome:  r.CloseOutcome,
				ClosedAt: r.ClosedAt,
			},
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

	ch, ok := channelQueryFor(input.EntryType)
	if !ok {
		return nil, 0, fmt.Errorf("invalid entry type: %s", et)
	}

	var campaignFilter string
	var campaignFilterArgs []interface{}

	entryJoin := ch.entryJoinOn("entries.entry_id")
	leadJoin := ch.ContactJoin

	if useCampaignFilter {
		var departmentArgs []interface{}
		campaignFilter, departmentArgs = ch.filterForKind(input.ContainerKind,
			func(column, entryCol string) (string, []interface{}) {
				return departmentScopeClause(column, entryCol,
					input.DepartmentIDs, input.RestrictDepartments, input.AssigneeOverrideUserID)
			})
		campaignFilterArgs = append([]interface{}{input.CampaignID}, departmentArgs...)
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

	if input.ResponsibleUnassigned {
		cteConditions = append(cteConditions, "NOT EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = cm.entry_id)")
	} else if input.ResponsibleKind != "" {
		cteConditions = append(cteConditions, "EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = cm.entry_id AND iaf.assignee_kind = ?)")
		cteArgs = append(cteArgs, string(input.ResponsibleKind))
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

	{
		statusCol := ch.StatusColumn
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
		unreadSubquery := `(SELECT COUNT(*) FROM conversation_messages cm4 WHERE cm4.entry_id = entries.entry_id AND cm4.entry_type = ? AND cm4.read = false AND ` + database.SentByContactSQL("cm4") + ` AND cm4.deleted_at IS NULL)`
		if *input.HasUnread {
			whereConditions = append(whereConditions, fmt.Sprintf("%s > 0", unreadSubquery))
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("%s = 0", unreadSubquery))
		}
		whereArgs = append(whereArgs, et)
	}

	if input.WindowOpen != nil && ch.WindowSubquery != "" {
		if *input.WindowOpen {
			whereConditions = append(whereConditions, fmt.Sprintf("entries.entry_id IN (%s)", ch.WindowSubquery))
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("entries.entry_id NOT IN (%s)", ch.WindowSubquery))
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

	entryCTESQL, entryCTEArgs := buildEntryUnion(entrySourceScope{
		EntryType:              input.EntryType,
		WhatsAppCampaignType:   input.WhatsAppCampaignType,
		ConversationStatus:     string(input.ConversationStatus),
		ExcludeFinished:        true,
		DepartmentIDs:          input.DepartmentIDs,
		RestrictDepartments:    input.RestrictDepartments,
		AssigneeOverrideUserID: input.AssigneeOverrideUserID,
	}, wsID, entrySource.inboxSelect)
	if entryCTESQL != "" {
		entryParts = append(entryParts, entryCTESQL)
		entryArgs = append(entryArgs, entryCTEArgs...)
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
		unreadSub := `(SELECT COUNT(*) FROM conversation_messages cm4 WHERE cm4.entry_id = ae.entry_id AND cm4.entry_type = ae.entry_type AND cm4.read = false AND ` + database.SentByContactSQL("cm4") + ` AND cm4.deleted_at IS NULL)`
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

	if input.ResponsibleUnassigned {
		whereConditions = append(whereConditions, "NOT EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = ae.entry_id)")
	} else if input.ResponsibleKind != "" {
		whereConditions = append(whereConditions, "EXISTS (SELECT 1 FROM inbox_assignments iaf WHERE iaf.entry_id = ae.entry_id AND iaf.assignee_kind = ?)")
		whereArgs = append(whereArgs, string(input.ResponsibleKind))
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

	refs := make([]entryRef, 0, len(matchedIDs))
	for _, m := range matchedIDs {
		refs = append(refs, entryRef{ID: m.EntryID, Type: shared.EntryType(m.EntryType)})
	}

	var allEntries []conversation.EntryWithLastMessage
	for _, batch := range hydrationBatches(refs) {
		entries, _, err := r.GetEntriesWithMessages("", batch.IDs, batch.EntryType, 1, len(batch.IDs), "")
		if err == nil {
			allEntries = append(allEntries, entries...)
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

func (r *repository) SearchEntriesByFilter(input conversation.SearchByFilterInput) ([]conversation.EntryWithLastMessage, int64, error) {
	wsID := input.WorkspaceID
	if wsID == "" {
		return nil, 0, fmt.Errorf("SearchEntriesByFilter: workspace id is required")
	}

	desc := crmfiltersql.NewConversationDescriptor()
	desc.WorkspaceID = wsID
	desc.LastActivityExpr = "ae.lm_created_at"
	whereSQL, whereArgs, err := crmfiltersql.Compile(input.Filter, desc, 1)
	if err != nil {
		return nil, 0, fmt.Errorf("SearchEntriesByFilter: %w", err)
	}

	var entryParts []string
	var entryArgs []interface{}

	boardCTESQL, boardCTEArgs := buildEntryUnion(entrySourceScope{
		WhatsAppCampaignType:   input.WhatsAppCampaignType,
		DepartmentIDs:          input.DepartmentIDs,
		RestrictDepartments:    input.RestrictDepartments,
		AssigneeOverrideUserID: input.AssigneeOverrideUserID,
		AssignedUserID:         input.AssignedUserID,
	}, wsID, entrySource.boardSelect)
	if boardCTESQL != "" {
		entryParts = append(entryParts, boardCTESQL)
		entryArgs = append(entryArgs, boardCTEArgs...)
	}

	entryCTE := strings.Join(entryParts, " UNION ALL ")

	whereClause := ""
	if strings.TrimSpace(whereSQL) != "" {
		whereClause = "WHERE " + whereSQL
	}

	leadsJoin := "LEFT JOIN leads l ON l.id = ae.lead_id AND l.deleted_at IS NULL"

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

	order := make(map[string]int, len(matchedIDs))
	type leadIdentity struct{ name, number string }
	leadByEntry := make(map[string]leadIdentity, len(matchedIDs))
	refs := make([]entryRef, 0, len(matchedIDs))
	for i, m := range matchedIDs {
		order[m.EntryID] = i
		leadByEntry[m.EntryID] = leadIdentity{name: m.LeadName, number: m.LeadNumber}
		refs = append(refs, entryRef{ID: m.EntryID, Type: shared.EntryType(m.EntryType)})
	}

	var allEntries []conversation.EntryWithLastMessage
	for _, batch := range hydrationBatches(refs) {
		entries, _, err := r.GetEntriesWithMessages("", batch.IDs, batch.EntryType, 1, len(batch.IDs), "")
		if err == nil {
			allEntries = append(allEntries, entries...)
		}
	}

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
		LeadID          string     `gorm:"column:lead_id"`
		BusinessPhoneID string     `gorm:"column:business_phone_id"`
		CampaignID      string     `gorm:"column:campaign_id"`
		CampaignName    string     `gorm:"column:campaign_name"`
		AgentID         string     `gorm:"column:agent_id"`
		WorkflowID      string     `gorm:"column:workflow_id"`
		AgentEnabled    bool       `gorm:"column:agent_responses_enabled"`
		WorkflowEnabled bool       `gorm:"column:workflow_enabled"`
		AutomationOn    *bool      `gorm:"column:automation_enabled"`
		ConvStatus      string     `gorm:"column:conversation_status"`
		CloseSource     string     `gorm:"column:close_source"`
		CloseReason     string     `gorm:"column:close_reason"`
		CloseOutcome    string     `gorm:"column:close_outcome"`
		ClosedAt        *time.Time `gorm:"column:closed_at"`
	}
	var info entryInfo

	if ch, ok := channelQueryFor(shared.EntryType(entryType)); ok {
		r.db.Raw(ch.entryInfoSQL(), entryID).Scan(&info)
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
			   AND cm2.read = false AND %s
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
	`, database.SentByContactSQL("cm2"))

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
		AutomationEnabled:     info.AutomationOn,
		ConversationStatus:    info.ConvStatus,
		Close: conversation.CloseRecord{
			Source:   conversation.CloseSource(info.CloseSource),
			Reason:   conversation.CloseReason(info.CloseReason),
			Outcome:  info.CloseOutcome,
			ClosedAt: info.ClosedAt,
		},
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

func (r *repository) CountInboundByEntry(entryID string, entryType shared.EntryType) (int64, error) {
	var count int64
	err := r.db.Raw(`
		SELECT COUNT(*)
		FROM conversation_messages
		WHERE entry_id = ?::uuid AND entry_type = ? AND deleted_at IS NULL
		  AND `+database.SentByContactSQL("")+`
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

func (r *repository) GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*conversation.Message, error) {
	var dbMessage schema.ConversationMessage
	if err := r.db.
		Where("entry_type = ? AND entry_id = ? AND external_message_id = ?", string(entryType), entryID, externalID).
		First(&dbMessage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, conversation.ErrMessageNotFound
		}
		return nil, err
	}
	return mapSchemaToDomain(&dbMessage), nil
}

func (r *repository) UpdateDeliveryStatus(wamid string, status conversation.DeliveryStatus) error {
	return r.UpdateDeliveryStatusWithReason(wamid, status, 0, "")
}

func (r *repository) UpdateDeliveryStatusWithReason(
	wamid string,
	status conversation.DeliveryStatus,
	errorCode int,
	errorMessage string,
) error {
	return r.UpdateDeliveryReceipt(wamid, conversation.DeliveryReceipt{
		Status:       status,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
	})
}

func (r *repository) UpdateDeliveryReceipt(wamid string, receipt conversation.DeliveryReceipt) error {
	status := receipt.Status
	errorCode := receipt.ErrorCode
	errorMessage := receipt.ErrorMessage

	updates := map[string]interface{}{
		"delivery_status": string(status),
		"updated_at":      time.Now().UTC(),
	}

	if receipt.HasPricing() {
		if category := receipt.Pricing.NormalizedCategory(); category != "" {
			updates["meta_pricing_category"] = category
			billable := receipt.Pricing.Billable
			updates["meta_pricing_billable"] = &billable
			if model := strings.TrimSpace(receipt.Pricing.Model); model != "" {
				updates["meta_pricing_model"] = model
			}
		}
		if origin := receipt.NormalizedOrigin(); origin != "" {
			updates["meta_conversation_origin"] = origin
		}
	}

	if errorCode != 0 || strings.TrimSpace(errorMessage) != "" {
		if len(errorMessage) > 500 {
			errorMessage = errorMessage[:500]
		}
		payload, err := json.Marshal(map[string]interface{}{
			"delivery_error": map[string]interface{}{
				"code":    errorCode,
				"message": errorMessage,
			},
		})
		if err == nil {
			updates["metadata"] = gorm.Expr(
				"COALESCE(metadata, '{}'::jsonb) || ?::jsonb", string(payload))
		}
	}

	result := r.db.Model(&schema.ConversationMessage{}).
		Where("whatsapp_message_id = ? OR external_message_id = ?", wamid, wamid).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	return nil
}
