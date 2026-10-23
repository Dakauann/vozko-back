package emailinbox

import (
	"strconv"
	"time"

	"vozko/domain/emailaddress"
)

const (
	MaxAttachmentTotalBytes   = 25 << 20
	MaxAttachments            = 20
	UnavailableAttachmentText = "[anexo indisponível]"
	EmptyBodyText             = "[mensagem sem texto]"

	MetadataSubject               = "email_subject"
	MetadataQuotedText            = "email_quoted_text"
	MetadataFromDiffers           = "email_from_differs"
	MetadataOriginalRecipient     = "email_original_recipient"
	MetadataAttachmentUnavailable = "email_attachment_unavailable"
	MetadataFileName              = "email_file_name"
	MetadataMessageID             = "email_message_id"
	MetadataTruncated             = "email_truncated"
	MetadataAutomated             = "email_automated"
	MetadataCampaignID            = "email_campaign_id"
	MetadataCampaignName          = "email_campaign_name"
)

type Mailbox struct {
	Name    string
	Address emailaddress.Address
}

type Attachment struct {
	FileName    string
	ContentType string
	Data        []byte
	Size        int64
	Inline      bool
}

type InboundMessage struct {
	From        Mailbox
	To          []emailaddress.Address
	Subject     string
	MessageID   string
	InReplyTo   string
	References  []string
	Text        string
	HTML        string
	QuotedText  string
	Date        time.Time
	Automated   bool
	Attachments []Attachment
}

type MessageParser interface {
	Parse(raw []byte) (InboundMessage, error)
}

func ExternalMessageID(headerMessageID, providerMessageID string) string {
	if id := NormalizeMessageID(headerMessageID); id != "" {
		return id
	}
	if providerMessageID == "" {
		return ""
	}
	return "provider:" + providerMessageID
}

func AttachmentRef(externalID string, index int) string {
	return externalID + ":att" + strconv.Itoa(index)
}

func AttachmentsWithinBudget(attachments []Attachment) []bool {
	kept := make([]bool, len(attachments))
	remaining := int64(MaxAttachmentTotalBytes)
	for i, att := range attachments {
		if att.Data == nil || att.Size > remaining {
			continue
		}
		kept[i] = true
		remaining -= att.Size
	}
	return kept
}
