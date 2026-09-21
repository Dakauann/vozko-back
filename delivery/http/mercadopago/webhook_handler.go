package mercadopagohttp

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/webhook"
	"vozko/infra/mercadopago"
)

const maxBodyBytes = 1 << 20

type WebhookHandler struct {
	publishWebhook webhook.PublishWebhookUseCase
	secret         string
	tolerance      time.Duration
	now            func() time.Time
}

type Option func(*WebhookHandler)

func WithSignatureTolerance(d time.Duration) Option {
	return func(h *WebhookHandler) {
		if d > 0 {
			h.tolerance = d
		}
	}
}

func WithClock(now func() time.Time) Option {
	return func(h *WebhookHandler) {
		if now != nil {
			h.now = now
		}
	}
}

func NewWebhookHandler(publishWebhook webhook.PublishWebhookUseCase, secret string, opts ...Option) *WebhookHandler {
	if secret == "" {
		log.Println("[WARN] MERCADOPAGO_WEBHOOK_SECRET is empty, all Mercado Pago webhook requests will be rejected")
	}
	h := &WebhookHandler{
		publishWebhook: publishWebhook,
		secret:         secret,
		now:            time.Now,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func RegisterPublicRoutes(r *mux.Router, h *WebhookHandler) {
	if h == nil {
		return
	}
	r.HandleFunc("/webhooks/mercadopago", h.HandleWebhook).Methods(http.MethodPost, http.MethodGet)
}

func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.WriteHeader(http.StatusOK)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to read webhook payload", nil)
		return
	}

	query := r.URL.Query()

	notification, parseErr := mercadopago.ParseNotification(body, query)

	candidateIDs := []string{query.Get("data.id"), query.Get("id")}
	if notification != nil {
		candidateIDs = append(candidateIDs, notification.ResourceID())
	}

	verifiedID, err := mercadopago.VerifySignatureAny(
		r.Header.Get("x-signature"),
		r.Header.Get("x-request-id"),
		candidateIDs,
		h.secret,
		h.tolerance,
		h.now(),
	)
	if err != nil {
		var sigErr *mercadopago.SignatureError
		reason := "unknown"
		if errors.As(err, &sigErr) {
			reason = string(sigErr.Reason)
		}
		log.Printf("[mercadopago-webhook] rejected request from %s: %s (request-id=%s, query-data.id=%q, body-id=%q)",
			r.RemoteAddr, reason, r.Header.Get("x-request-id"),
			query.Get("data.id"), bodyResourceID(notification))
		response.WriteError(w, http.StatusUnauthorized, "invalid webhook signature", nil)
		return
	}

	if parseErr != nil {
		log.Printf("[mercadopago-webhook] unparseable notification from %s: %v", r.RemoteAddr, parseErr)
		response.WriteSuccess(w, http.StatusOK, map[string]bool{"received": true})
		return
	}

	if verifiedID != "" && !strings.EqualFold(verifiedID, notification.ResourceID()) {
		log.Printf("[mercadopago-webhook] notification body claims id %q but the signature covers %q; using the signed id",
			notification.ResourceID(), verifiedID)
		notification.Data.ID = verifiedID
		notification.Resource = ""
	}

	normalized, err := json.Marshal(notification)
	if err != nil {
		log.Printf("[mercadopago-webhook] failed to encode notification: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "failed to enqueue webhook", nil)
		return
	}

	if err := h.publishWebhook.Publish(webhook.TopicMercadoPagoPayment, normalized); err != nil {
		log.Printf("[mercadopago-webhook] failed to enqueue event: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "failed to enqueue webhook", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]bool{"received": true})
}

func bodyResourceID(n *mercadopago.Notification) string {
	if n == nil {
		return ""
	}
	return n.ResourceID()
}
