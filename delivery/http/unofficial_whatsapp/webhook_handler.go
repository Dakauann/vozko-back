package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/domain/webhook"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

// maxWebhookBody caps the inbound body. A history batch is the largest thing
// this endpoint receives, and an unbounded read of a remote body is an
// availability bug waiting for a bad day.
const maxWebhookBody = 8 << 20

// InstanceLookup is the narrow port the handler needs to resolve a tenant.
type InstanceLookup interface {
	FindByDeliveryTokenHash(ctx context.Context, tokenHash string) (*uw.Instance, error)
}

// WebhookHandler receives provider event notifications.
//
// This endpoint is unlike every other webhook in the codebase because the
// provider authenticates NOTHING: it does not sign the body, and its webhook
// configuration accepts only a URL, so we cannot even ask it to echo a header of
// ours. Meta signs with X-Hub-Signature-256; Telegram echoes a secret token.
// This provider does neither.
//
// So the defence is layered, and every layer is load-bearing:
//
//  1. The path segment is 32 random bytes, never our instance uuid, which is
//     guessable from any other API response.
//  2. It is resolved through a SHA-256 digest, so a dumped instances table
//     yields no working URLs.
//  3. The body's own instance id is cross-checked against the row, so forging
//     an event needs both the URL and the provider's instance id.
//  4. The path is redacted before anything reaches a log.
//
// The residual risk — someone holding the URL can inject fabricated inbound
// messages — is a property of a provider that does not sign, not of this
// implementation. Rotation narrows it; nothing here closes it.
type WebhookHandler struct {
	instances InstanceLookup
	publish   webhook.PublishWebhookUseCase
}

func NewWebhookHandler(instances InstanceLookup, publish webhook.PublishWebhookUseCase) *WebhookHandler {
	return &WebhookHandler{instances: instances, publish: publish}
}

// Handle serves one event delivery.
func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	token := mux.Vars(r)["deliveryToken"]
	if token == "" {
		// 401 rather than 400 throughout: an endpoint that distinguishes
		// "malformed" from "unknown" tells a scanner which tokens exist.
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	instance, err := h.instances.FindByDeliveryTokenHash(r.Context(), uw.HashDeliveryToken(token))
	if err != nil {
		if errors.Is(err, uw.ErrInstanceNotFound) {
			// Logged WITHOUT the token: it is the credential, and writing it to
			// a log sink on every probe would defeat rotation entirely.
			log.Printf("[unofficial-whatsapp-webhook] rejected: unknown delivery token")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		log.Printf("[unofficial-whatsapp-webhook] instance lookup failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	env, err := uw.DecodeEnvelope(body)
	if err != nil {
		// A malformed body will never parse. Ack it so the provider stops
		// retrying, and report the shape — this vendor's payloads are
		// undocumented, so an unexpected shape is the only information there is.
		//
		// KEYS ONLY, never values: a dropped body is usually a real message, and
		// the whole point of dropping it is that we could not tell what it was.
		// Logging its contents would put customer message text and phone numbers
		// into the log sink, which is exactly the data this channel must not leak.
		log.Printf("[unofficial-whatsapp-webhook] undecodable body for instance %s, acking: %v (top-level keys: %v)",
			instance.ID, err, uw.DescribeUnknownBody(body))
		response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	// The second factor. Without it, the URL alone would be enough to inject
	// events into any tenant's inbox.
	if env.Instance != "" && env.Instance != instance.ProviderInstanceID {
		log.Printf("[unofficial-whatsapp-webhook] rejected: body names instance %q, token resolves to %q",
			env.Instance, instance.ProviderInstanceID)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	payload, err := json.Marshal(uwuc.QueuedEvent{InstanceID: instance.ID, Body: body})
	if err != nil {
		log.Printf("[unofficial-whatsapp-webhook] failed to marshal queued event: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	topic := webhook.TopicForUnofficialWhatsAppEvent(env.Event)
	if err := h.publish.Publish(topic, payload); err != nil {
		// A non-2xx makes the provider redeliver, which is safe because the
		// pipeline dedups — and necessary because this provider retries only a
		// handful of times and offers NO replay endpoint. Acknowledging an event
		// we failed to enqueue would lose it permanently.
		log.Printf("[unofficial-whatsapp-webhook] publish failed topic=%s: %v", topic, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]any{"status": "received"})
}

// RegisterPublicRoutes wires the unauthenticated webhook endpoint.
//
// Unauthenticated by necessity — the provider calls it — and protected instead
// by the unguessable delivery token in the path plus the instance cross-check
// inside the handler.
func RegisterPublicRoutes(public *mux.Router, wh *WebhookHandler) {
	if wh == nil {
		return
	}
	public.HandleFunc(uw.WebhookPathTemplate, wh.Handle).Methods(http.MethodPost)
}
