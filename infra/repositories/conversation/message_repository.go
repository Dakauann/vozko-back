package conversation_repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
	"vozko/infra/repositories/departmentscope"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func departmentScopeClause(deptColumn, entryIDColumn string, departmentIDs []string, restrict bool, assigneeUserID string) (string, []interface{}) {
	return departmentscope.Clause(deptColumn, entryIDColumn, departmentIDs, restrict, assigneeUserID)
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

func (r *repository) touchEntryMessageClocks(entryID, entryType string, sentBy conversation.SentBy, at time.Time) {
	table := entryTableForLastMessage(entryType)
	if table == "" || entryID == "" || at.IsZero() {
		return
	}
	sql, args := messageClockUpdate(table, entryID, entryType, sentBy, at)
	if err := r.db.Exec(sql, args...).Error; err != nil {
		log.Printf("[conversation] message clock bump failed (%s %s): %v", entryType, entryID, err)
	}
}

func messageClockUpdate(table, entryID, entryType string, sentBy conversation.SentBy, at time.Time) (string, []interface{}) {
	sql := fmt.Sprintf(`UPDATE %s SET last_message_at = CASE
			WHEN last_message_at IS NULL OR last_message_at < ? THEN ? ELSE last_message_at END`, table)
	args := []interface{}{at, at}
	if sentBy.IsContact() {
		sql += `, last_customer_message_at = CASE
			WHEN last_customer_message_at IS NULL OR last_customer_message_at < ? THEN ? ELSE last_customer_message_at END`
		args = append(args, at, at)
	}
	if sentBy.FromTheBusiness() {
		sql += `, last_agent_message_at = CASE
			WHEN last_agent_message_at IS NULL OR last_agent_message_at < ? THEN ? ELSE last_agent_message_at END`
		args = append(args, at, at)
	}
	switch {
	case sentBy.IsContact():
		sql += `, awaiting_reply_since = CASE
			WHEN EXISTS (SELECT 1 FROM conversation_messages rw
				WHERE rw.entry_id = ? AND rw.entry_type = ? AND rw.deleted_at IS NULL
				  AND ` + sqlexpr.SentAsReply("rw") + ` AND rw.created_at >= ?) THEN awaiting_reply_since
			WHEN awaiting_reply_since IS NULL OR awaiting_reply_since > ? THEN ?
			ELSE awaiting_reply_since END`
		args = append(args, entryID, entryType, at, at, at)
	case sentBy.AnswersTheCustomer():
		sql += `, awaiting_reply_since = CASE
			WHEN awaiting_reply_since IS NULL OR awaiting_reply_since > ? THEN awaiting_reply_since
			ELSE (SELECT MIN(cw.created_at) FROM conversation_messages cw
				WHERE cw.entry_id = ? AND cw.entry_type = ? AND cw.deleted_at IS NULL
				  AND ` + sqlexpr.SentByContact("cw") + ` AND cw.created_at > ?) END`
		args = append(args, at, entryID, entryType, at)
	}
	sql += ` WHERE id = ?`
	return sql, append(args, entryID)
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
	for column, value := range messageStateUpdates(message) {
		updates[column] = value
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

func messageStateUpdates(message *conversation.Message) map[string]interface{} {
	updates := map[string]interface{}{}
	if len(message.Metadata) > 0 {
		updates["metadata"] = []byte(message.Metadata)
	}
	if message.Transcript != "" {
		updates["transcript"] = message.Transcript
	}
	if message.EditedAt != nil {
		updates["edited_at"] = *message.EditedAt
	}
	if message.RetractedAt != nil {
		updates["retracted_at"] = *message.RetractedAt
	}
	return updates
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

	readsForward := input.After != nil && input.Before == nil
	order := "created_at DESC"
	if readsForward {
		order = "created_at ASC"
	}
	if err := query.Order(order).Find(&dbMessages).Error; err != nil {
		return nil, err
	}

	messages := make([]*conversation.Message, 0, len(dbMessages))
	for i := range dbMessages {
		messages = append(messages, mapSchemaToDomain(&dbMessages[i]))
	}
	if readsForward {
		reverseNewestFirst(messages)
	}

	return messages, nil
}

func reverseNewestFirst(messages []*conversation.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

func (r *repository) MarkAsRead(input conversation.MarkAsReadInput) (int64, error) {
	now := time.Now().UTC()

	query := r.db.Model(&schema.ConversationMessage{}).
		Where("entry_id = ? AND entry_type = ?", input.EntryID, string(input.EntryType)).
		Where("read = ?", false).
		Where(sqlexpr.SentByContact(""))

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
		Where(sqlexpr.SentByContact("")).
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
		Where(sqlexpr.SentByContact("")).
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
		ClientRequestID:   nilIfEmpty(message.ClientRequestID),
		Metadata:          message.Metadata,
		Transcript:        message.Transcript,
		EditedAt:          message.EditedAt,
		RetractedAt:       message.RetractedAt,
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
		ClientRequestID:   valueOrEmpty(message.ClientRequestID),
		Metadata:          message.Metadata,
		Transcript:        message.Transcript,
		EditedAt:          message.EditedAt,
		RetractedAt:       message.RetractedAt,
		CreatedAt:         message.CreatedAt,
		UpdatedAt:         message.UpdatedAt,
	}
}

func nilIfEmpty(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
	aiFields += ", " + ch.messageClockFieldsSQL()
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
		LastRetracted   bool       `gorm:"column:last_message_retracted"`
		LastMessageType string     `gorm:"column:last_message_type"`
		LastMessageAt   time.Time  `gorm:"column:last_message_at"`
		LastMessageFrom string     `gorm:"column:last_message_from"`
		SenderKind      string     `gorm:"column:last_message_sender_kind"`
		SenderID        string     `gorm:"column:last_message_sender_id"`
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
		CustomerClock   *time.Time `gorm:"column:last_customer_message_at"`
		AgentClock      *time.Time `gorm:"column:last_agent_message_at"`
		AwaitingSince   *time.Time `gorm:"column:awaiting_reply_since"`
	}

	query := fmt.Sprintf(`
		WITH filtered_entries AS (%s),
		entries_with_last_msg AS (
			SELECT e.entry_id, %s AS lead_id, %s AS business_phone_id,
			       %s AS campaign_id, %s AS campaign_name,
			       %s,
			       lm.text AS last_message_text, (lm.retracted_at IS NOT NULL) AS last_message_retracted, lm.message_type AS last_message_type,
			       lm.created_at AS last_message_at, lm.from_participant AS last_message_from,
			       lm.sender_kind AS last_message_sender_kind, lm.sender_id AS last_message_sender_id,
			       (lm.media_id IS NOT NULL) AS has_media, COALESCE(lm.media_type, '') AS media_type
			FROM filtered_entries e
			%s
			JOIN LATERAL (
				SELECT cm.text, cm.retracted_at, cm.message_type, cm.created_at, cm.from_participant, cm.sender_kind, cm.sender_id, cm.media_id, cm.media_type
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
		       te.last_customer_message_at, te.last_agent_message_at, te.awaiting_reply_since,
		       COALESCE(uc.cnt, 0) AS unread_count,
		       te.last_message_text, te.last_message_retracted, te.last_message_type, te.last_message_at,
		       te.last_message_from, te.last_message_sender_kind, te.last_message_sender_id,
		       te.has_media, te.media_type
		FROM top_entries te
		LEFT JOIN unread_counts uc ON uc.entry_id = te.entry_id
		ORDER BY te.last_message_at DESC
	`, entryCTE, leadField, bphoneField, campaignIDField, campaignNameField, aiFields, entryJoin, sqlexpr.SentByContact("cm2"))

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
			LastMessageRetracted:  r.LastRetracted,
			LastMessageType:       conversation.MessageType(r.LastMessageType),
			LastMessageAt:         r.LastMessageAt,
			LastMessageFrom:       r.LastMessageFrom,
			LastMessageSenderKind: r.SenderKind,
			LastMessageSenderID:   r.SenderID,
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
			LastCustomerMessageAt: r.CustomerClock,
			LastAgentMessageAt:    r.AgentClock,
			AwaitingReplySince:    r.AwaitingSince,
		})
	}

	return entries, totalCount, nil
}

