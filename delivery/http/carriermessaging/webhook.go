package carriermessaging

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/netip"
	"regexp"

	"github.com/gorilla/mux"

	cm "vozko/domain/carriermessaging"
	"vozko/domain/webhook"
	"vozko/infra/http/middleware"
	"vozko/pkg/webhookauth"
	cmuc "vozko/usecases/carriermessaging"
)

const (
	WebhookPath    = "/webhooks/carrier/{provider}/{token}/{kind}"
	maxWebhookBody = 1 << 20
	KindStatus     = "status"
	KindInbound    = "inbound"
	KindEvents     = "events"
)

var (
	webhookTokenShape   = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)
	ErrWebhookTokenWeak = errors.New("carrier webhooks: the path token needs 43 to 128 base64url characters")
)

type ProviderWebhooks interface {
	DecodeStatusReports(contentType string, body []byte) ([]cm.StatusReport, error)
	DecodeInbound(body []byte) (cm.Inbound, error)
	DecodeEvent(body []byte) (cm.SuggestionClick, bool, error)
	FromAllowedSource(ip netip.Addr) bool
}

type WebhookHandler struct {
	token     string
	providers map[string]ProviderWebhooks
	publish   webhook.PublishWebhookUseCase
}

func NewWebhookHandler(token string, providers map[string]ProviderWebhooks, publish webhook.PublishWebhookUseCase) (*WebhookHandler, error) {
	if !webhookTokenShape.MatchString(token) {
		return nil, ErrWebhookTokenWeak
	}
	if len(providers) == 0 || publish == nil {
		return nil, errors.New("carrier webhooks: providers and a publisher are required")
	}
	return &WebhookHandler{token: token, providers: providers, publish: publish}, nil
}

// @Summary		Webhooks de RCS e SMS do provedor
// @Description	Recebe relatórios de status, mensagens recebidas e toques em botões do provedor de RCS e SMS. Só aceita o token secreto do caminho e os IPs publicados pelo provedor; qualquer outra origem recebe 404. Os eventos são normalizados e enfileirados sem acesso ao banco.
// @Tags			RCS e SMS
// @Accept			json
// @Param			provider	path	string	true	"Provedor (cmcom)"
// @Param			token		path	string	true	"Token secreto do caminho (CARRIER_WEBHOOK_TOKEN)"
// @Param			kind		path	string	true	"status, inbound ou events"
// @Success		200
// @Failure		400
// @Failure		404
// @Failure		413
// @Failure		503
// @Router			/webhooks/carrier/{provider}/{token}/{kind} [post]
func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	provider, known := h.providers[vars["provider"]]
	if !known || !webhookauth.ConstantTimeEqual(vars["token"], h.token) || !h.fromProvider(provider, r) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(body) > maxWebhookBody {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	events, topic, err := decode(provider, vars["kind"], r.Header.Get("Content-Type"), body)
	if err != nil {
		log.Printf("[carrier-webhook] %s %s refused: %v", vars["provider"], vars["kind"], err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for _, event := range events {
		raw, err := cmuc.EncodeQueued(event)
		if err == nil {
			err = h.publish.Publish(topic, raw)
		}
		if err != nil {
			log.Printf("[carrier-webhook] %s %s not queued: %v", vars["provider"], vars["kind"], err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) fromProvider(provider ProviderWebhooks, r *http.Request) bool {
	ip, err := netip.ParseAddr(middleware.GetClientIP(r))
	return err == nil && provider.FromAllowedSource(ip)
}

var errUnknownWebhookKind = errors.New("carrier webhooks: unknown webhook kind")

func decode(provider ProviderWebhooks, kind, contentType string, body []byte) ([]any, string, error) {
	switch kind {
	case KindStatus:
		reports, err := provider.DecodeStatusReports(contentType, body)
		if err != nil {
			return nil, "", err
		}
		out := make([]any, 0, len(reports))
		for _, report := range reports {
			out = append(out, report)
		}
		return out, webhook.TopicCarrierStatus, nil
	case KindInbound:
		in, err := provider.DecodeInbound(body)
		if err != nil {
			return nil, "", err
		}
		return []any{in}, webhook.TopicCarrierInbound, nil
	case KindEvents:
		click, ok, err := provider.DecodeEvent(body)
		if err != nil || !ok {
			return nil, webhook.TopicCarrierEvent, err
		}
		return []any{click}, webhook.TopicCarrierEvent, nil
	}
	return nil, "", errUnknownWebhookKind
}
