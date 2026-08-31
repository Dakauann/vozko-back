// Package mercadopagohttp exposes the public inbound webhook endpoint for Mercado
// Pago payment notifications.
//
// It lives in its own package rather than alongside the Asaas endpoint because the two
// authenticate completely differently: Asaas sends a shared token in a header, while
// Mercado Pago signs an HMAC manifest built from a query parameter and a request-id
// header. Keeping them apart keeps each verification path readable and independently
// testable.
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

// maxBodyBytes caps the notification body. Mercado Pago notifications are a few
// hundred bytes; the limit exists so a hostile caller cannot stream indefinitely.
const maxBodyBytes = 1 << 20

// WebhookHandler receives, authenticates and enqueues Mercado Pago notifications.
type WebhookHandler struct {
	publishWebhook webhook.PublishWebhookUseCase
	secret         string
	// tolerance optionally bounds how old a signature timestamp may be. Zero disables
	// the check, which is the correct default: Mercado Pago retries a failed delivery
	// for hours without re-signing it, so a narrow window would reject exactly the
	// retries that carry the payments we missed.
	tolerance time.Duration
	now       func() time.Time
}

// Option configures the handler.
type Option func(*WebhookHandler)

// WithSignatureTolerance enables the replay window. Only worth setting when the
// deployment accepts losing legitimate late retries in exchange for a tighter window.
func WithSignatureTolerance(d time.Duration) Option {
	return func(h *WebhookHandler) {
		if d > 0 {
			h.tolerance = d
		}
	}
}

// WithClock overrides the clock used for the tolerance check. Intended for tests.
func WithClock(now func() time.Time) Option {
	return func(h *WebhookHandler) {
		if now != nil {
			h.now = now
		}
	}
}

// NewWebhookHandler builds the handler. An empty secret leaves the endpoint fully
// closed: every request is rejected, and that is logged loudly at boot rather than
// quietly accepting unsigned traffic that can mark invoices paid.
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

// RegisterPublicRoutes mounts the endpoint. It is public because Mercado Pago calls
// it; authentication is the x-signature check, not the router.
func RegisterPublicRoutes(r *mux.Router, h *WebhookHandler) {
	if h == nil {
		return
	}
	r.HandleFunc("/webhooks/mercadopago", h.HandleWebhook).Methods(http.MethodPost, http.MethodGet)
}

// HandleWebhook authenticates a notification and hands it to the queue.
//
// It never does real work inline. Mercado Pago gives the endpoint 22 seconds and
// retries anything slower, so the handler only verifies, normalizes and enqueues; the
// API round trip needed to interpret the notification happens in the consumer.
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Mercado Pago's dashboard probes the URL before saving it. Answer plainly.
		w.WriteHeader(http.StatusOK)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to read webhook payload", nil)
		return
	}

	query := r.URL.Query()

	// Parse first, because the signed id can come from either place. Mercado Pago
	// documents the data.id QUERY parameter, and its own SDK reads that, but a
	// meaningful share of real notifications arrive with no query parameter while still
	// being signed over the id in the body. Verifying against only one source rejects
	// authentic notifications, which means silently losing paid invoices.
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
		// The reason is logged, never returned: a forger must not learn which part of
		// the attempt was wrong. The id sources are logged too, because a mismatch is
		// almost always about which one Mercado Pago signed.
		log.Printf("[mercadopago-webhook] rejected request from %s: %s (request-id=%s, query-data.id=%q, body-id=%q)",
			r.RemoteAddr, reason, r.Header.Get("x-request-id"),
			query.Get("data.id"), bodyResourceID(notification))
		response.WriteError(w, http.StatusUnauthorized, "invalid webhook signature", nil)
		return
	}

	if parseErr != nil {
		log.Printf("[mercadopago-webhook] unparseable notification from %s: %v", r.RemoteAddr, parseErr)
		// 200 on purpose: the payload is authentic but unusable, and retrying it would
		// only make Mercado Pago resend the same broken notification for hours.
		response.WriteSuccess(w, http.StatusOK, map[string]bool{"received": true})
		return
	}

	// Act on the id that was actually SIGNED, never on an unsigned one. Otherwise a
	// valid signature over one payment could be replayed with another named in the
	// body, and the consumer would go and process that instead.
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
		// A 5xx makes Mercado Pago retry, which is exactly what should happen when the
		// queue is the thing that is broken.
		response.WriteError(w, http.StatusInternalServerError, "failed to enqueue webhook", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]bool{"received": true})
}

// bodyResourceID is a nil-safe accessor for rejection logging, where the notification
// may well have failed to parse.
func bodyResourceID(n *mercadopago.Notification) string {
	if n == nil {
		return ""
	}
	return n.ResourceID()
}