func (r *repository) SearchEntriesByFilter(input conversation.SearchByFilterInput) ([]conversation.EntryWithLastMessage, int64, error) {
	baseCTE, baseArgs, err := filteredEntriesCTE(input)
	if err != nil {
		return nil, 0, fmt.Errorf("SearchEntriesByFilter: %w", err)
	}

	totalCount, err := r.countFilteredEntries(baseCTE, baseArgs)
	if err != nil {
		return nil, 0, err
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
	matchArgs := append(append([]interface{}{}, baseArgs...), pageSize, offset)
	var matchedIDs []idResult
	if err := r.db.Raw(matchSQL, matchArgs...).Scan(&matchedIDs).Error; err != nil {
		return nil, 0, fmt.Errorf("error searching filtered board entries: %w", err)
	}
	if len(matchedIDs) == 0 {
		return nil, totalCount, nil
	}

	type leadIdentity struct{ name, number string }
	leadByEntry := make(map[string]leadIdentity, len(matchedIDs))
	refs := make([]entryRef, 0, len(matchedIDs))
	for _, m := range matchedIDs {
		leadByEntry[m.EntryID] = leadIdentity{name: m.LeadName, number: m.LeadNumber}
		refs = append(refs, entryRef{ID: m.EntryID, Type: shared.EntryType(m.EntryType)})
	}

	allEntries, err := r.hydrateInOrder(refs)
	if err != nil {
		return nil, 0, fmt.Errorf("error hydrating filtered board results: %w", err)
	}

	for i := range allEntries {
		if lead, ok := leadByEntry[allEntries[i].EntryID]; ok {
			allEntries[i].LeadName = lead.name
			allEntries[i].LeadNumber = lead.number
		}
	}

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
		CustomerClock   *time.Time `gorm:"column:last_customer_message_at"`
		AgentClock      *time.Time `gorm:"column:last_agent_message_at"`
		AwaitingSince   *time.Time `gorm:"column:awaiting_reply_since"`
	}
	var info entryInfo

	if ch, ok := channelQueryFor(shared.EntryType(entryType)); ok {
		query, args := ch.entryInfoQuery(entryID)
		r.db.Raw(query, args...).Scan(&info)
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
		LastRetracted   bool      `gorm:"column:last_message_retracted"`
		LastMessageType string    `gorm:"column:last_message_type"`
		LastMessageAt   time.Time `gorm:"column:last_message_at"`
		LastMessageFrom string    `gorm:"column:last_message_from"`
		SenderKind      string    `gorm:"column:last_message_sender_kind"`
		SenderID        string    `gorm:"column:last_message_sender_id"`
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
			(m.retracted_at IS NOT NULL) AS last_message_retracted,
			m.message_type AS last_message_type,
			m.created_at AS last_message_at,
			m.from_participant AS last_message_from,
			m.sender_kind AS last_message_sender_kind,
			m.sender_id AS last_message_sender_id,
			(m.media_id IS NOT NULL) AS has_media,
			COALESCE(m.media_type, '') AS media_type
		FROM conversation_messages m
		WHERE m.entry_id = ?::uuid AND m.entry_type = ? AND m.deleted_at IS NULL
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, sqlexpr.SentByContact("cm2"))

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
		LastMessageRetracted:  result.LastRetracted,
		LastMessageType:       conversation.MessageType(result.LastMessageType),
		LastMessageAt:         result.LastMessageAt,
		LastMessageFrom:       result.LastMessageFrom,
		LastMessageSenderKind: result.SenderKind,
		LastMessageSenderID:   result.SenderID,
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
		LastCustomerMessageAt: info.CustomerClock,
		LastAgentMessageAt:    info.AgentClock,
		AwaitingReplySince:    info.AwaitingSince,
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
		  AND `+sqlexpr.SentByContact("")+`
	`, entryID, string(entryType)).Scan(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

var customerSpokeAfterSQL = `SELECT EXISTS (
	SELECT 1 FROM conversation_messages
	WHERE entry_id = ?::uuid AND entry_type = ? AND deleted_at IS NULL
	  AND ` + sqlexpr.SentByContact("") + ` AND created_at > ?
)`

func (r *repository) CustomerSpokeAfter(entryID string, entryType shared.EntryType, after time.Time) (bool, error) {
	var spoke bool
	if err := r.db.Raw(customerSpokeAfterSQL, entryID, string(entryType), after).Scan(&spoke).Error; err != nil {
		return false, err
	}
	return spoke, nil
}

var _ conversation.CustomerSpeech = (*repository)(nil)

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
		WHERE entry_id = ?::uuid AND entry_type = ?
		  AND `+sqlexpr.MessageTextMatches("")+`
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
		WHERE entry_id = ?::uuid AND entry_type = ?
		  AND `+sqlexpr.MessageTextMatches("")+`
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

func (r *repository) GetByClientRequestID(entryType shared.EntryType, entryID, requestID string) (*conversation.Message, error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, conversation.ErrMessageNotFound
	}
	var dbMessage schema.ConversationMessage
	if err := r.db.
		Where("entry_type = ? AND entry_id = ? AND client_request_id = ?", string(entryType), entryID, requestID).
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
			if pricingType := strings.ToLower(strings.TrimSpace(receipt.Pricing.Type)); pricingType != "" {
				updates["meta_pricing_type"] = pricingType
			}
			if raw := receipt.Pricing.Raw; len(raw) > 0 && json.Valid(raw) {
				updates["meta_pricing"] = gorm.Expr("?::jsonb", string(raw))
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
