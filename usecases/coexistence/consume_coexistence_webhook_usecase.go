package coexistence_usecase

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/coexistence"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/messaging"
	"vozko/domain/shared"
	"vozko/domain/webhook"
	businessphone "vozko/domain/whatsapp/business_phone"
	whatsappcampaign "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type consumeCoexistenceWebhookUseCase struct {
	queueSub          messaging.MessageQueueSub
	businessPhoneRepo businessphone.Repository
	campaignRepo      whatsappcampaign.Repository
	entryRepo         wce.Repository
	messageRepo       conversation.MessageRepository
	leadRepo          lead.Repository
}

func NewConsumeCoexistenceWebhookUseCase(
	queueSub messaging.MessageQueueSub,
	businessPhoneRepo businessphone.Repository,
	campaignRepo whatsappcampaign.Repository,
	entryRepo wce.Repository,
	messageRepo conversation.MessageRepository,
	leadRepo lead.Repository,
) coexistence.ConsumeCoexistenceWebhookUseCase {
	return &consumeCoexistenceWebhookUseCase{
		queueSub:          queueSub,
		businessPhoneRepo: businessPhoneRepo,
		campaignRepo:      campaignRepo,
		entryRepo:         entryRepo,
		messageRepo:       messageRepo,
		leadRepo:          leadRepo,
	}
}

func (uc *consumeCoexistenceWebhookUseCase) Start() error {
	return uc.queueSub.Subscribe(webhook.TopicWhatsAppCoexistence, func(payload []byte, ack messaging.MessageAck) {
		uc.handle(payload, ack)
	})
}

func (uc *consumeCoexistenceWebhookUseCase) handle(raw []byte, ack messaging.MessageAck) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[coexistence-consumer] panic: %v", r)
		}
		if err := ack.Ack(); err != nil {
			log.Printf("[coexistence-consumer] failed to ack: %v", err)
		}
	}()

	field := extractField(raw)
	switch field {
	case "history":
		uc.handleHistory(raw)
	case "smb_app_state_sync":
		uc.handleStateSync(raw)
	case "smb_message_echoes":
		uc.handleMessageEchoes(raw)
	default:
		log.Printf("[coexistence-consumer] unknown coexistence field: %q", field)
	}
}

func (uc *consumeCoexistenceWebhookUseCase) handleHistory(raw []byte) {
	var payload coexistence.HistoryWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("[coexistence-consumer] invalid history payload: %v", err)
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			if change.Field != "history" {
				continue
			}

			phoneNumberID := change.Value.Metadata.PhoneNumberID
			if phoneNumberID == "" {
				log.Printf("[coexistence-consumer] history webhook missing phone_number_id")
				continue
			}

			businessPhone, err := uc.businessPhoneRepo.FindByMetaPhoneNumberID(phoneNumberID)
			if err != nil || businessPhone == nil {
				log.Printf("[coexistence-consumer] no business phone for meta_id=%s: %v", phoneNumberID, err)
				continue
			}

			if strings.TrimSpace(businessPhone.OwnerWorkspaceID) == "" {
				log.Printf("[coexistence-consumer] business phone %s has no workspace", businessPhone.ID)
				continue
			}

			organicCampaign, err := uc.campaignRepo.FindLatestOrganicByBusinessPhone(
				businessPhone.OwnerWorkspaceID, businessPhone.ID)
			if err != nil || organicCampaign == nil {
				log.Printf("[coexistence-consumer] no organic campaign for phone %s in workspace %s",
					businessPhone.ID, businessPhone.OwnerWorkspaceID)
				continue
			}

			for _, chunk := range change.Value.History {

				for _, histErr := range chunk.Errors {
					log.Printf("[coexistence-consumer] history error code=%d: %s — %s",
						histErr.Code, histErr.Title, histErr.Message)
				}

				log.Printf("[coexistence-consumer] processing history chunk phase=%d order=%d progress=%d%% threads=%d",
					chunk.Metadata.Phase, chunk.Metadata.ChunkOrder, chunk.Metadata.Progress, len(chunk.Threads))

				for _, thread := range chunk.Threads {
					uc.processHistoryThread(thread, organicCampaign, businessPhone)
				}
			}
		}
	}
}

