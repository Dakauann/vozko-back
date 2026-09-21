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

type AlertRuleRequest struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`

	Source    string `json:"source,omitempty"`
	AccountID string `json:"accountId,omitempty"`

	Metric        string `json:"metric"`
	Threshold     int    `json:"threshold"`
	WindowMinutes int    `json:"windowMinutes,omitempty"`
	MinMessages   int    `json:"minMessages,omitempty"`

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
		MinMessages:     r.MinMessages,
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
			Metric:              string(m),
			Windowed:            m.IsWindowed(),
			Below:               m.TriggersWhenBelow(),
			SubjectKind:         string(m.SubjectKind()),
			SupportsMinMessages: !m.IsWindowed() && m.SubjectKind() == ca.SubjectKindConversation,
		})
	}
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
			MaxMinMessages:         ca.MaxAlertMinMessages,
			TemplateParamCount:     ca.AlertTemplateParamCount,
		},
	})
}

type AlertMetricOption struct {
	Metric              string `json:"metric"`
	Windowed            bool   `json:"windowed"`
	Below               bool   `json:"triggersWhenBelow"`
	SubjectKind         string `json:"subjectKind"`
	SupportsMinMessages bool   `json:"supportsMinMessages"`
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
	MaxMinMessages         int `json:"maxMinMessages"`
	TemplateParamCount     int `json:"templateParamCount"`
}

type AlertSenderResponse struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type AlertChannelStatusResponse struct {
	Channel   string                `json:"channel"`
	Available bool                  `json:"available"`
	Reason    string                `json:"reason,omitempty"`
	Senders   []AlertSenderResponse `json:"senders"`
}

type AlertVocabularyResponse struct {
	Metrics       []AlertMetricOption          `json:"metrics"`
	Channels      []string                     `json:"channels"`
	ChannelStatus []AlertChannelStatusResponse `json:"channelStatus"`
	Limits        AlertLimitsResponse          `json:"limits"`
	Facts         []string                     `json:"facts"`
}

func actingUserID(r *http.Request) string {
	if claims := middleware.GetClaims(r); claims != nil {
		return claims.UserID
	}
	return ""
}

var (
	_ = (*cauc.ManageAlertRulesUseCase)(nil)
	_ = (*cauc.TestAlertRuleUseCase)(nil)
)
