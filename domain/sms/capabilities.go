package sms

import (
	"fmt"

	"vozko/domain/channel"
)

const TextViolationSegments = "sms_segments"

type segmentRule struct{}

func (segmentRule) Instruction() string {
	return fmt.Sprintf("SMS: no máximo %d partes; uma parte tem 160 caracteres sem acentos ou 70 com acentos e emojis, então prefira frases curtas e sem acentos.", MaxSegments)
}

func (segmentRule) Check(body string) (channel.TextViolation, bool) {
	m := Measure(body)
	if m.Segments <= MaxSegments {
		return channel.TextViolation{}, false
	}
	return channel.TextViolation{
		Code:    TextViolationSegments,
		Message: fmt.Sprintf("a mensagem ocupa %d partes de SMS e o limite é %d", m.Segments, MaxSegments),
	}, true
}

func Capabilities() channel.Capabilities {
	return channel.Capabilities{
		CanInitiateConversation: true,
		SupportsReadReceipts:    false,
		MaxTextRunes:            limits[GSM7].multi * MaxSegments,
		ProviderTextRules:       []channel.TextRule{segmentRule{}},
		SignatureFormat:         "%s: %s",
	}
}