func (uc *consumeCoexistenceWebhookUseCase) processHistoryThread(
	thread coexistence.HistoryThread,
	campaign *whatsappcampaign.Campaign,
	businessPhone *businessphone.WhatsAppBusinessPhoneNumber,
) {
	userPhone := thread.ID
	if userPhone == "" {
		return
	}

	leadRecord, _, err := uc.leadRepo.FindOrCreate(campaign.WorkspaceID, userPhone, lead.LeadUpdate{})
	if err != nil {
		log.Printf("[coexistence-consumer] failed to find/create lead for %s: %v", userPhone, err)
		return
	}

	entryRecord, err := uc.entryRepo.FindByCampaignAndLead(campaign.ID, leadRecord.ID)
	if err != nil || entryRecord == nil {
		entryRecord = &wce.WhatsAppCampaignEntry{
			ID:                 uuid.New().String(),
			CampaignID:         campaign.ID,
			LeadID:             leadRecord.ID,
			Status:             wce.SendStatusDelivered,
			ConversationStatus: "finished",
		}
		if createErr := uc.entryRepo.Create(entryRecord); createErr != nil {
			log.Printf("[coexistence-consumer] failed to create entry for lead %s: %v", leadRecord.ID, createErr)
			return
		}
		log.Printf("[coexistence-consumer] created entry %s for lead %s in campaign %s",
			entryRecord.ID, leadRecord.ID, campaign.ID)
	}

	for _, histMsg := range thread.Messages {
		uc.persistHistoryMessage(histMsg, entryRecord.ID, thread.ID, businessPhone)
	}
}

func (uc *consumeCoexistenceWebhookUseCase) persistHistoryMessage(
	histMsg coexistence.HistoryMessage,
	entryID string,
	threadPhone string,
	businessPhone *businessphone.WhatsAppBusinessPhoneNumber,
) {
	msgType, mediaType, text := classifyHistoryMessage(histMsg, businessPhone)
	waMessageID := histMsg.ID
	from := normalizeParticipantNumber(histMsg.From)
	to := normalizeParticipantNumber(histMsg.To)

	if isBusinessOriginatedHistoryMessage(histMsg, businessPhone) {
		if from == "" {
			from = normalizeBusinessPhoneNumber(businessPhone)
		}
		if to == "" {
			to = normalizeParticipantNumber(threadPhone)
		}
	} else if from == "" {
		from = normalizeParticipantNumber(threadPhone)
	}

	var ts time.Time
	if unix, err := strconv.ParseInt(histMsg.Timestamp, 10, 64); err == nil {
		ts = time.Unix(unix, 0).UTC()
	} else {
		ts = time.Now().UTC()
	}

	deliveryStatus := mapHistoryStatus(histMsg.Status)

	msg := &conversation.Message{
		ID:                uuid.New().String(),
		EntryID:           entryID,
		EntryType:         shared.EntryTypeWhatsApp,
		Channel:           conversation.MessageChannelWhatsApp,
		MessageType:       msgType,
		From:              from,
		To:                to,
		Text:              text,
		WhatsAppMessageID: &waMessageID,
		DeliveryStatus:    deliveryStatus,
		MediaType:         mediaType,
		CreatedAt:         ts,
		UpdatedAt:         ts,
	}

	if err := uc.messageRepo.Create(msg); err != nil {
		if !strings.Contains(err.Error(), "duplicate") {
			log.Printf("[coexistence-consumer] failed to persist history message %s: %v", histMsg.ID, err)
		}
	}
}

func (uc *consumeCoexistenceWebhookUseCase) handleStateSync(raw []byte) {
	var payload coexistence.SMBStateSyncWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("[coexistence-consumer] invalid state sync payload: %v", err)
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {

			phoneNumberID := change.Value.Metadata.PhoneNumberID
			if phoneNumberID == "" {
				continue
			}
			businessPhone, err := uc.businessPhoneRepo.FindByMetaPhoneNumberID(phoneNumberID)
			if err != nil || businessPhone == nil {
				log.Printf("[coexistence-consumer] state sync: no business phone for meta_id=%s", phoneNumberID)
				continue
			}
			workspaceID := strings.TrimSpace(businessPhone.OwnerWorkspaceID)
			if workspaceID == "" {
				continue
			}
			for _, item := range change.Value.StateSync {
				if item.Type == "contact" && item.Action == "add" {
					phone := item.Contact.PhoneNumber
					name := item.Contact.FullName
					if phone == "" {
						continue
					}
					_, _, err := uc.leadRepo.FindOrCreate(workspaceID, phone, lead.LeadUpdate{
						Name: name,
					})
					if err != nil {
						log.Printf("[coexistence-consumer] failed to upsert contact %s: %v", phone, err)
					}
				}
			}
		}
	}
}

