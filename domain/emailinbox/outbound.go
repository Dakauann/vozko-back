package emailinbox

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	"vozko/domain/emailsending"
)

var ErrOutboundIncomplete = errors.New("emailinbox: the sent email lacks its workspace, address, external id or a business sender")

type OutboundEmail struct {
	WorkspaceID    string
	LeadID         string
	LeadName       string
	To             emailaddress.Address
	SenderID       string
	Subject        string
	Text           string
	HTML           string
	SentMessageID  string
	CampaignMerge  *CampaignMerge
	ExternalID     string
	SentBy         conversation.SentBy
	OpensContact   bool
	SentAt         time.Time
	CampaignID     string
	CampaignName   string
	DeliveryStatus conversation.DeliveryStatus
}

func (e OutboundEmail) Validate() error {
	if strings.TrimSpace(e.WorkspaceID) == "" || strings.TrimSpace(string(e.To)) == "" ||
		strings.TrimSpace(e.ExternalID) == "" || !e.SentBy.FromTheBusiness() {
		return ErrOutboundIncomplete
	}
	return nil
}

type SentEmailTranscript interface {
	Transcribe(ctx context.Context, email OutboundEmail) (conversationID string, err error)
}

func OutboundExternalID(providerMessageID, messageID string) string {
	if id := strings.TrimSpace(providerMessageID); id != "" {
		return id
	}
	return strings.TrimSpace(messageID)
}

func OutboundMessageType(by conversation.SentBy) conversation.MessageType {
	switch by.Kind() {
	case conversation.SenderHuman, conversation.SenderCampaign:
		return conversation.MessageTypeOperator
	}
	return conversation.MessageTypeAIResponse
}

type SentOriginal struct {
	Subject      string
	Text         string
	SentBy       conversation.SentBy
	CampaignID   string
	CampaignName string
	Merge        *CampaignMerge
}

func (o SentOriginal) OutboundOf(sent emailsending.Message) OutboundEmail {
	return OutboundEmail{
		WorkspaceID:    sent.WorkspaceID,
		To:             sent.Address,
		Subject:        o.Subject,
		Text:           o.Text,
		ExternalID:     OutboundExternalID(sent.ProviderMessageID, sent.ID),
		SentBy:         o.SentBy,
		SentAt:         sent.SentAt,
		CampaignID:     o.CampaignID,
		CampaignName:   o.CampaignName,
		SentMessageID:  sent.ID,
		CampaignMerge:  o.Merge,
		DeliveryStatus: TranscriptStatus(sent.Status),
	}
}

func TranscriptStatus(status emailsending.MessageStatus) conversation.DeliveryStatus {
	switch status {
	case emailsending.MessageSent:
		return conversation.DeliveryStatusSent
	case emailsending.MessageDelivered:
		return conversation.DeliveryStatusDelivered
	case emailsending.MessageBounced, emailsending.MessageSuppressed, emailsending.MessageFailed:
		return conversation.DeliveryStatusFailed
	}
	return conversation.DeliveryStatusNone
}

func (c Conversation) ThreadAfterOutbound(subject string) Thread {
	next := NormalizeSubject(subject)
	if next == "" {
		next = c.Subject
	}
	return Thread{Subject: next, ThreadKey: c.ThreadKey, LastInboundMessageID: c.LastInboundMessageID, References: c.References}
}
