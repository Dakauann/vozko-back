package emailrefusal

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"vozko/delivery/http/response"
	"vozko/domain/emailhygiene"
	balance_usecase "vozko/usecases/balance"
	tx "vozko/usecases/transactionalemail"
)

var SendFailed = response.Refusal{Status: http.StatusBadGateway, Code: tx.CodeSendFailed, Message: "Não foi possível enviar o e-mail agora."}

var Transactional = []response.Refusal{
	{Status: http.StatusBadRequest, Code: tx.CodeRequestInvalid, Message: "A requisição está incompleta."},
	{Status: http.StatusBadRequest, Code: tx.CodeIdempotencyKeyRequired, Message: "Envie idempotency_key para que um reenvio não cobre duas vezes."},
	{Status: http.StatusBadRequest, Code: tx.CodeRecipientMissing, Message: "Informe o lead ou o endereço do destinatário."},
	{Status: http.StatusBadRequest, Code: tx.CodeAddressInvalid, Message: "O endereço de e-mail é inválido."},
	{Status: http.StatusNotFound, Code: tx.CodeLeadNotFound, Message: "Lead não encontrado."},
	{Status: http.StatusUnprocessableEntity, Code: tx.CodeLeadNoEmail, Message: "Este lead não tem e-mail cadastrado."},
	{Status: http.StatusForbidden, Code: tx.CodeLeadBlocked, Message: "Este contato está bloqueado."},
	{Status: http.StatusConflict, Code: tx.CodeLeadOptedOut, Message: "Este contato pediu para não receber mensagens."},
	{Status: http.StatusConflict, Code: tx.CodeRecipientSuppressed, Message: "Este endereço está na lista de supressão."},
	{Status: http.StatusConflict, Code: tx.CodeSenderMissing, Message: "Cadastre um remetente antes de enviar."},
	{Status: http.StatusConflict, Code: tx.CodeSenderNotVerified, Message: "O domínio do remetente ainda não foi verificado."},
	{Status: http.StatusConflict, Code: tx.CodeAccountPaused, Message: "O envio de e-mail deste workspace está pausado."},
	{Status: http.StatusServiceUnavailable, Code: tx.CodeAccountReactivation, Message: "Não foi possível reativar o envio de e-mail agora."},
	{Status: http.StatusTooManyRequests, Code: tx.CodeDailyCapReached, Message: "O limite diário de envios do domínio foi atingido."},
	{Status: http.StatusPaymentRequired, Code: tx.CodeBalanceInsufficient, Message: "Saldo insuficiente para enviar este e-mail."},
	{Status: http.StatusServiceUnavailable, Code: tx.CodeChargeFailed, Message: "A cobrança está indisponível, nada foi enviado."},
	{Status: http.StatusUnprocessableEntity, Code: tx.CodeMessageInvalid, Message: "O conteúdo do e-mail é inválido."},
	{Status: http.StatusUnprocessableEntity, Code: tx.CodeSubjectInvalid, Message: "O assunto do e-mail é inválido."},
	{Status: http.StatusNotFound, Code: tx.CodeDesignNotFound, Message: "Modelo não encontrado."},
	{Status: http.StatusConflict, Code: tx.CodeSendInProgress, Message: "Este envio já está em andamento."},
	{Status: http.StatusTooManyRequests, Code: tx.CodeSendThrottled, Message: "O envio de e-mail está no limite do provedor agora; nada foi cobrado, tente de novo com a mesma chave."},
	{Status: http.StatusServiceUnavailable, Code: tx.CodeUnavailable, Message: "O envio de e-mail não está disponível agora."},
}

var Hygiene = []response.Refusal{
	{Status: http.StatusConflict, Code: emailhygiene.CodeDomainChecking, Message: "O domínio deste endereço ainda está sendo verificado; nada foi cobrado, tente de novo em instantes."},
	{Status: http.StatusUnprocessableEntity, Code: emailhygiene.CodeDomainUnknown, Message: "Não foi possível confirmar que o domínio deste endereço recebe e-mail."},
	{Status: http.StatusUnprocessableEntity, Code: emailhygiene.CodeDisposableAddress, Message: "Este é um endereço de e-mail descartável."},
	{Status: http.StatusUnprocessableEntity, Code: emailhygiene.CodeNoMailDomain, Message: "O domínio deste endereço não recebe e-mail."},
}

func Index(groups ...[]response.Refusal) map[string]response.Refusal {
	out := map[string]response.Refusal{}
	for _, group := range groups {
		for _, r := range group {
			out[r.Code] = r
		}
	}
	return out
}

func Write(w http.ResponseWriter, logPrefix, workspaceID string, err error, byCode map[string]response.Refusal) {
	refusal, known := byCode[tx.CodeOf(err)]
	if !known {
		log.Printf("%s workspace %s: %v", logPrefix, workspaceID, err)
		refusal = SendFailed
	}
	var short *balance_usecase.InsufficientEmailBalanceError
	if refusal.Code == tx.CodeBalanceInsufficient && errors.As(err, &short) {
		response.WriteErrorWithDetails(w, refusal.Status, refusal.Code, refusal.Message, map[string]any{"missing_micros": short.MissingMicros})
		return
	}
	if seconds, ok := tx.RetryAfterSeconds(err); ok {
		w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		response.WriteErrorWithDetails(w, refusal.Status, refusal.Code, refusal.Message, map[string]any{"retry_after_seconds": seconds})
		return
	}
	response.WriteRefusal(w, refusal)
}
