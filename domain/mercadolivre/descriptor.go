package mercadolivre

import (
	"fmt"
	"strings"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

const TextViolationCharset = "charset"

func AccountNameSQL(columnPrefix string) string {
	return "COALESCE(NULLIF(" + columnPrefix + "display_name, ''), NULLIF(" + columnPrefix + "nickname, ''), " + columnPrefix + "ml_user_id)"
}

type charsetTextRule struct{}

func CharsetTextRule() channel.TextRule { return charsetTextRule{} }

func (charsetTextRule) Instruction() string {
	return "Use apenas caracteres do padrão ISO-8859-1 (letras, acentos e sinais comuns do português) e emojis comuns; " +
		"aspas curvas, reticências em um único caractere, travessões e o símbolo do euro são recusados pelo canal."
}

func (charsetTextRule) Check(body string) (channel.TextViolation, bool) {
	offending := offendingMessageRunes(body)
	if len(offending) == 0 {
		return channel.TextViolation{}, false
	}
	quoted := make([]string, 0, len(offending))
	for _, r := range offending {
		quoted = append(quoted, fmt.Sprintf("%q", string(r)))
	}
	return channel.TextViolation{
		Code:    TextViolationCharset,
		Message: "a mensagem tem caracteres que o canal não aceita: " + strings.Join(quoted, ", "),
	}, true
}

func Descriptor() *channel.Descriptor {
	return &channel.Descriptor{
		Kind:      channel.KindMercadoLivre,
		EntryType: shared.EntryTypeMercadoLivre,
		Capabilities: channel.Capabilities{
			CanInitiateConversation: false,
			SupportsTemplates:       false,
			SupportsReactions:       false,
			SupportsTypingIndicator: false,
			SupportsReadReceipts:    true,
			SupportsRichText:        false,

			MaxTextRunes:      MaxMessageRunes,
			ProviderTextRules: []channel.TextRule{CharsetTextRule()},

			OutboundWindow: 0,
			ExtendedWindow: 0,

			SignatureFormat: "%s:\n%s",

			MediaLimits: map[channel.MediaKind]channel.MediaLimit{
				channel.MediaImage: {
					MaxBytes:  MaxAttachmentBytes,
					MIMETypes: []string{MIMEJPEG, MIMEPNG},
				},
				channel.MediaDocument: {
					MaxBytes:  MaxAttachmentBytes,
					MIMETypes: []string{MIMEPDF, MIMEText},
				},
			},

			Interactive: channel.InteractiveLimits{},
		},
		InboxSQL: channel.InboxSQL{
			EntryTable:         "mercadolivre_conversations",
			ContactIDField:     "mlc.contact_id::text",
			AccountIDField:     "COALESCE(mlc.account_id::text, '')",
			ContainerIDField:   "mla.id::text",
			ContainerNameField: AccountNameSQL("mla."),
			AutomationFields: "COALESCE(mla.agent_id::text, '') AS agent_id, " +
				"COALESCE(mla.workflow_id::text, '') AS workflow_id, " +
				"mla.enable_agent_responses AS agent_responses_enabled, " +
				"mla.enable_workflow AS workflow_enabled",
			EntryJoin: `JOIN mercadolivre_conversations mlc ON mlc.id = %[1]s AND mlc.deleted_at IS NULL
			            JOIN mercadolivre_accounts mla ON mla.id = mlc.account_id`,
		},
	}
}
