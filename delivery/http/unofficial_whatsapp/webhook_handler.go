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

const maxWebhookBody = 8 << 20

type InstanceLookup interface {
	FindByDeliveryTokenHash(ctx context.Context, tokenHash string) (*uw.Instance, error)
}

type WebhookHandler struct {
	instances InstanceLookup
	publish   webhook.PublishWebhookUseCase
}

func NewWebhookHandler(instances InstanceLookup, publish webhook.PublishWebhookUseCase) *WebhookHandler {
	return &WebhookHandler{instances: instances, publish: publish}
}

func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	token := mux.Vars(r)["deliveryToken"]
	if token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	instance, err := h.instances.FindByDeliveryTokenHash(r.Context(), uw.HashDeliveryToken(token))
	if err != nil {
		if errors.Is(err, uw.ErrInstanceNotFound) {
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
		log.Printf("[unofficial-whatsapp-webhook] undecodable body for instance %s, acking: %v (top-level keys: %v)",
			instance.ID, err, uw.DescribeUnknownBody(body))
		response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

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
		log.Printf("[unofficial-whatsapp-webhook] publish failed topic=%s: %v", topic, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]any{"status": "received"})
}

func RegisterPublicRoutes(public *mux.Router, wh *WebhookHandler) {
	if wh == nil {
		return
	}
	public.HandleFunc(uw.WebhookPathTemplate, wh.Handle).Methods(http.MethodPost)
}
