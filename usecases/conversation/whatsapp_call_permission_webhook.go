package conversation_usecase

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	conversation_domain "vozko/domain/conversation"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	callpermission "vozko/domain/whatsapp/call_permission"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type WhatsAppCallPermissionConsumer struct {
	businessPhones businessphone.Repository
	permissions    callpermission.Repository
	entries        wce.Repository
	messages       conversation_domain.MessageRepository
	hub            conversation_domain.EventBroadcaster
}

func NewWhatsAppCallPermissionConsumer(
	businessPhones businessphone.Repository,
	permissions callpermission.Repository,
	entries wce.Repository,
	messages conversation_domain.MessageRepository,
	hub conversation_domain.EventBroadcaster,
) *WhatsAppCallPermissionConsumer {
	return &WhatsAppCallPermissionConsumer{
		businessPhones: businessPhones,
		permissions:    permissions,
		entries:        entries,
		messages:       messages,
		hub:            hub,
	}
}

func (c *WhatsAppCallPermissionConsumer) HandleMessagesWebhook(payload []byte) error {
	if c == nil || c.permissions == nil {
		return nil
	}
	var envelope conversation_domain.WhatsAppWebhookPayload
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return err
	}

	for _, entry := range envelope.Entry {
		for _, change := range entry.Changes {
			meta := change.Value.Metadata
			for _, msg := range change.Value.Messages {
				if msg.Interactive == nil || msg.Interactive.CallPermissionReply == nil {
					continue
				}
				c.process(meta.PhoneNumberID, msg.From, msg.Interactive.CallPermissionReply)
			}
		}
	}
	return nil
}

func (c *WhatsAppCallPermissionConsumer) process(metaPhoneNumberID, userNumber string, reply *conversation_domain.WhatsAppCallPermissionReply) {
	metaPhoneNumberID = strings.TrimSpace(metaPhoneNumberID)
	userNumber = callpermission.NormalizeNumber(userNumber)
	if metaPhoneNumberID == "" || userNumber == "" {
		return
	}

	phone, err := c.businessPhones.FindByMetaPhoneNumberID(metaPhoneNumberID)
	if err != nil || phone == nil {
		return
	}

	now := time.Now().UTC()
	accepted := strings.EqualFold(strings.TrimSpace(reply.Response), "accept")

	perm := &callpermission.CallPermission{
		WorkspaceID:     phone.OwnerWorkspaceID,
		BusinessPhoneID: phone.ID,
		UserNumber:      userNumber,
		ResponseSource:  reply.ResponseSource,
		RespondedAt:     &now,
	}
	if accepted {
		perm.Status = callpermission.StatusGranted
		if reply.ExpirationTimestamp > 0 {
			exp := time.Unix(reply.ExpirationTimestamp, 0).UTC()
			perm.ExpiresAt = &exp
		}
	} else {
		perm.Status = callpermission.StatusRejected
	}

	var entryID, leadID string
	if c.entries != nil {
		if e, eErr := c.entries.FindByNumberAndBusinessPhone(userNumber, phone.ID); eErr == nil && e != nil {
			entryID = e.ID
			leadID = e.LeadID
		}
	}
	perm.LeadID = leadID

	_ = c.permissions.Upsert(perm)

	if entryID == "" || c.messages == nil {
		return
	}

	text := "O cliente recusou receber ligações pelo WhatsApp."
	msgType := conversation_domain.MessageTypeCallPermissionRejected
	if accepted {
		msgType = conversation_domain.MessageTypeCallPermissionGranted
		if perm.ExpiresAt != nil {
			text = "O cliente autorizou ligações pelo WhatsApp até " + perm.ExpiresAt.Format("02/01/2006 15:04") + "."
		} else {
			text = "O cliente autorizou ligações pelo WhatsApp."
		}
	}

	message := &conversation_domain.Message{
		ID:          uuid.NewString(),
		EntryID:     entryID,
		EntryType:   shared.EntryTypeWhatsApp,
		Channel:     conversation_domain.MessageChannelWhatsApp,
		MessageType: msgType,
		From:        userNumber,
		Text:        text,
		Read:        false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	message.Normalize()
	if err := c.messages.Create(message); err != nil {
		return
	}
	if c.hub != nil {
		c.hub.BroadcastNewMessage(entryID, string(shared.EntryTypeWhatsApp), message)
	}
}

var _ conversation_domain.WhatsAppCallPermissionWebhookHandler = (*WhatsAppCallPermissionConsumer)(nil)
