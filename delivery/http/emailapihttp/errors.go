package emailapihttp

import (
	"log"
	"net/http"

	"vozko/delivery/http/emailrefusal"
	"vozko/delivery/http/response"
	"vozko/domain/emailapi"
	wh "vozko/domain/emailwebhook"
	emailapi_usecase "vozko/usecases/emailapi"
	tx "vozko/usecases/transactionalemail"
)

const (
	CodeLogFailed     = "email_log_failed"
	CodeWebhookFailed = "email_webhook_failed"
)

var apiRefusals = []response.Refusal{
	{Status: http.StatusBadRequest, Code: emailapi_usecase.CodeIdempotencyKeyInvalid, Message: "idempotency_key precisa de 1 a 256 caracteres imprimíveis."},
	{Status: http.StatusConflict, Code: emailapi_usecase.CodeIdempotencyKeyReused, Message: "Esta idempotency_key já foi usada em outra requisição."},
	{Status: http.StatusBadRequest, Code: emailapi_usecase.CodeFromRequired, Message: "Informe o remetente em from."},
	{Status: http.StatusUnprocessableEntity, Code: emailapi_usecase.CodeSenderUnknown, Message: "from não é um remetente deste workspace."},
	{Status: http.StatusBadRequest, Code: emailapi_usecase.CodeRecipientsInvalid, Message: "Envie para 1 a 50 destinatários, com ao menos um em to."},
	{Status: http.StatusBadRequest, Code: emailapi_usecase.CodeReplyToInvalid, Message: "reply_to aceita até 5 endereços válidos."},
	{Status: http.StatusUnprocessableEntity, Code: emailapi_usecase.CodeHeaderNotAllowed, Message: "Um dos cabeçalhos não é permitido."},
	{Status: http.StatusBadRequest, Code: emailapi_usecase.CodeTagsInvalid, Message: "As tags aceitam até 10 nomes distintos com letras, números, _ e -."},
	{Status: http.StatusUnprocessableEntity, Code: emailapi_usecase.CodeAttachmentInvalid, Message: "Um anexo não tem nome, tipo ou conteúdo válido."},
	{Status: http.StatusUnprocessableEntity, Code: emailapi_usecase.CodeAttachmentBlocked, Message: "Este tipo de arquivo não pode ser anexado."},
	{Status: http.StatusRequestEntityTooLarge, Code: emailapi_usecase.CodeAttachmentTooLarge, Message: "Os anexos passam do tamanho que um e-mail comporta."},
	{Status: http.StatusUnprocessableEntity, Code: emailapi_usecase.CodeAttachmentUnavailable, Message: "Não foi possível baixar um dos anexos."},
}

var logRefusals = []response.Refusal{
	{Status: http.StatusNotFound, Code: emailapi_usecase.CodeMessageNotFound, Message: "E-mail não encontrado."},
	{Status: http.StatusBadRequest, Code: emailapi_usecase.CodeLogCursorInvalid, Message: "O cursor é inválido."},
	{Status: http.StatusBadRequest, Code: emailapi_usecase.CodeLogFilterInvalid, Message: "Um dos filtros é inválido."},
}

var refusalLogFailed = response.Refusal{Status: http.StatusInternalServerError, Code: CodeLogFailed, Message: "Não foi possível ler o registro de envios."}

var webhookRefusals = []response.ErrorRefusal{
	{Err: wh.ErrURLInvalid, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "email_webhook_url_invalid", Message: "O endereço precisa ser https, com host, sem usuário e senha."}},
	{Err: wh.ErrURLBlocked, Refusal: response.Refusal{Status: http.StatusUnprocessableEntity, Code: "email_webhook_url_blocked", Message: "O endereço não resolve para um endereço público da internet."}},
	{Err: wh.ErrEventsInvalid, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "email_webhook_events_invalid", Message: "Escolha ao menos um evento conhecido."}},
	{Err: wh.ErrEndpointNotFound, Refusal: response.Refusal{Status: http.StatusNotFound, Code: "email_webhook_not_found", Message: "Webhook não encontrado."}},
	{Err: wh.ErrLimitReached, Refusal: response.Refusal{Status: http.StatusConflict, Code: "email_webhook_limit_reached", Message: "O workspace já tem o máximo de webhooks."}},
	{Err: wh.ErrDeliveryNotFound, Refusal: response.Refusal{Status: http.StatusNotFound, Code: "email_webhook_delivery_not_found", Message: "Entrega não encontrada."}},
	{Err: wh.ErrDeliveryNotRetryable, Refusal: response.Refusal{Status: http.StatusConflict, Code: "email_webhook_delivery_not_retryable", Message: "Só entregas que falharam ou foram puladas podem ser reenviadas."}},
	{Err: emailapi.ErrCursorInvalid, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: emailapi_usecase.CodeLogCursorInvalid, Message: "O cursor é inválido."}},
}

var refusalWebhookFailed = response.Refusal{Status: http.StatusInternalServerError, Code: CodeWebhookFailed, Message: "Não foi possível concluir a operação com os webhooks."}

var sendByCode = emailrefusal.Index(emailrefusal.Transactional, apiRefusals)

var logByCode = emailrefusal.Index(logRefusals)

func RefusalCodes() []string {
	return response.RefusalCodes(emailrefusal.Transactional, apiRefusals, logRefusals, response.ErrorRefusalsOf(webhookRefusals),
		[]response.Refusal{emailrefusal.SendFailed, refusalLogFailed, refusalWebhookFailed})
}

func writeSendRefusal(w http.ResponseWriter, workspaceID string, err error) {
	emailrefusal.Write(w, "[email-api]", workspaceID, err, sendByCode)
}

func writeLogRefusal(w http.ResponseWriter, workspaceID string, err error) {
	if refusal, ok := logByCode[tx.CodeOf(err)]; ok {
		response.WriteRefusal(w, refusal)
		return
	}
	log.Printf("[email-api] log of workspace %s: %v", workspaceID, err)
	response.WriteRefusal(w, refusalLogFailed)
}

func writeWebhookRefusal(w http.ResponseWriter, workspaceID string, err error) {
	if response.WriteErrorRefusal(w, err, webhookRefusals) {
		return
	}
	log.Printf("[email-webhook] workspace %s: %v", workspaceID, err)
	response.WriteRefusal(w, refusalWebhookFailed)
}
