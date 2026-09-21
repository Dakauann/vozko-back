package telegram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	tgdomain "vozko/domain/telegram"
	"vozko/domain/webhook"
	tguc "vozko/usecases/telegram"
)

const maxWebhookBody = 2 << 20

type AccountLookup interface {
	FindByIDForWebhook(ctx context.Context, id string) (*tgdomain.Account, error)
}

type WebhookHandler struct {
	accounts AccountLookup
	publish  webhook.PublishWebhookUseCase
}

func NewWebhookHandler(accounts AccountLookup, publish webhook.PublishWebhookUseCase) *WebhookHandler {
	return &WebhookHandler{accounts: accounts, publish: publish}
}

func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	account, err := h.accounts.FindByIDForWebhook(r.Context(), accountID)
	if err != nil {
		if errors.Is(err, tgdomain.ErrAccountNotFound) {
			log.Printf("[telegram-webhook] rejected: unknown account %s", accountID)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		log.Printf("[telegram-webhook] account lookup failed for %s: %v", accountID, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if !h.verifySecret(account, r.Header.Get(tgdomain.SecretTokenHeader)) {
		log.Printf("[telegram-webhook] rejected: secret token mismatch for account %s", accountID)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	update, err := tgdomain.DecodeUpdate(body)
	if err != nil {
		log.Printf("[telegram-webhook] undecodable update for account %s, acking: %v", accountID, err)
		response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	payload, err := json.Marshal(tguc.QueuedUpdate{AccountID: accountID, Update: body})
	if err != nil {
		log.Printf("[telegram-webhook] failed to marshal queued update: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	topic := topicFor(update)
	if err := h.publish.Publish(topic, payload); err != nil {
		log.Printf("[telegram-webhook] publish failed topic=%s: %v", topic, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]any{"status": "received"})
}

func (h *WebhookHandler) verifySecret(account *tgdomain.Account, presented string) bool {
	if account.WebhookSecret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(account.WebhookSecret), []byte(presented)) == 1
}

func topicFor(u *tgdomain.Update) string {
	switch {
	case u.Message != nil,
		u.EditedMessage != nil,
		u.BusinessMessage != nil,
		u.EditedBusinessMessage != nil,
		u.DeletedBusinessMessages != nil,
		u.CallbackQuery != nil:
		return webhook.TopicTelegramMessage
	default:
		return webhook.TopicTelegramAccount
	}
}
