package emailpricing

import (
	"context"
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/balance"
	balance_usecase "vozko/usecases/balance"
)

type PriceList interface {
	Execute(ctx context.Context, workspaceID string) (balance_usecase.EmailPriceListResult, error)
}

var pricingRefusals = []response.ErrorRefusal{
	{Err: balance_usecase.ErrExchangeRateUnavailable, Refusal: response.Refusal{Status: http.StatusServiceUnavailable, Code: "email_exchange_rate_unavailable", Message: "A cotação do dólar não está disponível agora."}},
	{Err: balance.ErrPriceUnavailable, Refusal: response.Refusal{Status: http.StatusServiceUnavailable, Code: "email_pricing_unavailable", Message: "O preço do e-mail não está configurado para este workspace."}},
}

var refusalPricingFailed = response.Refusal{Status: http.StatusInternalServerError, Code: "email_pricing_failed", Message: "Não foi possível ler os preços do e-mail."}

type Handler struct {
	prices PriceList
}

func NewHandler(prices PriceList) *Handler {
	return &Handler{prices: prices}
}

// @Summary		Consultar os preços do e-mail
// @Description	Retorna os preços de e-mail que valem para o workspace, já com o plano aplicado, em micros de dólar e em reais pela cotação atual, e quando a cotação foi atualizada (`exchangeRateUpdatedAt`). `send` é o preço por e-mail enviado (com o valor de 1.000 envios em reais). `receive` é o recebimento de respostas: `perMessage` por resposta recebida mais `perChunk` a cada bloco completo de `chunkBytes` (256 KB) do tamanho da mensagem. Cada envio e cada resposta recebida são debitados da carteira; uma resposta já aceita é sempre entregue e cobrada, mesmo deixando a carteira negativa, e com a carteira negativa novas respostas são recusadas até a recarga. Se um preço ou a cotação não puderem ser lidos, a resposta é uma recusa. Exige `balance:read`.
// @Tags			E-mail
// @Produce		json
// @Success		200	{object}	EmailPricingResponse
// @Failure		403	{object}	response.ErrorResponse	"Workspace obrigatório"
// @Failure		500	{object}	response.ErrorResponse	"email_pricing_failed"
// @Failure		503	{object}	response.ErrorResponse	"email_exchange_rate_unavailable ou email_pricing_unavailable"
// @Security		BearerAuth
// @Router			/email/pricing [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	result, err := h.prices.Execute(r.Context(), workspaceID)
	if err != nil {
		WritePriceListRefusal(w, workspaceID, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponse(result))
}

func WritePriceListRefusal(w http.ResponseWriter, workspaceID string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if response.WriteErrorRefusal(w, err, pricingRefusals) {
		return
	}
	log.Printf("[email-pricing] workspace %s: %v", workspaceID, err)
	response.WriteRefusal(w, refusalPricingFailed)
}

func RefusalCodes() []string {
	return response.RefusalCodes(response.ErrorRefusalsOf(pricingRefusals), []response.Refusal{refusalPricingFailed})
}
