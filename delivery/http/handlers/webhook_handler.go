package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	"vozko/domain/webhook"
	businessphone "vozko/domain/whatsapp/business_phone"
	whatsapptemplate "vozko/domain/whatsapp/template"
	"vozko/pkg/webhookauth"
)

type WebhookHandler struct {
	publishWebhook      webhook.PublishWebhookUseCase
	expectedToken       string
	whatsappVerifyToken string
	whatsappAppSecrets  []string

	dialog360WebhookSecret string
	callWebhook            conversation.WhatsAppCallWebhookHandler

	permissionWebhook conversation.WhatsAppCallPermissionWebhookHandler
}

func (h *WebhookHandler) SetDialog360WebhookSecret(secret string) {
	h.dialog360WebhookSecret = secret
}

func (h *WebhookHandler) SetCallPermissionWebhookHandler(handler conversation.WhatsAppCallPermissionWebhookHandler) {
	h.permissionWebhook = handler
}

func (h *WebhookHandler) SetCallWebhookHandler(handler conversation.WhatsAppCallWebhookHandler) {
	h.callWebhook = handler
}

func NewWebhookHandler(
	publishWebhook webhook.PublishWebhookUseCase,
	expectedToken string,
	whatsappVerifyToken string,
	whatsappAppSecrets ...string,
) *WebhookHandler {
	if expectedToken == "" {
		log.Println("[WARN] ASAAS_WEBHOOK_TOKEN is empty, all Asaas webhook requests will be rejected")
	}
	secrets := normalizeAppSecrets(whatsappAppSecrets)
	if len(secrets) == 0 {
		log.Println("[WARN] META_APP_SECRET is empty, WhatsApp webhook signature verification disabled")
	} else {
		log.Printf("[webhook] WhatsApp signature verification enabled (%d app secret(s))", len(secrets))
	}
	return &WebhookHandler{
		publishWebhook:      publishWebhook,
		expectedToken:       expectedToken,
		whatsappVerifyToken: whatsappVerifyToken,
		whatsappAppSecrets:  secrets,
	}
}

