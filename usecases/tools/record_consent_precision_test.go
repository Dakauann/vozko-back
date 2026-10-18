package tools_usecase

import (
	"strings"
	"testing"
)

func TestRecordOptOutRecordsOnlyAnExplicitRequestToStopReceivingMessages(t *testing.T) {
	description := optOutTool(t, &consentRecorderStub{}, conversationMessages()).Definition().Description
	for _, rule := range []string{
		"pedido explícito para parar de receber mensagens",
		"desinteresse por um produto ou oferta",
		"só sobre ligações",
		"cancelar um pedido, plano ou assinatura",
		"reclamação sem pedido para parar",
		"ambíguo",
		"pergunte ao lead se ele quer parar de receber mensagens",
		"cite a mensagem em que ele confirmou",
	} {
		if !strings.Contains(description, rule) {
			t.Errorf("the record_opt_out description must state %q", rule)
		}
	}
}

func TestRecordConsentRecordsOnlyAnExplicitYesToTheAgentsOwnQuestion(t *testing.T) {
	description := consentTool(t, &consentRecorderStub{}, conversationMessages()).Definition().Description
	for _, rule := range []string{
		"sim explícito",
		"sua própria pergunta sobre receber mensagens",
		"ambígua",
		"pergunte de novo",
		"cite a mensagem em que ele confirmou",
	} {
		if !strings.Contains(description, rule) {
			t.Errorf("the record_consent description must state %q", rule)
		}
	}
}

func TestConsentToolDescriptionsListNoWordsToMatch(t *testing.T) {
	for _, description := range []string{
		optOutTool(t, &consentRecorderStub{}, conversationMessages()).Definition().Description,
		consentTool(t, &consentRecorderStub{}, conversationMessages()).Definition().Description,
	} {
		for _, example := range []string{"sair", "stop", "descadastr", "\"sim\"", "\"não\""} {
			if strings.Contains(strings.ToLower(description), example) {
				t.Errorf("a description lists %q as a word to match: mechanics only", example)
			}
		}
	}
}
