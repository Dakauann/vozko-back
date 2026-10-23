package emailwebhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

const (
	SNSPath          = "/webhooks/email/sns"
	maxSNSBody       = 256 << 10
	confirmTimeout   = 10 * time.Second
	maxConfirmAnswer = 64 << 10
)

type NotificationIntake interface {
	Accept(messageID, topicARN, message string) error
}

type SubscriptionConfirmer interface {
	Confirm(ctx context.Context, subscribeURL string) error
}

type HTTPConfirmer struct {
	client *http.Client
}

func NewHTTPConfirmer(client *http.Client) *HTTPConfirmer {
	if client == nil {
		client = &http.Client{
			Timeout: confirmTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &HTTPConfirmer{client: client}
}

func (c *HTTPConfirmer) Confirm(ctx context.Context, subscribeURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subscribeURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxConfirmAnswer))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("subscription confirmation status %d", resp.StatusCode)
	}
	return nil
}

type Deps struct {
	Verifier      *Verifier
	AllowedTopics []string
	Intake        NotificationIntake
	Confirmer     SubscriptionConfirmer
}

type Handler struct {
	verifier  *Verifier
	allowed   map[string]struct{}
	intake    NotificationIntake
	confirmer SubscriptionConfirmer
}

func NewHandler(d Deps) *Handler {
	if d.Verifier == nil || d.Intake == nil || d.Confirmer == nil || len(d.AllowedTopics) == 0 {
		panic("email webhooks: verifier, allowed topics, intake and confirmer are required")
	}
	allowed := make(map[string]struct{}, len(d.AllowedTopics))
	for _, t := range d.AllowedTopics {
		if t = strings.TrimSpace(t); t != "" {
			allowed[t] = struct{}{}
		}
	}
	return &Handler{verifier: d.Verifier, allowed: allowed, intake: d.Intake, confirmer: d.Confirmer}
}

func readLimited(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return nil, false
	}
	if int64(len(raw)) > limit {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	return raw, true
}

// @Summary		Receber notificações do provedor de e-mail
// @Description	Endpoint do tópico SNS do provedor de e-mail: eventos de envio, e-mails recebidos e mudanças da conta. Toda mensagem tem a assinatura conferida com o certificado do host SNS da região antes de qualquer leitura; tópicos fora da lista permitida recebem 403. Confirmações de assinatura só são aceitas para o mesmo host SNS.
// @Tags			Webhooks
// @Accept			json
// @Success		200
// @Failure		400
// @Failure		401
// @Failure		403
// @Router			/webhooks/email/sns [post]
func (h *Handler) HandleSNS(w http.ResponseWriter, r *http.Request) {
	raw, ok := readLimited(w, r, maxSNSBody)
	if !ok {
		return
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		http.Error(w, "invalid envelope", http.StatusBadRequest)
		return
	}
	if _, allowed := h.allowed[env.TopicArn]; !allowed {
		log.Printf("[email-webhook] refused message %q from topic %q", env.MessageID, env.TopicArn)
		http.Error(w, "topic not allowed", http.StatusForbidden)
		return
	}
	if err := h.verifier.Verify(r.Context(), env); err != nil {
		if errors.Is(err, ErrUnknownType) {
			http.Error(w, "unknown message type", http.StatusBadRequest)
			return
		}
		log.Printf("[email-webhook] signature check failed for %q: %v", env.MessageID, err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	switch env.Type {
	case typeSubscription:
		if !h.verifier.ValidSubscribeURL(env.SubscribeURL) {
			log.Printf("[email-webhook] subscribe url %q refused", env.SubscribeURL)
			http.Error(w, "invalid subscribe url", http.StatusUnauthorized)
			return
		}
		if err := h.confirmer.Confirm(r.Context(), env.SubscribeURL); err != nil {
			log.Printf("[email-webhook] subscription to %q not confirmed: %v", env.TopicArn, err)
			http.Error(w, "confirmation failed", http.StatusBadGateway)
			return
		}
		log.Printf("[email-webhook] subscription to %q confirmed", env.TopicArn)
	case typeUnsubscription:
		log.Printf("[email-webhook] unsubscribed from %q", env.TopicArn)
	default:
		if err := h.intake.Accept(env.MessageID, env.TopicArn, env.Message); err != nil {
			log.Printf("[email-webhook] could not queue %q: %v", env.MessageID, err)
			http.Error(w, "queue unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func RegisterPublicRoutes(public *mux.Router, h *Handler) {
	if public == nil {
		panic("email webhooks: routes need the router")
	}
	public.HandleFunc(SNSPath, h.HandleSNS).Methods(http.MethodPost)
}
