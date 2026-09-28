package metawebhook

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	mm "vozko/domain/metamessaging"
	"vozko/domain/webhook"
	"vozko/pkg/webhookauth"
)

const maxBody = 4 << 20

type Config struct {
	Name        string
	Publisher   webhook.PublishWebhookUseCase
	Secrets     []string
	VerifyToken string
	Objects     []string
	Route       func(entry *mm.EntryEnvelope) []string
}

type Handler struct {
	name        string
	publish     webhook.PublishWebhookUseCase
	secrets     []string
	verifyToken string
	objects     map[string]struct{}
	route       func(entry *mm.EntryEnvelope) []string
}

func New(cfg Config) *Handler {
	secrets := make([]string, 0, len(cfg.Secrets))
	for _, s := range cfg.Secrets {
		if s = strings.TrimSpace(s); s != "" {
			secrets = append(secrets, s)
		}
	}
	objects := make(map[string]struct{}, len(cfg.Objects))
	for _, o := range cfg.Objects {
		objects[o] = struct{}{}
	}
	return &Handler{
		name:        cfg.Name,
		publish:     cfg.Publisher,
		secrets:     secrets,
		verifyToken: strings.TrimSpace(cfg.VerifyToken),
		objects:     objects,
		route:       cfg.Route,
	}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.verify(w, r)
	case http.MethodPost:
		h.receive(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	challenge := query.Get("hub.challenge")
	if query.Get("hub.mode") != "subscribe" || challenge == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if h.verifyToken == "" || !webhookauth.ConstantTimeEqual(query.Get("hub.verify_token"), h.verifyToken) {
		log.Printf("[%s] verification rejected (token mismatch)", h.name)
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(challenge))
}

func (h *Handler) receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(h.secrets) == 0 {
		log.Printf("[%s] rejected: no app secret configured", h.name)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !h.signatureValid(body, r.Header.Get("X-Hub-Signature-256")) {
		log.Printf("[%s] rejected: signature mismatch", h.name)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	envelopes, err := mm.DecodeEnvelope(body)
	if err != nil {
		log.Printf("[%s] undecodable payload, acking: %v", h.name, err)
		response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	entries := mm.SplitEntries(envelopes)
	published := 0
	for _, entry := range entries {
		if !h.acceptsObject(entry.Object) {
			log.Printf("[%s] ignoring entry %s for object %q", h.name, entry.Entry.ID, entry.Object)
			continue
		}
		topics := h.route(entry)
		if len(topics) == 0 {
			log.Printf("[%s] entry %s has nothing routable", h.name, entry.Entry.ID)
			continue
		}
		payload, err := json.Marshal(entry)
		if err != nil {
			log.Printf("[%s] failed to marshal entry %s: %v", h.name, entry.Entry.ID, err)
			continue
		}
		for _, topic := range topics {
			if err := h.publish.Publish(topic, payload); err != nil {
				log.Printf("[%s] publish failed topic=%s: %v", h.name, topic, err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			published++
		}
	}

	log.Printf("[%s] accepted %d byte(s), %d entr(y|ies), %d publish(es)", h.name, len(body), len(entries), published)
	response.WriteSuccess(w, http.StatusOK, map[string]any{"status": "received", "published": published})
}

func (h *Handler) acceptsObject(object string) bool {
	if len(h.objects) == 0 {
		return true
	}
	_, ok := h.objects[object]
	return ok
}

func (h *Handler) signatureValid(body []byte, header string) bool {
	for _, secret := range h.secrets {
		if webhookauth.VerifyPrefixedHMAC(secret, body, header) {
			return true
		}
	}
	return false
}
