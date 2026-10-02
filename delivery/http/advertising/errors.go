package advertisinghttp

import (
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	"vozko/domain/balance"
	"vozko/domain/media"
	adsuc "vozko/usecases/advertising"
	aichat_usecase "vozko/usecases/aichat"
)

type errorMapping struct {
	target  error
	status  int
	code    string
	message string
}

var domainErrors = []errorMapping{
	{advertising.ErrAccountNotFound, http.StatusNotFound, "not_found", "Conta de anúncios não encontrada"},
	{advertising.ErrObjectNotFound, http.StatusNotFound, "not_found", "Anúncio não encontrado"},
	{advertising.ErrJobNotFound, http.StatusNotFound, "not_found", "Publicação não encontrada"},
	{advertising.ErrRuleNotFound, http.StatusNotFound, "not_found", "Regra automática não encontrada"},
	{advertising.ErrAudienceNotFound, http.StatusNotFound, "not_found", "Público não encontrado"},
	{advertising.ErrSavedAudienceNotFound, http.StatusNotFound, "not_found", "Público salvo não encontrado"},
	{advertising.ErrLeadFormNotFound, http.StatusNotFound, "not_found", "Formulário não encontrado"},
	{advertising.ErrSettingsNotFound, http.StatusNotFound, "not_found", "Configuração de conversões não encontrada"},
	{advertising.ErrBusinessPhoneNotFound, http.StatusNotFound, "business_phone_not_found", "Número oficial de WhatsApp não encontrado neste workspace"},
	{advertising.ErrGrantNotFound, http.StatusConflict, "reconnect_required", "A conta de anúncios precisa ser reconectada"},
	{advertising.ErrAccountNeedsReconnect, http.StatusConflict, "reconnect_required", "A conta de anúncios precisa ser reconectada"},
	{advertising.ErrMissingScopes, http.StatusConflict, "missing_permissions", "A Meta não concedeu as permissões de anúncios; conecte de novo e aceite todas"},
	{advertising.ErrAccountLinkedElsewhere, http.StatusConflict, "account_linked_elsewhere", "Esta conta de anúncios já está conectada a outro workspace"},
	{advertising.ErrAccountNotActive, http.StatusConflict, "account_not_active", "A conta de anúncios não está ativa na Meta"},
	{advertising.ErrNoFundingSource, http.StatusConflict, "no_payment_method", "Configure um meio de pagamento no Gerenciador de Anúncios da Meta"},
	{advertising.ErrUnknownTimezone, http.StatusConflict, "account_unreadable", "Não foi possível ler o fuso horário da conta de anúncios; sincronize a conta"},
	{advertising.ErrUnknownCurrency, http.StatusConflict, "account_unreadable", "Não foi possível ler a moeda da conta de anúncios; sincronize a conta"},
	{advertising.ErrMixedCurrencies, http.StatusConflict, "mixed_currencies", "A Meta informou valores em outra moeda; nada foi somado"},
	{advertising.ErrPageNotGranted, http.StatusConflict, "page_not_granted", "A página não está disponível nesta conexão de anúncios"},
	{advertising.ErrNumberNotLinked, http.StatusConflict, "number_not_linked", "O número de WhatsApp não está vinculado à página"},
	{advertising.ErrNumberNotOwned, http.StatusConflict, "number_not_owned", "O número de WhatsApp não está conectado a este workspace"},
	{advertising.ErrJobNotRunnable, http.StatusConflict, "job_not_runnable", "A publicação não pode rodar no estado atual"},
	{advertising.ErrVideoNotReady, http.StatusConflict, "video_not_ready", "A Meta ainda está processando o vídeo; tente de novo em instantes"},
	{advertising.ErrObjectLocked, http.StatusConflict, "object_locked", "Itens excluídos ou arquivados não podem ser alterados"},
	{advertising.ErrNoBudget, http.StatusConflict, "no_daily_budget", "Este item não tem orçamento próprio"},
	{advertising.ErrBudgetKindLocked, http.StatusConflict, "budget_kind_locked", "A Meta não troca orçamento diário por total depois que o item é criado"},
	{advertising.ErrAudienceTermsNotAccepted, http.StatusConflict, "audience_terms_not_accepted", "Aceite os termos de públicos personalizados da Meta nesta conta de anúncios"},
	{advertising.ErrBudgetChangeTooSoon, http.StatusTooManyRequests, "budget_change_limit", "A Meta permite 4 mudanças de orçamento por hora"},
	{advertising.ErrInvalidBudget, http.StatusBadRequest, "invalid_budget", "Valor inválido: precisa ser maior que zero e, no limite de gastos, maior que o já gasto"},
	{advertising.ErrInvalidRange, http.StatusBadRequest, "invalid_range", "Período inválido"},
	{advertising.ErrNothingToChange, http.StatusBadRequest, "nothing_to_change", "Nenhuma alteração foi enviada"},
	{advertising.ErrEditNotForLevel, http.StatusUnprocessableEntity, "not_for_level", "Esta alteração não vale para este nível"},
	{advertising.ErrBreakdownCombination, http.StatusUnprocessableEntity, "invalid_breakdown", "A Meta não aceita esta combinação de quebras"},
	{advertising.ErrNoCustomersMatched, http.StatusUnprocessableEntity, "no_customers_matched", "Nenhum cliente tem e-mail ou telefone que a Meta consiga encontrar"},
	{advertising.ErrMediaNotImage, http.StatusBadRequest, "not_an_image", "A mídia escolhida não é uma imagem"},
	{advertising.ErrImageGenerationFailed, http.StatusBadGateway, "image_generation_failed", "Não foi possível gerar a imagem"},
	{media.ErrMediaNotFound, http.StatusNotFound, "not_found", "Mídia não encontrada"},
	{adsuc.ErrCustomerFileUnreadable, http.StatusUnprocessableEntity, "customer_file_unreadable", "O arquivo de clientes não é um CSV legível"},
	{adsuc.ErrConversationNotVisible, http.StatusForbidden, "forbidden", "Você não tem acesso a esta conversa"},
	{adsuc.ErrFeeNotCharged, http.StatusPaymentRequired, "fee_not_charged", "Não foi possível cobrar a taxa do anúncio; verifique o saldo"},
	{aichat_usecase.ErrInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance", "Saldo insuficiente"},
	{aichat_usecase.ErrNoSubscription, http.StatusPaymentRequired, "no_subscription", "O workspace não tem uma assinatura ativa"},
	{balance.ErrInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance", "Saldo insuficiente"},
	{balance.ErrPriceUnavailable, http.StatusServiceUnavailable, "price_unavailable", "O preço do anúncio não está configurado"},
}

func writeError(w http.ResponseWriter, err error, fallback string) {
	var invalid *advertising.ValidationError
	if errors.As(err, &invalid) {
		expected := make(map[string]string, len(invalid.Issues))
		for _, issue := range invalid.Issues {
			expected[issue.Field] = issue.Code
		}
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_draft", "Há campos a corrigir", expected)
		return
	}
	for _, m := range domainErrors {
		if errors.Is(err, m.target) {
			response.WriteErrorWithCode(w, m.status, m.code, m.message, nil)
			return
		}
	}
	switch advertising.Classify(err) {
	case advertising.FailureReauth:
		response.WriteErrorWithCode(w, http.StatusConflict, "reconnect_required", "A conta de anúncios precisa ser reconectada", nil)
		return
	case advertising.FailureRetryable:
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "meta_busy", "A Meta está limitando as chamadas; tente de novo em instantes", nil)
		return
	case advertising.FailurePermission:
		response.WriteErrorWithCode(w, http.StatusForbidden, "meta_permission", "A Meta recusou: falta permissão nesta conta ou página", nil)
		return
	case advertising.FailureRejected:
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "meta_rejected", advertising.Explain(err), nil)
		return
	}
	log.Printf("[ads] %s: %v", fallback, err)
	response.WriteError(w, http.StatusInternalServerError, fallback, nil)
}
