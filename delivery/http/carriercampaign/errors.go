package carriercampaign

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"vozko/delivery/http/audiencex"
	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	cm "vozko/domain/carriermessaging"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/selection"
	"vozko/domain/shared"
	"vozko/domain/sms"
	ccuc "vozko/usecases/carriercampaign"
)

func refusal(status int, code, message string) response.Refusal {
	return response.Refusal{Status: status, Code: code, Message: message}
}

const (
	invalid  = http.StatusUnprocessableEntity
	conflict = http.StatusConflict
)

var refusals = []response.ErrorRefusal{
	{Err: cc.ErrCampaignNotFound, Refusal: refusal(http.StatusNotFound, "carrier_campaign_not_found", "Campanha não encontrada.")},
	{Err: cc.ErrRunNotFound, Refusal: refusal(http.StatusNotFound, "carrier_campaign_run_not_found", "Execução da campanha não encontrada.")},
	{Err: cc.ErrChannelInvalid, Refusal: refusal(invalid, "carrier_campaign_channel_invalid", "Escolha RCS ou SMS.")},
	{Err: cc.ErrNameRequired, Refusal: refusal(invalid, "carrier_campaign_name_required", "Dê um nome à campanha.")},
	{Err: cc.ErrNameTooLong, Refusal: refusal(invalid, "carrier_campaign_name_too_long", "O nome da campanha está longo demais.")},
	{Err: cc.ErrSenderRequired, Refusal: refusal(invalid, "carrier_campaign_sender_required", "Escolha o remetente da campanha.")},
	{Err: cc.ErrKindInvalid, Refusal: refusal(invalid, "carrier_campaign_kind_invalid", "Escolha se a campanha é de marketing ou transacional.")},
	{Err: cc.ErrStatusInvalid, Refusal: refusal(invalid, "carrier_campaign_status_invalid", "Situação de campanha desconhecida.")},
	{Err: cc.ErrVariantsRequired, Refusal: refusal(invalid, "carrier_campaign_variants_required", "Escreva a mensagem da campanha.")},
	{Err: cc.ErrTooManyVariants, Refusal: refusal(invalid, "carrier_campaign_too_many_variants", "Um teste A/B tem no máximo 4 variações.")},
	{Err: cc.ErrVariantNameRequired, Refusal: refusal(invalid, "carrier_campaign_variant_name_required", "Dê um nome a cada variação do teste A/B.")},
	{Err: cc.ErrVariantNameDuplicate, Refusal: refusal(invalid, "carrier_campaign_variant_name_duplicate", "Duas variações têm o mesmo nome.")},
	{Err: cc.ErrVariantChannelMismatch, Refusal: refusal(invalid, "carrier_campaign_variant_channel_mismatch", "Campanhas RCS levam conteúdo rico e campanhas de SMS levam texto.")},
	{Err: cc.ErrFallbackNotAllowed, Refusal: refusal(invalid, "carrier_campaign_fallback_not_allowed", "Só campanhas RCS têm envio alternativo.")},
	{Err: cc.ErrWhatsAppFallbackIncomplete, Refusal: refusal(invalid, "carrier_campaign_whatsapp_fallback_incomplete", "Escolha o número e o modelo do WhatsApp do envio alternativo.")},
	{Err: cc.ErrWhatsAppActorRequired, Refusal: refusal(invalid, "carrier_campaign_whatsapp_actor_required", "O envio alternativo pelo WhatsApp precisa de quem o configurou.")},
	{Err: cc.ErrSMSRouteRequired, Refusal: refusal(invalid, "carrier_campaign_sms_route_required", "Escolha o remetente de SMS.")},
	{Err: cc.ErrScheduleInPast, Refusal: refusal(invalid, "carrier_campaign_schedule_in_past", "O agendamento precisa ser no futuro.")},
	{Err: cc.ErrScheduleMissing, Refusal: refusal(invalid, "carrier_campaign_schedule_missing", "Escolha quando a campanha começa.")},
	{Err: cc.ErrRecurrenceEnded, Refusal: refusal(invalid, "carrier_campaign_recurrence_ended", "A repetição não tem mais nenhuma data pela frente.")},
	{Err: cc.ErrRecurrenceFrequency, Refusal: refusal(invalid, "carrier_campaign_recurrence_frequency", "A repetição é diária, semanal ou mensal.")},
	{Err: cc.ErrRecurrenceTime, Refusal: refusal(invalid, "carrier_campaign_recurrence_time", "Informe o horário da repetição no formato HH:MM.")},
	{Err: cc.ErrRecurrenceTimezone, Refusal: refusal(invalid, "carrier_campaign_recurrence_timezone", "O fuso horário da repetição é desconhecido.")},
	{Err: cc.ErrRecurrenceWeekdays, Refusal: refusal(invalid, "carrier_campaign_recurrence_weekdays", "A repetição semanal precisa de dias da semana diferentes.")},
	{Err: cc.ErrRecurrenceDayOfMonth, Refusal: refusal(invalid, "carrier_campaign_recurrence_day_of_month", "A repetição mensal precisa de um dia do mês de 1 a 31.")},
	{Err: shared.ErrDateInvalid, Refusal: refusal(invalid, "carrier_campaign_recurrence_until_invalid", "Informe a data final da repetição no formato AAAA-MM-DD.")},
	{Err: cc.ErrAudienceInvalid, Refusal: refusal(invalid, "carrier_campaign_audience_invalid", "O público escolhido não é válido.")},
	{Err: cc.ErrAudienceRequired, Refusal: refusal(invalid, "carrier_campaign_audience_required", "Escolha o público da campanha.")},
	{Err: cc.ErrFinished, Refusal: refusal(conflict, "carrier_campaign_finished", "A campanha já terminou.")},
	{Err: cc.ErrNotEditable, Refusal: refusal(conflict, "carrier_campaign_not_editable", "Só rascunhos, campanhas prontas e campanhas agendadas podem mudar.")},
	{Err: cc.ErrNotDraft, Refusal: refusal(conflict, "carrier_campaign_not_draft", "Só um rascunho pode ficar pronto.")},
	{Err: cc.ErrReadyScheduled, Refusal: refusal(conflict, "carrier_campaign_ready_scheduled", "Tire o agendamento para deixar a campanha pronta.")},
	{Err: cc.ErrNotScheduled, Refusal: refusal(conflict, "carrier_campaign_not_scheduled", "A campanha não está agendada.")},
	{Err: cc.ErrNotLaunched, Refusal: refusal(conflict, "carrier_campaign_not_launched", "A campanha ainda não foi iniciada.")},
	{Err: cc.ErrRunFinished, Refusal: refusal(conflict, "carrier_campaign_run_finished", "Esta execução da campanha já terminou.")},
	{Err: cc.ErrRecipientsInvalid, Refusal: refusal(invalid, "carrier_campaign_recipients_invalid", "O número de destinatários não é válido.")},
	{Err: campaign.ErrAlreadyRunning, Refusal: refusal(conflict, "carrier_campaign_already_running", "A campanha já está em andamento.")},
	{Err: campaign.ErrNotRunning, Refusal: refusal(conflict, "carrier_campaign_not_running", "A campanha não está em andamento.")},
	{Err: campaign.ErrAlreadyStopped, Refusal: refusal(conflict, "carrier_campaign_already_stopped", "A campanha já foi encerrada.")},
	{Err: campaign.ErrBindingsMismatch, Refusal: refusal(invalid, "carrier_campaign_bindings_mismatch", "Cada variável da mensagem precisa de um preenchimento.")},
	{Err: campaign.ErrBindingUnknown, Refusal: refusal(invalid, "carrier_campaign_binding_unknown", "Uma variável usa um preenchimento desconhecido.")},
	{Err: campaign.ErrBindingLiteralEmpty, Refusal: refusal(invalid, "carrier_campaign_binding_literal_empty", "Uma variável de texto fixo está vazia.")},
	{Err: campaign.ErrBindingFieldUnknown, Refusal: refusal(invalid, "carrier_campaign_binding_field_unknown", "Uma variável usa um campo do lead que não existe.")},
	{Err: campaign.ErrBindingSensitive, Refusal: refusal(invalid, "carrier_campaign_binding_sensitive", "Campos sensíveis do lead não podem ir na mensagem.")},
	{Err: campaign.ErrTimelineBucketInvalid, Refusal: refusal(invalid, "carrier_campaign_timeline_bucket_invalid", "O intervalo do gráfico é minuto, hora ou dia.")},
	{Err: ccuc.ErrActorRequired, Refusal: refusal(http.StatusUnauthorized, "carrier_campaign_actor_required", "Entre de novo para continuar.")},
	{Err: ccuc.ErrOnlyDraftDeletes, Refusal: refusal(conflict, "carrier_campaign_only_draft_deletes", "Só rascunhos e campanhas prontas podem ser excluídos.")},
	{Err: ccuc.ErrAudienceEmpty, Refusal: refusal(invalid, "carrier_campaign_audience_empty", "O público escolhido não tem ninguém.")},
	{Err: ccuc.ErrVariantIndex, Refusal: refusal(invalid, "carrier_campaign_variant_unknown", "A campanha não tem essa variação.")},
	{Err: ccuc.ErrRunAlreadyExists, Refusal: refusal(conflict, "carrier_campaign_run_exists", "Esta data da campanha já foi iniciada.")},
	{Err: ccuc.ErrQueueMissing, Refusal: refusal(http.StatusServiceUnavailable, "carrier_campaign_queue_unavailable", "A fila de envio está indisponível. Tente de novo em instantes.")},
	{Err: ccuc.ErrRecipientStatusInvalid, Refusal: refusal(invalid, "carrier_campaign_recipient_status_invalid", "Situação de destinatário desconhecida.")},
	{Err: ccuc.ErrOverviewWindow, Refusal: refusal(invalid, "carrier_campaign_overview_window", "A visão geral cobre 7, 30 ou 90 dias.")},
	{Err: errQueryInvalid, Refusal: refusal(http.StatusBadRequest, "carrier_campaign_query_invalid", "Os filtros da consulta não são válidos.")},
	{Err: selection.ErrEveryoneUnconfirmed, Refusal: refusalUnconfirmed},
	{Err: selection.ErrFingerprintMismatch, Refusal: refusalUnconfirmed},
	{Err: selection.ErrCountRequired, Refusal: refusalUnconfirmed},
	{Err: selection.ErrScopeDenied, Refusal: refusal(http.StatusForbidden, "carrier_campaign_audience_forbidden", "Você não tem acesso a esse público.")},
	{Err: lead.ErrLeadForbidden, Refusal: refusal(http.StatusForbidden, "carrier_campaign_audience_forbidden", "Você não tem acesso a esse público.")},
	{Err: rcs.ErrContentEmpty, Refusal: refusal(invalid, "carrier_campaign_content_invalid", "A mensagem RCS não é válida. Revise o conteúdo.")},
	{Err: rcs.ErrContentMixed, Refusal: refusal(invalid, "carrier_campaign_content_invalid", "A mensagem RCS não é válida. Revise o conteúdo.")},
	{Err: rcs.ErrTextTooLong, Refusal: refusal(invalid, "carrier_campaign_content_invalid", "A mensagem RCS não é válida. Revise o conteúdo.")},
	{Err: rcs.ErrTooManySuggestions, Refusal: refusal(invalid, "carrier_campaign_content_invalid", "A mensagem RCS não é válida. Revise o conteúdo.")},
	{Err: rcs.ErrTooManyCards, Refusal: refusal(invalid, "carrier_campaign_content_invalid", "A mensagem RCS não é válida. Revise o conteúdo.")},
	{Err: rcs.ErrCardEmpty, Refusal: refusal(invalid, "carrier_campaign_content_invalid", "A mensagem RCS não é válida. Revise o conteúdo.")},
	{Err: rcs.ErrCardTitleTooLong, Refusal: refusal(invalid, "carrier_campaign_content_invalid", "A mensagem RCS não é válida. Revise o conteúdo.")},
	{Err: rcs.ErrCardTextTooLong, Refusal: refusal(invalid, "carrier_campaign_content_invalid", "A mensagem RCS não é válida. Revise o conteúdo.")},
	{Err: rcs.ErrSuggestionInvalid, Refusal: refusal(invalid, "carrier_campaign_content_invalid", "A mensagem RCS não é válida. Revise o conteúdo.")},
	{Err: rcs.ErrMediaInvalid, Refusal: refusal(invalid, "carrier_campaign_content_invalid", "A mensagem RCS não é válida. Revise o conteúdo.")},
	{Err: sms.ErrTextEmpty, Refusal: refusal(invalid, "carrier_campaign_sms_text_empty", "O SMS está vazio.")},
	{Err: sms.ErrTextTooLong, Refusal: refusal(invalid, "carrier_campaign_sms_text_too_long", "Com o nome da marca o SMS passa de 8 partes.")},
	{Err: cm.ErrPhoneInvalid, Refusal: refusal(invalid, "carrier_campaign_phone_invalid", "O número de telefone não é válido.")},
	{Err: cm.ErrTestLimit, Refusal: refusal(http.StatusTooManyRequests, "carrier_campaign_test_limit", "Os envios de teste gratuitos de hoje acabaram.")},
	{Err: cm.ErrChannelNotLaunched, Refusal: refusal(conflict, "carrier_campaign_channel_coming_soon", "Este canal estará disponível em breve.")},
	{Err: cm.ErrSuppressed, Refusal: refusal(conflict, "carrier_campaign_contact_suppressed", "O contato pediu para não receber mensagens de marketing.")},
	{Err: cm.ErrRecipientRefused, Refusal: refusal(conflict, "carrier_campaign_contact_refused", "O contato está bloqueado ou pediu para sair.")},
	{Err: cm.ErrFrequencyCap, Refusal: refusal(conflict, "carrier_campaign_frequency_cap", "O contato já recebeu o máximo de mensagens de marketing permitido.")},
	{Err: cm.ErrPriceUnavailable, Refusal: refusal(http.StatusServiceUnavailable, "carrier_campaign_price_unavailable", "Este tipo de mensagem ainda não tem preço configurado.")},
	{Err: cm.ErrProviderRefused, Refusal: refusal(http.StatusBadGateway, "carrier_campaign_provider_refused", "O provedor recusou a mensagem.")},
	{Err: cm.ErrProviderUnavailable, Refusal: refusal(http.StatusServiceUnavailable, "carrier_campaign_provider_unavailable", "O provedor não respondeu. Tente de novo em instantes.")},
}

