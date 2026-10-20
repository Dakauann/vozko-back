package conversation

import (
	"bytes"
	"errors"
	"strings"

	"vozko/domain/shared"
)

const ResentAsMetadataKey = "resent_as"

var resentAsMetadataMarker = []byte(`"` + ResentAsMetadataKey + `"`)

var ErrResendNotAllowed = errors.New("conversation: only a failed operator message, not sent again yet, of a kind the operator send reproduces exactly can be resent")

func (m *Message) BelongsTo(entryID string, entryType shared.EntryType) bool {
	return m != nil && m.EntryID != "" && m.EntryID == strings.TrimSpace(entryID) && m.EntryType == entryType
}

func (m *Message) ResentAs() string {
	if m == nil || !bytes.Contains(m.Metadata, resentAsMetadataMarker) {
		return ""
	}
	return strings.TrimSpace(metadataString(metadataFields(m.Metadata), ResentAsMetadataKey))
}

func (m *Message) MarkResentAs(messageID string) error {
	updated, err := withMetadataValue(m.Metadata, ResentAsMetadataKey, strings.TrimSpace(messageID))
	if err != nil {
		return err
	}
	m.Metadata = updated
	return nil
}

func CheckResend(m *Message, entryID string, entryType shared.EntryType) error {
	if !m.BelongsTo(entryID, entryType) {
		return ErrMessageNotFound
	}
	if !m.resendable() {
		return ErrResendNotAllowed
	}
	return nil
}

func (m *Message) resendableBy(operatorID string) bool {
	return strings.TrimSpace(operatorID) != "" && m.resendable()
}

func (m *Message) resendable() bool {
	return m != nil && m.ResolvedDirection().IsOutbound() && m.SenderUserID() != "" &&
		m.DeliveryStatus == DeliveryStatusFailed && !m.IsRetracted() && m.ResentAs() == "" &&
		m.reproducibleByOperatorSend()
}

func (m *Message) reproducibleByOperatorSend() bool {
	if m.MessageType != MessageTypeOperator || len(m.Image) > 0 || len(m.Video) > 0 {
		return false
	}
	if _, isLocation := LocationOf(m); isLocation {
		return false
	}
	if m.HasButtons() {
		return !m.HasMedia()
	}
	if m.HasMedia() {
		return m.MediaType.sendableWithCaption()
	}
	return m.MediaType == "" && m.Media == nil && strings.TrimSpace(m.Text) != ""
}

func (t MediaType) sendableWithCaption() bool {
	switch t {
	case MediaTypeImage, MediaTypeVideo, MediaTypeAudio, MediaTypeDocument:
		return true
	}
	return false
}

func (m *Message) ResendInput() OperatorSendInput {
	in := OperatorSendInput{
		EntryID:   m.EntryID,
		EntryType: string(m.EntryType),
		Text:      m.Text,
	}
	if m.ReplyToMessageID != nil {
		in.ReplyToMessageID = *m.ReplyToMessageID
	}
	if buttons, ok := m.Buttons(); ok {
		in.Buttons = &buttons
		in.Text = ""
		return in
	}
	if m.HasMedia() {
		in.MediaID, in.MediaType = *m.MediaID, string(m.MediaType)
	}
	return in
}