func (h *WebhookHandler) HandleAsaasWebhook(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("asaas-access-token")
	if token == "" || !secureCompare(token, h.expectedToken) {
		response.WriteError(w, http.StatusUnauthorized, "invalid webhook token", nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to read webhook payload", nil)
		return
	}

	if !json.Valid(body) {
		response.WriteError(w, http.StatusBadRequest, "invalid JSON payload", nil)
		return
	}

	if err := h.publishWebhook.Publish(webhook.TopicAsaasPayment, body); err != nil {
		log.Printf("[asaas-webhook] failed to enqueue event: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "failed to enqueue webhook", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]bool{"received": true})
}

func (h *WebhookHandler) HandleWhatsAppWebhook(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("[whatsapp-webhook] received %s request from %s\n", r.Method, r.RemoteAddr)
	switch r.Method {
	case http.MethodGet:
		h.handleWhatsAppVerification(w, r)
	case http.MethodPost:
		h.handleWhatsAppEvent(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *WebhookHandler) handleWhatsAppVerification(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	mode := query.Get("hub.mode")
	challenge := query.Get("hub.challenge")
	verifyToken := query.Get("hub.verify_token")

	if mode == "subscribe" && challenge != "" {
		if h.whatsappVerifyToken != "" && verifyToken != h.whatsappVerifyToken {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		log.Printf("[whatsapp-webhook] verification succeeded mode=%s", mode)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(challenge))
		return
	}

	log.Printf("[whatsapp-webhook] invalid verification request: mode=%s challenge=%t", mode, challenge != "")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte("invalid verification request"))
}

var phoneWebhookFields = map[string]bool{
	businessphone.FieldPhoneNumberQualityUpdate: true,
	businessphone.FieldPhoneNumberNameUpdate:    true,
	businessphone.FieldAccountAlerts:            true,
	businessphone.FieldBusinessCapabilityUpdate: true,
	businessphone.FieldAccountUpdate:            true,
	businessphone.FieldAccountReviewUpdate:      true,
	businessphone.FieldBusinessStatusUpdate:     true,
}

var templateWebhookFields = map[string]bool{
	whatsapptemplate.FieldMessageTemplateStatusUpdate:     true,
	whatsapptemplate.FieldMessageTemplateQualityUpdate:    true,
	whatsapptemplate.FieldMessageTemplateComponentsUpdate: true,
	whatsapptemplate.FieldTemplateCategoryUpdate:          true,
}

var coexistenceWebhookFields = map[string]bool{
	"history":            true,
	"smb_app_state_sync": true,
	"smb_message_echoes": true,
}

func (h *WebhookHandler) handleWhatsAppEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to read webhook payload", nil)
		return
	}

	if len(h.whatsappAppSecrets) > 0 {
		sigHeader := r.Header.Get("X-Hub-Signature-256")
		if !verifyHubSignatureAny(h.whatsappAppSecrets, body, sigHeader) {
			log.Printf("[whatsapp-webhook] invalid X-Hub-Signature-256 from %s", r.RemoteAddr)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	h.routeWhatsAppEnvelope(w, body)
}

func (h *WebhookHandler) HandleDialog360MessageWebhook(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("[360dialog-webhook] received %s request from %s\n", r.Method, r.RemoteAddr)
	switch r.Method {
	case http.MethodGet:
		if challenge := r.URL.Query().Get("hub.challenge"); challenge != "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(challenge))
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodPost:
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to read webhook payload", nil)
		return
	}

	if !h.authenticateDialog360(r) {
		log.Printf("[360dialog-webhook] unauthenticated request from %s", r.RemoteAddr)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	h.routeWhatsAppEnvelope(w, body)
}

func (h *WebhookHandler) authenticateDialog360(r *http.Request) bool {
	if h.dialog360WebhookSecret == "" {
		log.Printf("[360dialog-webhook] rejected: D360_WEBHOOK_SECRET not configured (fail-closed)")
		return false
	}
	if s := r.URL.Query().Get("secret"); s != "" && secureCompare(s, h.dialog360WebhookSecret) {
		return true
	}
	if s := r.Header.Get("X-360Dialog-Webhook-Secret"); s != "" && secureCompare(s, h.dialog360WebhookSecret) {
		return true
	}
	return false
}

func (h *WebhookHandler) routeWhatsAppEnvelope(w http.ResponseWriter, body []byte) {
	field := extractWebhookField(body)

	if field == "calls" && h.callWebhook != nil {
		if err := h.callWebhook.HandleCallsWebhook(body); err != nil {
			log.Printf("[whatsapp-webhook] calls handler error: %v", err)
		}
		response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "received"})
		return
	}

	if field == "messages" && h.permissionWebhook != nil {
		if err := h.permissionWebhook.HandleMessagesWebhook(body); err != nil {
			log.Printf("[whatsapp-webhook] call permission handler error: %v", err)
		}
	}

	var topic string
	switch {
	case phoneWebhookFields[field]:
		topic = webhook.TopicWhatsAppPhone
	case templateWebhookFields[field]:
		topic = webhook.TopicWhatsAppTemplate
	case coexistenceWebhookFields[field]:
		topic = webhook.TopicWhatsAppCoexistence
	default:
		topic = webhook.TopicWhatsAppMessage
	}

	if err := h.publishWebhook.Publish(topic, body); err != nil {
		log.Printf("[whatsapp-webhook] failed to enqueue event (topic=%s): %v", topic, err)
		response.WriteError(w, http.StatusInternalServerError, "failed to enqueue webhook", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "received"})
}

func extractWebhookField(body []byte) string {
	var envelope struct {
		Entry []struct {
			Changes []struct {
				Field string `json:"field"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	for _, entry := range envelope.Entry {
		for _, change := range entry.Changes {
			if change.Field != "" {
				return change.Field
			}
		}
	}
	return ""
}

func secureCompare(a, b string) bool {
	return webhookauth.ConstantTimeEqual(a, b)
}

func verifyHubSignature(appSecret string, body []byte, sigHeader string) bool {
	return webhookauth.VerifyPrefixedHMAC(appSecret, body, sigHeader)
}

func normalizeAppSecrets(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func verifyHubSignatureAny(appSecrets []string, body []byte, sigHeader string) bool {
	for _, secret := range appSecrets {
		if verifyHubSignature(secret, body, sigHeader) {
			return true
		}
	}
	return false
}