func (uc *consumeCoexistenceWebhookUseCase) handleMessageEchoes(raw []byte) {
	var payload coexistence.SMBMessageEchoesWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("[coexistence-consumer] invalid message echoes payload: %v", err)
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			phoneNumberID := change.Value.Metadata.PhoneNumberID
			if phoneNumberID == "" {
				continue
			}

			businessPhone, err := uc.businessPhoneRepo.FindByMetaPhoneNumberID(phoneNumberID)
			if err != nil || businessPhone == nil {
				log.Printf("[coexistence-consumer] no business phone for meta_id=%s", phoneNumberID)
				continue
			}

			if strings.TrimSpace(businessPhone.OwnerWorkspaceID) == "" {
				continue
			}

			organicCampaign, err := uc.campaignRepo.FindLatestOrganicByBusinessPhone(
				businessPhone.OwnerWorkspaceID, businessPhone.ID)
			if err != nil || organicCampaign == nil {
				continue
			}

			for _, echo := range change.Value.MessageEchoes {
				userPhone := echo.To
				if userPhone == "" {
					continue
				}

				leadRecord, _, err := uc.leadRepo.FindOrCreate(businessPhone.OwnerWorkspaceID, userPhone, lead.LeadUpdate{})
				if err != nil {
					continue
				}

				entryRecord, err := uc.entryRepo.FindByCampaignAndLead(organicCampaign.ID, leadRecord.ID)
				if err != nil || entryRecord == nil {
					entryRecord = &wce.WhatsAppCampaignEntry{
						ID:                 uuid.New().String(),
						CampaignID:         organicCampaign.ID,
						LeadID:             leadRecord.ID,
						Status:             wce.SendStatusDelivered,
						ConversationStatus: "finished",
					}
					if createErr := uc.entryRepo.Create(entryRecord); createErr != nil {
						continue
					}
				}

				uc.persistSMBEcho(echo, entryRecord.ID, businessPhone)
			}
		}
	}
}

func (uc *consumeCoexistenceWebhookUseCase) persistSMBEcho(
	echo coexistence.SMBMessageEcho,
	entryID string,
	businessPhone *businessphone.WhatsAppBusinessPhoneNumber,
) {
	msgType, mediaType, text := classifySMBEcho(echo)
	waMessageID := echo.ID

	var ts time.Time
	if unix, err := strconv.ParseInt(echo.Timestamp, 10, 64); err == nil {
		ts = time.Unix(unix, 0).UTC()
	} else {
		ts = time.Now().UTC()
	}

	msg := &conversation.Message{
		ID:                uuid.New().String(),
		EntryID:           entryID,
		EntryType:         shared.EntryTypeWhatsApp,
		Channel:           conversation.MessageChannelWhatsApp,
		MessageType:       msgType,
		From:              echo.From,
		To:                echo.To,
		Text:              text,
		WhatsAppMessageID: &waMessageID,
		DeliveryStatus:    conversation.DeliveryStatusSent,
		MediaType:         mediaType,
		CreatedAt:         ts,
		UpdatedAt:         ts,
	}

	if err := uc.messageRepo.Create(msg); err != nil {
		if !strings.Contains(err.Error(), "duplicate") {
			log.Printf("[coexistence-consumer] failed to persist echo message %s: %v", echo.ID, err)
		}
	}
}

