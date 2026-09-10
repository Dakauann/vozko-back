package commentanalysis

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	ca "vozko/domain/comment_analysis"
	"vozko/infra/http/middleware"
	cauc "vozko/usecases/comment_analysis"
)

// The alert rules API.
//
// Every route here is behind comment_analysis:send rather than update, and the
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
// @Tags		CommentAnalysis
// @Produce	json
// @Success	200	{array}	comment_analysis.AlertRule
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
// @Tags		CommentAnalysis
// @Accept		json
// @Produce	json
// @Success	201	{object}	comment_analysis.AlertRule
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
// @Tags		CommentAnalysis
// @Accept		json
// @Produce	json
// @Param		id	path	string	true	"ID da regra"
// @Success	200	{object}	comment_analysis.AlertRule
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
// @Tags		CommentAnalysis
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
// @Tags		CommentAnalysis
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
// @Tags		CommentAnalysis
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
	response.WriteSuccess(w, http.StatusOK, AlertVocabularyResponse{
		Metrics:  metrics,
		Channels: []string{string(ca.AlertChannelOfficial), string(ca.AlertChannelUnofficial)},
		Facts:    ca.AlertFactKeys(),
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

type AlertVocabularyResponse struct {
	Metrics  []AlertMetricOption `json:"metrics"`
	Channels []string            `json:"channels"`
	Limits   AlertLimitsResponse `json:"limits"`
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
