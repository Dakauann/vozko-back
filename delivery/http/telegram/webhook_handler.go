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

// maxWebhookBody caps the inbound body. Telegram posts exactly one Update per
// request, there is no batching, so this is generous even for a large album
// caption.
const maxWebhookBody = 2 << 20

// AccountLookup is the narrow port the handler needs to resolve a tenant.
type AccountLookup interface {
	FindByIDForWebhook(ctx context.Context, id string) (*tgdomain.Account, error)
}

// WebhookHandler receives Telegram update notifications.
//
// Two things make this endpoint different from every other webhook in the
// codebase, and both come from the same fact: an Update carries NO identification
// of the bot it belongs to.
//
//  1. Tenancy comes from the URL. Each bot is registered with its own
//     /webhooks/telegram/{accountId} path, keyed by our uuid, never by the bot
//     token, which would leak a permanent credential through proxy logs and
//     Referer headers.
//  2. Authenticity comes from a header, not a signature. Telegram does not sign
//     the body; it echoes a secret token we chose. That single header is the
//     only thing separating a real update from a forged one, so it is compared
//     in constant time and a mismatch is refused outright.
type WebhookHandler struct {
	accounts AccountLookup
	publish  webhook.PublishWebhookUseCase
}

func NewWebhookHandler(accounts AccountLookup, publish webhook.PublishWebhookUseCase) *WebhookHandler {
	return &WebhookHandler{accounts: accounts, publish: publish}
}

// Handle serves one update delivery.
func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	accountID := mux.Vars(r)["accountId"]
	if accountID == "" {
		// 401 rather than 400: an endpoint that distinguishes "malformed" from
		// "unknown" tells a scanner which account ids exist.
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	account, err := h.accounts.FindByIDForWebhook(r.Context(), accountID)
	if err != nil {
		if errors.Is(err, tgdomain.ErrAccountNotFound) {
			// Deliberately 401, not 404, for the same reason.
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
		// A malformed body will never parse. Ack it so Telegram stops retrying,
		// and log it so the shape can be investigated.
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
		// A non-2xx makes Telegram redeliver. That is safe, the pipeline dedups
		// on update_id, and far better than acknowledging an update we failed to
		// enqueue, because Telegram discards undelivered updates after 24 hours
		// and there is no history API to recover them from.
		log.Printf("[telegram-webhook] publish failed topic=%s: %v", topic, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]any{"status": "received"})
}

// verifySecret compares the echoed token in constant time.
//
// An account with no secret is refused rather than waved through: it can only
// mean the row predates registration or was tampered with, and accepting
// unverified input is never the safer default.
func (h *WebhookHandler) verifySecret(account *tgdomain.Account, presented string) bool {
	if account.WebhookSecret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(account.WebhookSecret), []byte(presented)) == 1
}

// topicFor routes an update to its queue.
//
// Anything unrecognised lands on the account topic rather than being discarded:
// the Bot API adds update kinds several times a year, and silence would hide
// real traffic.
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