func extractField(raw []byte) string {
	var envelope struct {
		Entry []struct {
			Changes []struct {
				Field string `json:"field"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ""
	}
	for _, entry := range envelope.Entry {
		for _, change := range entry.Changes {
			if change.Field != "" {
				return change.Field
			}
		}
	}
	return ""
}

func classifyHistoryMessage(
	msg coexistence.HistoryMessage,
	businessPhone *businessphone.WhatsAppBusinessPhoneNumber,
) (conversation.MessageType, conversation.MediaType, string) {
	outbound := isBusinessOriginatedHistoryMessage(msg, businessPhone)

	switch msg.Type {
	case "text":
		text := ""
		if msg.Text != nil {
			text = msg.Text.Body
		}
		if outbound {
			return conversation.MessageTypeOperator, "", text
		}
		return conversation.MessageTypeUserMessage, "", text

	case "image":
		caption := ""
		if msg.Image != nil {
			caption = msg.Image.Caption
		}
		if outbound {
			return conversation.MessageTypeOperator, conversation.MediaTypeImage, caption
		}
		return conversation.MessageTypeMedia, conversation.MediaTypeImage, caption

	case "video":
		caption := ""
		if msg.Video != nil {
			caption = msg.Video.Caption
		}
		if outbound {
			return conversation.MessageTypeOperator, conversation.MediaTypeVideo, caption
		}
		return conversation.MessageTypeMedia, conversation.MediaTypeVideo, caption

	case "audio":
		if outbound {
			return conversation.MessageTypeOperator, conversation.MediaTypeAudio, ""
		}
		return conversation.MessageTypeAudio, conversation.MediaTypeAudio, ""

	case "document":
		caption := ""
		if msg.Document != nil {
			caption = msg.Document.Caption
		}
		if outbound {
			return conversation.MessageTypeOperator, conversation.MediaTypeDocument, caption
		}
		return conversation.MessageTypeMedia, conversation.MediaTypeDocument, caption

	case "sticker":
		if outbound {
			return conversation.MessageTypeOperator, conversation.MediaTypeSticker, ""
		}
		return conversation.MessageTypeMedia, conversation.MediaTypeSticker, ""

	case "location":
		text := ""
		if msg.Location != nil {
			text = fmt.Sprintf("📍 %s — %s (%.6f, %.6f)",
				msg.Location.Name, msg.Location.Address,
				msg.Location.Latitude, msg.Location.Longitude)
		}
		if outbound {
			return conversation.MessageTypeOperator, "", text
		}
		return conversation.MessageTypeUserMessage, "", text

	case "contacts":
		parts := make([]string, 0, len(msg.Contacts))
		for _, c := range msg.Contacts {
			parts = append(parts, c.Name.FormattedName)
		}
		text := "👤 " + strings.Join(parts, ", ")
		if outbound {
			return conversation.MessageTypeOperator, "", text
		}
		return conversation.MessageTypeUserMessage, "", text

	default:

		return conversation.MessageTypeSystem, "", fmt.Sprintf("[%s message]", msg.Type)
	}
}

func classifySMBEcho(echo coexistence.SMBMessageEcho) (conversation.MessageType, conversation.MediaType, string) {

	switch echo.Type {
	case "text":
		text := ""
		if echo.Text != nil {
			text = echo.Text.Body
		}
		return conversation.MessageTypeOperator, "", text
	case "image":
		caption := ""
		if echo.Image != nil {
			caption = echo.Image.Caption
		}
		return conversation.MessageTypeOperator, conversation.MediaTypeImage, caption
	case "video":
		caption := ""
		if echo.Video != nil {
			caption = echo.Video.Caption
		}
		return conversation.MessageTypeOperator, conversation.MediaTypeVideo, caption
	case "audio":
		return conversation.MessageTypeOperator, conversation.MediaTypeAudio, ""
	case "document":
		caption := ""
		if echo.Document != nil {
			caption = echo.Document.Caption
		}
		return conversation.MessageTypeOperator, conversation.MediaTypeDocument, caption
	case "sticker":
		return conversation.MessageTypeOperator, conversation.MediaTypeSticker, ""
	default:
		return conversation.MessageTypeSystem, "", fmt.Sprintf("[%s message]", echo.Type)
	}
}

func isBusinessOriginatedHistoryMessage(
	msg coexistence.HistoryMessage,
	businessPhone *businessphone.WhatsAppBusinessPhoneNumber,
) bool {
	businessNumber := normalizeBusinessPhoneNumber(businessPhone)
	if businessNumber == "" {
		return strings.TrimSpace(msg.To) != ""
	}

	from := normalizeParticipantNumber(msg.From)
	if from != "" {
		return from == businessNumber
	}

	to := normalizeParticipantNumber(msg.To)
	return to != "" && to == businessNumber
}

func normalizeBusinessPhoneNumber(businessPhone *businessphone.WhatsAppBusinessPhoneNumber) string {
	if businessPhone == nil {
		return ""
	}
	return normalizeParticipantNumber(businessPhone.DisplayPhoneNumber)
}

func normalizeParticipantNumber(number string) string {
	normalized := lead.NormalizeNumber(number)
	if normalized != "" {
		return normalized
	}

	trimmed := strings.TrimSpace(number)
	if trimmed == "" {
		return ""
	}
	var digits strings.Builder
	digits.Grow(len(trimmed))
	for _, r := range trimmed {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	result := digits.String()
	if result == "" {
		return trimmed
	}
	return result
}

func mapHistoryStatus(status string) conversation.DeliveryStatus {
	switch strings.ToUpper(status) {
	case "SENT":
		return conversation.DeliveryStatusSent
	case "DELIVERED":
		return conversation.DeliveryStatusDelivered
	case "READ":
		return conversation.DeliveryStatusRead
	case "FAILED":
		return conversation.DeliveryStatusFailed
	default:
		return conversation.DeliveryStatusNone
	}
}