var (
	refusalShortfall   = refusal(http.StatusPaymentRequired, "carrier_campaign_insufficient_balance", "O saldo não cobre o pior caso da campanha e a reserva para respostas.")
	refusalQuietHours  = refusal(conflict, "carrier_campaign_quiet_hours", "Mensagens de marketing só saem dentro do horário de envio.")
	refusalMissing     = refusal(invalid, "carrier_campaign_missing_variable", "Uma variável da mensagem ficou vazia para este contato.")
	refusalNotReady    = refusal(conflict, "carrier_campaign_not_ready", "O remetente da campanha não está pronto para enviar.")
	refusalChanged     = refusal(conflict, "carrier_campaign_audience_changed", "O público mudou desde a contagem. Confirme o novo total para continuar.")
	refusalUnconfirmed = refusal(conflict, "carrier_campaign_audience_unconfirmed", "Confirme o total do público antes de iniciar a campanha.")
	refusalFailed      = refusal(http.StatusInternalServerError, "carrier_campaign_request_failed", "Não foi possível concluir a operação.")
)

var errQueryInvalid = errors.New("carrier campaign: invalid query parameter")

func RefusalCodes() []string {
	return response.RefusalCodes(response.ErrorRefusalsOf(refusals), []response.Refusal{refusalShortfall, refusalQuietHours, refusalMissing, refusalNotReady, refusalChanged, refusalFailed})
}

