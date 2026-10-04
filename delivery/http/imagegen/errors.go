package imagegenhttp

import (
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/balance"
	"vozko/domain/imagegen"
	aichat_usecase "vozko/usecases/aichat"
)

type errorMapping struct {
	target  error
	status  int
	code    string
	message string
}

var domainErrors = []errorMapping{
	{imagegen.ErrModelsUnavailable, http.StatusServiceUnavailable, "models_unavailable", "A lista de modelos de imagem não está disponível agora; tente de novo em instantes"},
	{imagegen.ErrNoImageModels, http.StatusServiceUnavailable, "models_unavailable", "A lista de modelos de imagem não está disponível agora; tente de novo em instantes"},
	{imagegen.ErrJobNotFound, http.StatusNotFound, "not_found", "Geração de imagem não encontrada"},
	{imagegen.ErrDuplicateActiveJob, http.StatusConflict, "already_generating", "Esta imagem acabou de ser pedida; tente de novo em instantes"},
	{imagegen.ErrRequesterRequired, http.StatusUnauthorized, "unauthenticated", "Usuário não identificado"},
	{imagegen.ErrWorkspaceRequired, http.StatusForbidden, "workspace_required", "Workspace não informado"},
	{aichat_usecase.ErrInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance", "Saldo insuficiente"},
	{aichat_usecase.ErrNoSubscription, http.StatusPaymentRequired, "no_subscription", "O workspace não tem uma assinatura ativa"},
	{balance.ErrInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance", "Saldo insuficiente"},
}

func writeError(w http.ResponseWriter, err error, fallback string) {
	var invalid *imagegen.ValidationError
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
	log.Printf("[image-generation] %s: %v", fallback, err)
	response.WriteError(w, http.StatusInternalServerError, fallback, nil)
}
