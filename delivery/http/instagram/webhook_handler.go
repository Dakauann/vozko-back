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

const maxWebhookBody = 4 << 20

type WebhookHandler struct {
	publish     webhook.PublishWebhookUseCase
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

func (h *WebhookHandler) verify(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	mode := query.Get("hub.mode")
	challenge := query.Get("hub.challenge")
	token := query.Get("hub.verify_token")

	if mode != "subscribe" || challenge == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if h.verifyToken == "" || !webhookauth.ConstantTimeEqual(token, h.verifyToken) {
		log.Printf("[instagram-webhook] verification rejected (token mismatch)")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(challenge))
}

func (h *WebhookHandler) receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if len(h.appSecrets) == 0 {
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
		log.Printf("[instagram-webhook] undecodable payload, acking: %v", err)
		response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

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