type ShortfallDetails struct {
	MissingMicros int64 `json:"missing_micros"`
}

type QuietHoursDetails struct {
	NextOpen time.Time `json:"next_open"`
}

type MissingVariableDetails struct {
	Slot   int    `json:"slot"`
	Source string `json:"source,omitempty"`
}

type NotReadyDetails struct {
	Blocker string `json:"blocker"`
}

type AudienceChangedDetails struct {
	Expected int `json:"expected"`
	Matched  int `json:"matched"`
}

func writeDetails(w http.ResponseWriter, r response.Refusal, details any) {
	response.WriteErrorWithDetails(w, r.Status, r.Code, r.Message, details)
}

func writeError(w http.ResponseWriter, err error, subject string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	var short *cm.ShortfallError
	if errors.As(err, &short) {
		writeDetails(w, refusalShortfall, ShortfallDetails{MissingMicros: short.MissingMicros})
		return
	}
	var quiet *cm.QuietHoursError
	if errors.As(err, &quiet) {
		writeDetails(w, refusalQuietHours, QuietHoursDetails{NextOpen: quiet.NextOpen})
		return
	}
	if errors.Is(err, cm.ErrQuietHours) {
		response.WriteRefusal(w, refusalQuietHours)
		return
	}
	if missing, ok := campaign.MissingVariablesOf(err); ok {
		writeDetails(w, refusalMissing, MissingVariableDetails{Slot: missing[0].Slot, Source: string(missing[0].Source)})
		return
	}
	var changed *selection.CountChangedError
	if errors.As(err, &changed) {
		writeDetails(w, refusalChanged, AudienceChangedDetails{Expected: changed.Expected, Matched: changed.Matched})
		return
	}
	if blocker, ok := ccuc.BlockerOf(err); ok {
		writeDetails(w, refusalNotReady, NotReadyDetails{Blocker: blocker})
		return
	}
	if response.WriteErrorRefusal(w, err, refusals) || audiencex.WriteRefusal(w, err) {
		return
	}
	log.Printf("[carrier-campaign] %s: %v", subject, err)
	response.WriteRefusal(w, refusalFailed)
}
