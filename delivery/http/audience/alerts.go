package audience

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	ca "vozko/domain/audience"
	"vozko/infra/http/middleware"
	cauc "vozko/usecases/audience"
)

// The alert rules API.
//
// Every route here is behind audience:send rather than update, and the
// distinction is the point: configuring an automated sender IS granting sends.
// Somebody who may tune the classifier must not thereby be able to arm a rule
// that messages a phone number in the workspace's name.

// AlertRuleRequest is the wire shape of a rule. The firing history is absent by
// design: it is the server's, and a client that could set it would be able to
// skip a cooldown or silence a rule for the day.
type AlertRuleRequest struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`

	Source    string `json:"source,omitempty"`
	AccountID string `json:"accountId,omitempty"`

	Metric        string `json:"metric"`
	Threshold     int    `json:"threshold"`
	WindowMinutes int    `json:"windowMinutes,omitempty"`

	Channel         string `json:"channel"`
	Recipient       string `json:"recipient"`
	BusinessPhoneID string `json:"businessPhoneId,omitempty"`
	TemplateID      string `json:"templateId,omitempty"`
	InstanceID      string `json:"instanceId,omitempty"`

	Brief           bool `json:"brief,omitempty"`
	CooldownMinutes int  `json:"cooldownMinutes,omitempty"`
	MaxPerDay       int  `json:"maxPerDay,omitempty"`
}

func (r AlertRuleRequest) toDomain() ca.AlertRule {
	return ca.AlertRule{
		Name:            r.Name,
		Enabled:         r.Enabled,
		Source:          ca.Source(strings.TrimSpace(r.Source)),
		AccountID:       r.AccountID,
		Metric:          ca.AlertMetric(strings.TrimSpace(r.Metric)),
		Threshold:       r.Threshold,
		WindowMinutes:   r.WindowMinutes,
		Channel:         ca.AlertChannel(strings.TrimSpace(r.Channel)),
		Recipient:       r.Recipient,
		BusinessPhoneID: r.BusinessPhoneID,
		TemplateID:      r.TemplateID,
		InstanceID:      r.InstanceID,
		Brief:           r.Brief,
		CooldownMinutes: r.CooldownMinutes,
		MaxPerDay:       r.MaxPerDay,
	}
}

// @Summary	Listar regras de alerta
// @Tags		Analysis
// @Produce	json
// @Success	200	{array}	audience.AlertRule
// @Security	BearerAuth
// @Router		/comment-analysis/alerts [get]
func (h *Handler) ListAlertRules(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	rules, err := h.alerts.List(r.Context(), middleware.GetWorkspaceID(r),
		ca.Source(strings.TrimSpace(v.Get("source"))), strings.TrimSpace(v.Get("accountId")))
	if err != nil {
		writeDomainError(w, err, "Failed to list alert rules")
		return
	}
	response.WriteSuccess(w, http.StatusOK, rules)
}

// @Summary	Criar uma regra de alerta
// @Tags		Analysis
// @Accept		json
// @Produce	json
// @Success	201	{object}	audience.AlertRule
// @Security	BearerAuth
// @Router		/comment-analysis/alerts [post]
func (h *Handler) CreateAlertRule(w http.ResponseWriter, r *http.Request) {
	var req AlertRuleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rule := req.toDomain()
	rule.WorkspaceID = middleware.GetWorkspaceID(r)
	rule.CreatedByUserID = actingUserID(r)

	created, err := h.alerts.Create(r.Context(), rule)
	if err != nil {
		writeDomainError(w, err, "Failed to create the alert rule")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, created)
}

// @Summary	Editar uma regra de alerta
// @Tags		Analysis
// @Accept		json
// @Produce	json
// @Param		id	path	string	true	"ID da regra"
// @Success	200	{object}	audience.AlertRule
// @Security	BearerAuth
// @Router		/comment-analysis/alerts/{id} [put]
func (h *Handler) UpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	var req AlertRuleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := h.alerts.Update(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], req.toDomain())
	if err != nil {
		writeDomainError(w, err, "Failed to update the alert rule")
		return
	}
	response.WriteSuccess(w, http.StatusOK, updated)
}

// @Summary	Remover uma regra de alerta
// @Tags		Analysis
// @Produce	json
// @Param		id	path	string	true	"ID da regra"
// @Success	204
// @Security	BearerAuth
// @Router		/comment-analysis/alerts/{id} [delete]
func (h *Handler) DeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	if err := h.alerts.Delete(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"]); err != nil {
		writeDomainError(w, err, "Failed to delete the alert rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary	Disparar uma regra de alerta para teste
// @Tags		Analysis
// @Produce	json
// @Param		id	path	string	true	"ID da regra"
// @Success	202
// @Security	BearerAuth
// @Router		/comment-analysis/alerts/{id}/test [post]
func (h *Handler) TestAlertRule(w http.ResponseWriter, r *http.Request) {
	if h.testAlert == nil {
		writeDomainError(w, ca.ErrInvalidFilter, "Alerts are not configured in this deployment")
		return
	}
	err := h.testAlert.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], actingUserID(r))
	if err != nil {
		writeDomainError(w, err, "Failed to send the test alert")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// @Summary	Vocabulário de alertas (métricas e canais)
// @Tags		Analysis
// @Produce	json
// @Success	200	{object}	AlertVocabularyResponse
// @Security	BearerAuth
// @Router		/comment-analysis/alerts/options [get]
func (h *Handler) AlertOptions(w http.ResponseWriter, r *http.Request) {
	metrics := make([]AlertMetricOption, 0, len(ca.AllAlertMetrics()))
	for _, m := range ca.AllAlertMetrics() {
		metrics = append(metrics, AlertMetricOption{
			Metric:   string(m),
			Windowed: m.IsWindowed(),
			Below:    m.TriggersWhenBelow(),
		})
	}
	// Channels is the product's VOCABULARY, unchanged: what an alert can be
	// written against. ChannelStatus is what THIS workspace can actually send
	// on right now, which is a different question and the one that was never
	// asked. Kept as two fields rather than one so existing clients keep
	// working off the list they already read.
	channelStatus := make([]AlertChannelStatusResponse, 0, 2)
	if h.channels != nil {
		statuses, err := h.channels.Execute(r.Context(), middleware.GetWorkspaceID(r))
		if err != nil {
			writeDomainError(w, err, "Failed to read the alert channels")
			return
		}
		for _, s := range statuses {
			senders := make([]AlertSenderResponse, 0, len(s.Senders))
			for _, snd := range s.Senders {
				senders = append(senders, AlertSenderResponse{ID: snd.ID, Label: snd.Label})
			}
			channelStatus = append(channelStatus, AlertChannelStatusResponse{
				Channel:   string(s.Channel),
				Available: s.Available,
				Reason:    s.Reason,
				Senders:   senders,
			})
		}
	}

	response.WriteSuccess(w, http.StatusOK, AlertVocabularyResponse{
		Metrics:       metrics,
		Channels:      []string{string(ca.AlertChannelOfficial), string(ca.AlertChannelUnofficial)},
		ChannelStatus: channelStatus,
		Facts:         ca.AlertFactKeys(),
		Limits: AlertLimitsResponse{
			MinCooldownMinutes:     ca.MinAlertCooldownMinutes,
			DefaultCooldownMinutes: ca.DefaultAlertCooldownMinutes,
			MaxCooldownMinutes:     ca.MaxAlertCooldownMinutes,
			DefaultPerDay:          ca.DefaultAlertsPerDay,
			MaxPerDay:              ca.MaxAlertsPerDay,
			MinWindowMinutes:       ca.MinAlertWindowMinutes,
			DefaultWindowMinutes:   ca.DefaultAlertWindowMinutes,
			MaxWindowMinutes:       ca.MaxAlertWindowMinutes,
			TemplateParamCount:     ca.AlertTemplateParamCount,
		},
	})
}

// AlertMetricOption tells a client how to render one metric: whether it needs a
// window, and which way it alarms. Sent rather than hardcoded so the picker and
// the evaluator cannot disagree about what a metric means.
type AlertMetricOption struct {
	Metric   string `json:"metric"`
	Windowed bool   `json:"windowed"`
	Below    bool   `json:"triggersWhenBelow"`
}

type AlertLimitsResponse struct {
	MinCooldownMinutes     int `json:"minCooldownMinutes"`
	DefaultCooldownMinutes int `json:"defaultCooldownMinutes"`
	MaxCooldownMinutes     int `json:"maxCooldownMinutes"`
	DefaultPerDay          int `json:"defaultPerDay"`
	MaxPerDay              int `json:"maxPerDay"`
	MinWindowMinutes       int `json:"minWindowMinutes"`
	DefaultWindowMinutes   int `json:"defaultWindowMinutes"`
	MaxWindowMinutes       int `json:"maxWindowMinutes"`
	// TemplateParamCount is how many facts an alert can supply. NOT a
	// requirement on the template: one that declares fewer gets the first few,
	// one that declares more has the rest padded, and one with no variables is
	// perfectly fine.
	TemplateParamCount int `json:"templateParamCount"`
}

// AlertSenderResponse is one number a channel can send from.
type AlertSenderResponse struct {
	ID string `json:"id"`
	// Label is what the operator recognises, never an internal id.
	Label string `json:"label"`
}

// AlertChannelStatusResponse is whether THIS workspace can use a channel now.
//
// The list beside it (Channels) is the product vocabulary and says nothing
// about the tenant. Serving only that was the bug: a workspace with no
// connected number was offered the unofficial channel, accepted, and armed a
// rule that could never fire.
type AlertChannelStatusResponse struct {
	Channel   string `json:"channel"`
	Available bool   `json:"available"`
	// Reason is a stable key the client translates: "no_sender",
	// "not_enabled". A greyed control with no explanation sends people to
	// support instead of to the connect screen.
	Reason  string                `json:"reason,omitempty"`
	Senders []AlertSenderResponse `json:"senders"`
}

type AlertVocabularyResponse struct {
	Metrics  []AlertMetricOption `json:"metrics"`
	Channels []string            `json:"channels"`
	// ChannelStatus narrows Channels to what this workspace can actually do.
	// Empty when the deployment has no directory wired, in which case the
	// client falls back to Channels exactly as it did before.
	ChannelStatus []AlertChannelStatusResponse `json:"channelStatus"`
	Limits        AlertLimitsResponse          `json:"limits"`
	// Facts is what an alert can put into a template's variables, in the order
	// a POSITIONAL template is filled. Sent so the settings screen can tell an
	// operator what their template will actually receive, and so a NAMED
	// template can be written with variables we recognise.
	Facts []string `json:"facts"`
}

// actingUserID is who the request is from, for attribution of anything the
// rule later sends.
func actingUserID(r *http.Request) string {
	if claims := middleware.GetClaims(r); claims != nil {
		return claims.UserID
	}
	return ""
}

// compile-time reminder that the handler's alert deps are the use cases above.
var (
	_ = (*cauc.ManageAlertRulesUseCase)(nil)
	_ = (*cauc.TestAlertRuleUseCase)(nil)
)
