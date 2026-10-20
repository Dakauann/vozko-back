package conversation

import "encoding/json"

const (
	WhatsAppFormReplyType = "nfm_reply"
	FormReplyPlaceholder  = "Formulário respondido"
)

func (m *WhatsAppMessage) FormReply() (*WhatsAppNFMReply, bool) {
	if m == nil || m.Interactive == nil || m.Interactive.NFMReply == nil {
		return nil, false
	}
	if m.Interactive.Type != "" && m.Interactive.Type != WhatsAppFormReplyType {
		return nil, false
	}
	return m.Interactive.NFMReply, true
}

func WithMetadataValue(raw json.RawMessage, key string, value any) (json.RawMessage, error) {
	return withMetadataValue(raw, key, value)
}
