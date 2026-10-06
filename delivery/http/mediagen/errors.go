package mediagenhttp

import (
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/balance"
	"vozko/domain/mediagen"
	aichat_usecase "vozko/usecases/aichat"
)

type errorMapping struct {
	target  error
	status  int
	code    string
	message string
}

var domainErrors = []errorMapping{
	{mediagen.ErrModelsUnavailable, http.StatusServiceUnavailable, "models_unavailable", "A lista de modelos não está disponível agora; tente de novo em instantes"},
	{mediagen.ErrNoModels, http.StatusServiceUnavailable, "models_unavailable", "Não há modelos disponíveis para este tipo de mídia agora; tente de novo em instantes"},
	{mediagen.ErrJobNotFound, http.StatusNotFound, "not_found", "Geração não encontrada"},
	{mediagen.ErrTooManyActive, http.StatusTooManyRequests, "too_many_jobs", "Já há processamentos demais em andamento neste workspace; aguarde um terminar"},
	{mediagen.ErrDuplicateActiveJob, http.StatusConflict, "already_generating", "Este pedido acabou de ser feito; tente de novo em instantes"},
	{mediagen.ErrRequesterRequired, http.StatusUnauthorized, "unauthenticated", "Usuário não identificado"},
	{mediagen.ErrWorkspaceRequired, http.StatusForbidden, "workspace_required", "Workspace não informado"},
	{aichat_usecase.ErrInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance", "Saldo insuficiente"},
	{aichat_usecase.ErrNoSubscription, http.StatusPaymentRequired, "no_subscription", "O workspace não tem uma assinatura ativa"},
	{balance.ErrInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance", "Saldo insuficiente"},
}

func WriteError(w http.ResponseWriter, err error, fallback string) {
	var invalid *mediagen.ValidationError
	if errors.As(err, &invalid) {
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_request", "Há campos a corrigir", invalid.Codes())
		return
	}
	for _, m := range domainErrors {
		if errors.Is(err, m.target) {
			response.WriteErrorWithCode(w, m.status, m.code, m.message, nil)
			return
		}
	}
	log.Printf("[media-generation] %s: %v", fallback, err)
	response.WriteError(w, http.StatusInternalServerError, fallback, nil)
}
