package carriermessaging

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"vozko/delivery/http/response"
	cm "vozko/domain/carriermessaging"
	"vozko/domain/media"
	"vozko/domain/rcs"
	"vozko/domain/sms"
	cmuc "vozko/usecases/carriermessaging"
	"vozko/usecases/shared/automationrefs"
)

func refusal(status int, code, message string) response.Refusal {
	return response.Refusal{Status: status, Code: code, Message: message}
}

var refusals = []response.ErrorRefusal{
	{Err: rcs.ErrSenderNotFound, Refusal: refusal(http.StatusNotFound, "rcs_sender_not_found", "Remetente RCS não encontrado.")},
	{Err: rcs.ErrSenderCannotSend, Refusal: refusal(http.StatusConflict, "rcs_sender_not_launched", "O remetente RCS ainda não está ativo.")},
	{Err: rcs.ErrSenderTransition, Refusal: refusal(http.StatusConflict, "rcs_sender_transition", "O remetente RCS não pode fazer essa mudança agora.")},
	{Err: rcs.ErrBillingCategoryFixed, Refusal: refusal(http.StatusConflict, "rcs_billing_category_fixed", "A categoria de cobrança não muda depois do envio para aprovação.")},
	{Err: rcs.ErrDescriptionNotISV, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_description_not_isv", "A descrição precisa citar o nome da marca.")},
	{Err: rcs.ErrContactEmailDomain, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_contact_email_domain", "O e-mail de contato precisa ser do domínio da própria marca.")},
	{Err: rcs.ErrSectorRefused, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_sector_refused", "As operadoras não aceitam remetentes deste setor.")},
	{Err: rcs.ErrGamblingAuthorization, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_gambling_authorization", "Apostas exigem o comprovante da autorização do governo.")},
	{Err: cmuc.ErrCNPJInvalid, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_cnpj_invalid", "O CNPJ informado não é válido.")},
	{Err: cmuc.ErrPauseNotYours, Refusal: refusal(http.StatusConflict, "rcs_pause_not_yours", "Esta pausa só pode ser retirada pela plataforma ou pela operadora.")},
	{Err: rcs.ErrContentEmpty, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_content_empty", "A mensagem está vazia.")},
	{Err: rcs.ErrContentMixed, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_content_mixed", "Cartões não podem vir junto com texto ou mídia avulsa.")},
	{Err: rcs.ErrTextTooLong, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_text_too_long", "O texto passou do limite do RCS.")},
	{Err: rcs.ErrTooManySuggestions, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_too_many_buttons", "A mensagem tem botões demais.")},
	{Err: rcs.ErrTooManyCards, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_too_many_cards", "Um carrossel tem no máximo 10 cartões.")},
	{Err: rcs.ErrCardEmpty, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_card_empty", "Um cartão precisa de título, texto ou mídia.")},
	{Err: rcs.ErrCardTitleTooLong, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_card_title_too_long", "O título do cartão está longo demais.")},
	{Err: rcs.ErrCardTextTooLong, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_card_text_too_long", "O texto do cartão está longo demais.")},
	{Err: rcs.ErrSuggestionInvalid, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_button_invalid", "Um botão está incompleto ou inválido.")},
	{Err: rcs.ErrMediaInvalid, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_media_invalid", "O arquivo de mídia não é aceito pelo RCS.")},
	{Err: media.ErrMediaNotFound, Refusal: refusal(http.StatusNotFound, "rcs_media_not_found", "Arquivo não encontrado na biblioteca de mídia.")},
	{Err: media.ErrRemoteFileMissing, Refusal: refusal(http.StatusUnprocessableEntity, "rcs_media_unreachable", "Não foi possível ler o arquivo da biblioteca.")},
	{Err: sms.ErrRouteNotFound, Refusal: refusal(http.StatusNotFound, "sms_route_not_found", "Remetente de SMS não encontrado.")},
	{Err: sms.ErrRouteCannotSend, Refusal: refusal(http.StatusConflict, "sms_route_paused", "O remetente de SMS está pausado.")},
	{Err: sms.ErrRouteInvalid, Refusal: refusal(http.StatusUnprocessableEntity, "sms_route_invalid", "O nome da marca no SMS precisa ter de 2 a 20 letras sem acento, números e espaços.")},
	{Err: sms.ErrNoSharedNumber, Refusal: refusal(http.StatusServiceUnavailable, "sms_no_shared_number", "O número compartilhado de SMS não está configurado.")},
	{Err: cmuc.ErrNumberTaken, Refusal: refusal(http.StatusConflict, "sms_number_taken", "Este número já atende outro remetente de SMS.")},
	{Err: sms.ErrTextEmpty, Refusal: refusal(http.StatusUnprocessableEntity, "sms_text_empty", "O SMS está vazio.")},
	{Err: sms.ErrTextTooLong, Refusal: refusal(http.StatusUnprocessableEntity, "sms_text_too_long", "O SMS passou de 8 partes.")},
	{Err: cm.ErrPhoneInvalid, Refusal: refusal(http.StatusUnprocessableEntity, "carrier_phone_invalid", "O número de telefone não é válido.")},
	{Err: cm.ErrChannelNotLaunched, Refusal: refusal(http.StatusConflict, "carrier_channel_coming_soon", "Este canal estará disponível em breve.")},
	{Err: cm.ErrSuppressed, Refusal: refusal(http.StatusConflict, "carrier_contact_suppressed", "O contato pediu para não receber mensagens de marketing.")},
	{Err: cm.ErrRecipientRefused, Refusal: refusal(http.StatusConflict, "carrier_contact_refused", "O contato está bloqueado ou pediu para sair.")},
	{Err: cm.ErrFrequencyCap, Refusal: refusal(http.StatusConflict, "carrier_frequency_cap", "O contato já recebeu o máximo de mensagens de marketing permitido.")},
	{Err: cm.ErrTestLimit, Refusal: refusal(http.StatusTooManyRequests, "carrier_test_limit", "Os 20 envios de teste gratuitos de hoje acabaram.")},
	{Err: cm.ErrPriceUnavailable, Refusal: refusal(http.StatusServiceUnavailable, "carrier_price_unavailable", "Este tipo de mensagem ainda não tem preço configurado.")},
	{Err: cm.ErrSettingsInvalid, Refusal: refusal(http.StatusUnprocessableEntity, "carrier_settings_invalid", "As regras de envio informadas não são válidas.")},
	{Err: cm.ErrMessageNotFound, Refusal: refusal(http.StatusNotFound, "carrier_message_not_found", "Mensagem não encontrada.")},
	{Err: cm.ErrConversationNotFound, Refusal: refusal(http.StatusNotFound, "carrier_conversation_not_found", "Conversa não encontrada.")},
	{Err: cm.ErrProviderRefused, Refusal: refusal(http.StatusBadGateway, "carrier_provider_refused", "O provedor recusou a mensagem.")},
	{Err: cm.ErrProviderUnavailable, Refusal: refusal(http.StatusServiceUnavailable, "carrier_provider_unavailable", "O provedor não respondeu. Tente de novo em instantes.")},
	{Err: cmuc.ErrChannelInvalid, Refusal: refusal(http.StatusUnprocessableEntity, "carrier_channel_invalid", "Canal desconhecido.")},
	{Err: cmuc.ErrAutomationIncomplete, Refusal: refusal(http.StatusUnprocessableEntity, "carrier_automation_incomplete", "Escolha o agente ou o fluxo antes de ligar a automação.")},
	{Err: automationrefs.ErrNotInWorkspace, Refusal: refusal(http.StatusUnprocessableEntity, "carrier_inbox_refs_not_in_workspace", "O departamento, o funil, o agente ou o fluxo escolhido não é deste workspace.")},
}

var (
	refusalShortfall  = refusal(http.StatusPaymentRequired, "carrier_insufficient_balance", "O saldo não cobre este envio e a reserva para respostas.")
	refusalQuietHours = refusal(http.StatusConflict, "carrier_quiet_hours", "Mensagens de marketing só saem dentro do horário de envio.")
	refusalFailed     = refusal(http.StatusInternalServerError, "carrier_request_failed", "Não foi possível concluir a operação.")
	refusalIncomplete = refusal(http.StatusUnprocessableEntity, "rcs_sender_incomplete", "Faltam dados da marca para enviar o remetente.")
	refusalPause      = refusal(http.StatusUnprocessableEntity, "rcs_pause_reason_invalid", "Motivo de pausa desconhecido.")
)

func RefusalCodes() []string {
	return response.RefusalCodes(response.ErrorRefusalsOf(refusals), []response.Refusal{refusalShortfall, refusalQuietHours, refusalFailed, refusalPause, refusalIncomplete})
}

type ShortfallDetails struct {
	MissingMicros int64 `json:"missing_micros"`
}

type IncompleteDetails struct {
	Field string `json:"field"`
}

type QuietHoursDetails struct {
	NextOpen time.Time `json:"next_open"`
}

func writeError(w http.ResponseWriter, err error, subject string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	var short *cm.ShortfallError
	if errors.As(err, &short) {
		response.WriteErrorWithDetails(w, refusalShortfall.Status, refusalShortfall.Code, refusalShortfall.Message, ShortfallDetails{MissingMicros: short.MissingMicros})
		return
	}
	var incomplete *rcs.IncompleteError
	if errors.As(err, &incomplete) {
		r := refusalIncomplete
		response.WriteErrorWithDetails(w, r.Status, r.Code, r.Message, IncompleteDetails{Field: incomplete.Field})
		return
	}
	var quiet *cm.QuietHoursError
	if errors.As(err, &quiet) {
		response.WriteErrorWithDetails(w, refusalQuietHours.Status, refusalQuietHours.Code, refusalQuietHours.Message, QuietHoursDetails{NextOpen: quiet.NextOpen})
		return
	}
	if errors.Is(err, cm.ErrQuietHours) {
		response.WriteRefusal(w, refusalQuietHours)
		return
	}
	if response.WriteErrorRefusal(w, err, refusals) {
		return
	}
	log.Printf("[carrier] %s: %v", subject, err)
	response.WriteRefusal(w, refusalFailed)
}
