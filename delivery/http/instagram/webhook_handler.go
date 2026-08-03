package instagram

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	igdomain "vozko/domain/instagram"
	"vozko/domain/webhook"
	"vozko/pkg/webhookauth"
)

// maxWebhookBody caps the inbound body. A batch can hold up to 1000 entries, so
// this is generous but still bounded.
const maxWebhookBody = 4 << 20

// WebhookHandler receives Instagram event notifications.
//
// Meta's budget is a 200 within about 5 seconds, and after an hour of failures it
// UNSUBSCRIBES the webhook entirely. So this handler does the minimum: verify the
// signature over the raw bytes, split the batch per account, publish, and return.
// All real work happens in the queue consumer.
type WebhookHandler struct {
	publish webhook.PublishWebhookUseCase
	// appSecrets accepts more than one secret. The Instagram API setup carries
	// its own app secret, distinct from the WhatsApp/Facebook one, and the docs do
	// not state unambiguously which signs Instagram webhooks, so both are
	// registered and either verifies.
	appSecrets  []string
	verifyToken string
}

func NewWebhookHandler(
	publish webhook.PublishWebhookUseCase,
	appSecrets []string,
	verifyToken string,
) *WebhookHandler {
	cleaned := make([]string, 0, len(appSecrets))
	for _, s := range appSecrets {
		if s = strings.TrimSpace(s); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	return &WebhookHandler{publish: publish, appSecrets: cleaned, verifyToken: strings.TrimSpace(verifyToken)}
}

// Handle serves both the GET verification handshake and the POST event delivery.
func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.verify(w, r)
	case http.MethodPost:
		h.receive(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// verify answers the subscription handshake.
//
// The query parameter names contain DOTS, not underscores, and the challenge must
// be echoed back verbatim as the raw body, parsing it to an int and reprinting it
// risks changing the bytes.
func (h *WebhookHandler) verify(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	mode := query.Get("hub.mode")
	challenge := query.Get("hub.challenge")
	token := query.Get("hub.verify_token")

	if mode != "subscribe" || challenge == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Fail closed on a missing configured token: an unconfigured endpoint must not
	// accept anyone's subscription.
	if h.verifyToken == "" || !webhookauth.ConstantTimeEqual(token, h.verifyToken) {
		log.Printf("[instagram-webhook] verification rejected (token mismatch)")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(challenge))
}

// receive verifies, splits and enqueues an event batch.
func (h *WebhookHandler) receive(w http.ResponseWriter, r *http.Request) {
	// The RAW bytes are read first and hashed as-is. Meta signs an
	// escaped-unicode rendering of the payload, so re-marshalling anything before
	// verification produces a different signature.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if len(h.appSecrets) == 0 {
		// Refuse rather than process unverified input. The WhatsApp handler logs a
		// warning and continues; that is not a default worth copying.
		log.Printf("[instagram-webhook] rejected: no app secret configured")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !h.verifySignature(body, r.Header.Get("X-Hub-Signature-256")) {
		log.Printf("[instagram-webhook] rejected: signature mismatch")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	envelopes, err := igdomain.DecodeEnvelope(body)
	if err != nil {
		// A malformed body will never parse. Ack it so Meta stops retrying, and log
		// it so the shape can be investigated.
		log.Printf("[instagram-webhook] undecodable payload, acking: %v", err)
		response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	// One queue message per entry. A single POST can carry up to 1000 entries and
	// can span MULTIPLE Instagram accounts, so splitting here gives per-account
	// failure isolation: one tenant's poison event cannot block another's.
	entries := igdomain.SplitEntries(envelopes)
	published := 0

	log.Printf("[instagram-webhook] accepted %d byte(s), %d envelope(s), %d entr(y|ies)",
		len(body), len(envelopes), len(entries))

	for _, entry := range entries {
		if entry == nil || entry.Entry == nil {
			continue
		}
		payload, err := json.Marshal(entry)
		if err != nil {
			log.Printf("[instagram-webhook] failed to marshal entry: %v", err)
			continue
		}
		field := fieldOf(entry)
		topic := webhook.TopicForInstagramField(field)
		log.Printf("[instagram-webhook] entry account=%s field=%q -> %s",
			entry.Entry.ID, field, topic)

		if err := h.publish.Publish(topic, payload); err != nil {
			// Returning 500 makes Meta redeliver the whole batch. That is safe
			// because the pipeline is idempotent, and it is far better than
			// acknowledging an event we failed to persist.
			log.Printf("[instagram-webhook] publish failed topic=%s: %v", topic, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		published++
	}

	response.WriteSuccess(w, http.StatusOK, map[string]any{
		"status":    "received",
		"published": published,
	})
}

func (h *WebhookHandler) verifySignature(body []byte, header string) bool {
	for _, secret := range h.appSecrets {
		if webhookauth.VerifyPrefixedHMAC(secret, body, header) {
			return true
		}
	}
	return false
}

// fieldOf determines which topic an entry belongs to.
//
// An entry can carry four different shapes, and the Instagram Login flavour puts
// field/value directly on the entry with NO changes array, the shape most
// implementations miss.
func fieldOf(env *igdomain.EntryEnvelope) string {
	e := env.Entry
	switch {
	case len(e.Messaging) > 0:
		return "messages"
	case len(e.Standby) > 0:
		return "standby"
	case e.Field != "":
		return e.Field
	case len(e.Changes) > 0:
		for _, c := range e.Changes {
			if c != nil && c.Field != "" {
				return c.Field
			}
		}
	}
	return ""
}
