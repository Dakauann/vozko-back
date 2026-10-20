package tiktok

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	ttdomain "vozko/domain/tiktok"
	"vozko/pkg/webhookauth"
	ttuc "vozko/usecases/tiktok"
)

const (
	maxWebhookBody     = 1 << 20
	signatureTolerance = 5 * time.Minute
	signatureHeader    = "Tiktok-Signature"
)

type Publisher interface {
	Publish(topic string, message []byte) error
}

type WebhookHandler struct {
	secret    string
	publisher Publisher
	metrics   ttuc.Metrics
	now       func() time.Time
}

func NewWebhookHandler(secret string, publisher Publisher, metrics ttuc.Metrics) *WebhookHandler {
	if strings.TrimSpace(secret) == "" || publisher == nil || metrics == nil {
		panic("tiktok webhook: the app secret, the publisher and metrics are required")
	}
	return &WebhookHandler{secret: secret, publisher: publisher, metrics: metrics, now: time.Now}
}

// @Summary		Webhook do TikTok
// @Description	Recebe eventos do TikTok (mensagens diretas, ecos, leituras, indicações e comentários). A assinatura `Tiktok-Signature` (HMAC-SHA256 de `timestamp.corpo` com o segredo do app) é verificada antes de qualquer processamento, com tolerância de 5 minutos. O evento é colocado na fila e a resposta é imediata.
// @Tags			TikTok
// @Accept			json
// @Success		200
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		413	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Router			/webhooks/tiktok [post]
func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil {
		h.metrics.AddTikTokEvent("webhook", "unreadable")
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return
	}
	if len(raw) > maxWebhookBody {
		h.metrics.AddTikTokEvent("webhook", "too_large")
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		h.metrics.AddTikTokEvent("webhook", "not_json")
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := webhookauth.VerifyTimestampedHMAC(h.secret, r.Header.Get(signatureHeader), compact.Bytes(), h.now(), signatureTolerance); err != nil {
		h.metrics.AddTikTokEvent("webhook", signatureOutcome(err))
		log.Printf("[tiktok][webhook] refused delivery %s: %v", r.Header.Get("x-tt-logid"), err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	env, err := ttdomain.DecodeEnvelope(raw)
	if err != nil {
		h.metrics.AddTikTokEvent("webhook", "invalid_envelope")
		http.Error(w, "invalid envelope", http.StatusBadRequest)
		return
	}
	topic, handled := ttdomain.TopicFor(env.Event)
	if !handled {
		h.metrics.AddTikTokEvent("webhook", "ignored_event")
		w.WriteHeader(http.StatusOK)
		return
	}
	payload, err := json.Marshal(env)
	if err != nil {
		http.Error(w, "encode failed", http.StatusInternalServerError)
		return
	}
	if err := h.publisher.Publish(topic, payload); err != nil {
		h.metrics.AddTikTokEvent("webhook", "publish_failed")
		log.Printf("[tiktok][webhook] publish to %s failed, TikTok will retry: %v", topic, err)
		http.Error(w, "queue unavailable", http.StatusServiceUnavailable)
		return
	}
	h.metrics.AddTikTokEvent("webhook", "accepted")
	w.WriteHeader(http.StatusOK)
}

func signatureOutcome(err error) string {
	switch {
	case errors.Is(err, webhookauth.ErrSignatureStale):
		return "signature_stale"
	case errors.Is(err, webhookauth.ErrSignatureMalformed):
		return "signature_malformed"
	}
	return "signature_mismatch"
}
