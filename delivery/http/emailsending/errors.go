package emailsending

import (
	"context"
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/response"
	es "vozko/domain/emailsending"
	esuc "vozko/usecases/emailsending"
)

func refusal(status int, code, message string) response.Refusal {
	return response.Refusal{Status: status, Code: code, Message: message}
}

var refusals = []response.ErrorRefusal{
	{Err: es.ErrPlanRequired, Refusal: refusal(http.StatusPaymentRequired, "email_plan_required", "Ative um plano pago para configurar o envio de e-mails.")},
	{Err: es.ErrPlanUnreadable, Refusal: refusal(http.StatusServiceUnavailable, "email_plan_unavailable", "Não foi possível confirmar o plano do workspace agora. Tente de novo em instantes.")},
	{Err: es.ErrAccountReactivation, Refusal: refusal(http.StatusServiceUnavailable, "email_account_reactivation_failed", "Não foi possível reativar o envio de e-mails agora. Tente de novo em instantes.")},
	{Err: esuc.ErrReplyCapacityReached, Refusal: refusal(http.StatusServiceUnavailable, "email_reply_capacity_reached", "O recebimento de respostas está sem capacidade agora. A equipe já foi avisada; tente de novo mais tarde.")},
	{Err: esuc.ErrProviderUnavailable, Refusal: refusal(http.StatusServiceUnavailable, "email_provider_unavailable", "O provedor de e-mail não respondeu. Tente de novo em instantes.")},
	{Err: es.ErrAccountNotProvisioned, Refusal: refusal(http.StatusConflict, "email_account_not_provisioned", "O envio de e-mails ainda não foi ativado neste workspace.")},
	{Err: es.ErrAccountNotFound, Refusal: refusal(http.StatusConflict, "email_account_not_provisioned", "O envio de e-mails ainda não foi ativado neste workspace.")},
	{Err: es.ErrAccountPaused, Refusal: refusal(http.StatusConflict, "email_account_paused", "O envio de e-mails deste workspace está pausado.")},
	{Err: esuc.ErrNotInProbation, Refusal: refusal(http.StatusConflict, "email_account_not_in_probation", "A conta já saiu do período de avaliação.")},
	{Err: es.ErrInvalidTenantName, Refusal: refusal(http.StatusUnprocessableEntity, "email_setup_invalid", "Não foi possível montar o envio de e-mails deste workspace.")},
	{Err: es.ErrInvalidSlug, Refusal: refusal(http.StatusUnprocessableEntity, "email_subdomain_invalid", "Use só letras minúsculas sem acento, números e hífens entre eles, com até 30 caracteres.")},
	{Err: es.ErrSlugReserved, Refusal: refusal(http.StatusUnprocessableEntity, "email_subdomain_reserved", "Este nome é reservado. Escolha outro.")},
	{Err: es.ErrSlugTaken, Refusal: refusal(http.StatusConflict, "email_subdomain_taken", "Este nome já está em uso. Escolha outro.")},
	{Err: es.ErrSubdomainLocked, Refusal: refusal(http.StatusConflict, "email_subdomain_locked", "O endereço não muda mais depois do primeiro envio. Para usar outro nome, conecte um domínio próprio.")},
	{Err: es.ErrUnknownReleaseState, Refusal: refusal(http.StatusUnprocessableEntity, "email_setup_invalid", "Não foi possível montar o envio de e-mails deste workspace.")},
	{Err: es.ErrInvalidSharedDomain, Refusal: refusal(http.StatusUnprocessableEntity, "email_setup_invalid", "Não foi possível montar o envio de e-mails deste workspace.")},
	{Err: es.ErrUnknownDomainKind, Refusal: refusal(http.StatusUnprocessableEntity, "email_setup_invalid", "Não foi possível montar o envio de e-mails deste workspace.")},
	{Err: es.ErrUnknownConnectMethod, Refusal: refusal(http.StatusUnprocessableEntity, "email_setup_invalid", "Não foi possível montar o envio de e-mails deste workspace.")},
	{Err: es.ErrUnknownSetupFailure, Refusal: refusal(http.StatusUnprocessableEntity, "email_setup_invalid", "Não foi possível montar o envio de e-mails deste workspace.")},
	{Err: esuc.ErrDomainInvalid, Refusal: refusal(http.StatusUnprocessableEntity, "email_domain_invalid", "Informe um domínio válido, como suaempresa.com.br.")},
	{Err: esuc.ErrDomainFreeMailbox, Refusal: refusal(http.StatusUnprocessableEntity, "email_domain_free_mailbox", "Domínios de e-mail gratuito, como Gmail ou Outlook, não podem ser usados para enviar.")},
	{Err: es.ErrFreeMailboxFrom, Refusal: refusal(http.StatusUnprocessableEntity, "email_domain_free_mailbox", "Domínios de e-mail gratuito, como Gmail ou Outlook, não podem ser usados para enviar.")},
	{Err: esuc.ErrDomainShared, Refusal: refusal(http.StatusUnprocessableEntity, "email_domain_shared", "Este domínio é de uso compartilhado da plataforma e não pode ser conectado.")},
	{Err: es.ErrDomainTaken, Refusal: refusal(http.StatusConflict, "email_domain_taken", "Este domínio já está conectado em outro workspace.")},
	{Err: es.ErrDomainNotFound, Refusal: refusal(http.StatusNotFound, "email_domain_not_found", "Domínio não encontrado.")},
	{Err: es.ErrDomainNotVerified, Refusal: refusal(http.StatusConflict, "email_domain_not_verified", "O domínio ainda não foi verificado.")},
	{Err: es.ErrDomainReleasing, Refusal: refusal(http.StatusConflict, "email_domain_releasing", "Este domínio está sendo removido e não aceita remetentes.")},
	{Err: esuc.ErrSharedDomainRemoval, Refusal: refusal(http.StatusConflict, "email_domain_shared_removal", "O domínio compartilhado não pode ser removido.")},
	{Err: esuc.ErrDomainInUse, Refusal: refusal(http.StatusConflict, "email_domain_in_use", "Remova ou mova os remetentes deste domínio antes de removê-lo.")},
	{Err: es.ErrInvalidLocalPart, Refusal: refusal(http.StatusUnprocessableEntity, "email_sender_invalid_local_part", "Use um nome de endereço válido antes do @, como contato.")},
	{Err: es.ErrInvalidDisplayName, Refusal: refusal(http.StatusUnprocessableEntity, "email_sender_invalid_display_name", "Use um nome de exibição de até 80 caracteres, sem aspas nem < >.")},
	{Err: es.ErrInvalidForward, Refusal: refusal(http.StatusUnprocessableEntity, "email_sender_invalid_forward", "O endereço para receber cópia das respostas é inválido.")},
	{Err: es.ErrSenderNotFound, Refusal: refusal(http.StatusNotFound, "email_sender_not_found", "Remetente não encontrado.")},
	{Err: es.ErrSenderExists, Refusal: refusal(http.StatusConflict, "email_sender_exists", "Já existe um remetente com este endereço.")},
	{Err: es.ErrSenderDomainMismatch, Refusal: refusal(http.StatusConflict, "email_sender_domain_mismatch", "O remetente não pertence a este domínio.")},
	{Err: esuc.ErrDefaultSenderRequired, Refusal: refusal(http.StatusConflict, "email_sender_default_required", "Escolha outro remetente padrão antes.")},
	{Err: es.ErrNoDefaultSender, Refusal: refusal(http.StatusConflict, "email_sender_default_required", "O workspace ainda não tem remetente padrão.")},
	{Err: esuc.ErrInvalidAddress, Refusal: refusal(http.StatusUnprocessableEntity, "email_address_invalid", "Informe um endereço de e-mail válido.")},
	{Err: esuc.ErrSuppressionNotFound, Refusal: refusal(http.StatusNotFound, "email_suppression_not_found", "Este endereço não está na lista de bloqueio.")},
	{Err: esuc.ErrSuppressionLocked, Refusal: refusal(http.StatusConflict, "email_suppression_locked", "Endereços que denunciaram spam ou que não existem não podem sair da lista de bloqueio.")},
	{Err: es.ErrRecipientSuppressed, Refusal: refusal(http.StatusConflict, "email_recipient_suppressed", "Este destinatário está na lista de bloqueio.")},
	{Err: es.ErrDailyCapReached, Refusal: refusal(http.StatusTooManyRequests, "email_daily_cap_reached", "O limite diário de envios deste domínio foi atingido.")},
	{Err: es.ErrInvalidMessage, Refusal: refusal(http.StatusUnprocessableEntity, "email_message_invalid", "A mensagem está incompleta ou inválida.")},
	{Err: es.ErrInsecureUnsubscribe, Refusal: refusal(http.StatusUnprocessableEntity, "email_message_invalid", "A mensagem está incompleta ou inválida.")},
	{Err: es.ErrRawMessageKey, Refusal: refusal(http.StatusUnprocessableEntity, "email_message_invalid", "A mensagem está incompleta ou inválida.")},
	{Err: es.ErrRawMessageTooLarge, Refusal: refusal(http.StatusUnprocessableEntity, "email_message_invalid", "A mensagem está incompleta ou inválida.")},
	{Err: es.ErrMessageNotFound, Refusal: refusal(http.StatusNotFound, "email_message_not_found", "Mensagem não encontrada.")},
}

var refusalFailed = refusal(http.StatusInternalServerError, "email_request_failed", "Não foi possível concluir a operação de e-mail.")

var setupFailureErrors = map[es.SetupFailure]error{
	es.SetupFailureProvider:      esuc.ErrProviderUnavailable,
	es.SetupFailureReplyCapacity: esuc.ErrReplyCapacityReached,
}

func setupErrorCode(failure es.SetupFailure) string {
	if failure == "" {
		return ""
	}
	return refusalCode(setupFailureErrors[failure])
}

func refusalCode(err error) string {
	if refusal, ok := response.MatchRefusal(err, refusals); ok {
		return refusal.Code
	}
	return refusalFailed.Code
}

func RefusalCodes() []string {
	return response.RefusalCodes(response.ErrorRefusalsOf(refusals), []response.Refusal{refusalFailed})
}

func writeError(w http.ResponseWriter, err error, subject string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if response.WriteErrorRefusal(w, err, refusals) {
		return
	}
	log.Printf("[email] %s: %v", subject, err)
	response.WriteRefusal(w, refusalFailed)
}
