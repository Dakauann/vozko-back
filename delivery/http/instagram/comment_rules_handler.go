package instagram

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	ca "vozko/domain/commentautomation"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
)

type CommentRuleRequest struct {
	Name             string   `json:"name"`
	Enabled          bool     `json:"enabled"`
	IGMediaID        string   `json:"igMediaId"`
	Match            string   `json:"match"`
	Keywords         []string `json:"keywords"`
	Actions          []string `json:"actions"`
	PublicReplyText  string   `json:"publicReplyText"`
	PrivateReplyText string   `json:"privateReplyText"`
	Priority         int      `json:"priority"`
}

func (r CommentRuleRequest) toDomain(workspaceID, accountID, id string) *ca.Rule {
	actions := make([]ca.Action, 0, len(r.Actions))
	for _, a := range r.Actions {
		actions = append(actions, ca.Action(a))
	}
	return &ca.Rule{
		ID:               id,
		WorkspaceID:      workspaceID,
		Source:           shared.EntryTypeInstagram,
		AccountID:        accountID,
		Name:             r.Name,
		Enabled:          r.Enabled,
		ContainerID:      r.IGMediaID,
		Match:            ca.Match(r.Match),
		Keywords:         r.Keywords,
		Actions:          actions,
		PublicReplyText:  r.PublicReplyText,
		PrivateReplyText: r.PrivateReplyText,
		Priority:         r.Priority,
	}
}

type CommentRuleResponse struct {
	ID               string    `json:"id"`
	WorkspaceID      string    `json:"workspaceId"`
	IGAccountID      string    `json:"igAccountId"`
	Name             string    `json:"name"`
	Enabled          bool      `json:"enabled"`
	IGMediaID        string    `json:"igMediaId,omitempty"`
	Match            string    `json:"match"`
	Keywords         []string  `json:"keywords"`
	Actions          []string  `json:"actions"`
	PublicReplyText  string    `json:"publicReplyText,omitempty"`
	PrivateReplyText string    `json:"privateReplyText,omitempty"`
	Priority         int       `json:"priority"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func presentCommentRule(rule *ca.Rule) CommentRuleResponse {
	actions := make([]string, 0, len(rule.Actions))
	for _, a := range rule.Actions {
		actions = append(actions, string(a))
	}
	return CommentRuleResponse{
		ID: rule.ID, WorkspaceID: rule.WorkspaceID, IGAccountID: rule.AccountID, Name: rule.Name, Enabled: rule.Enabled,
		IGMediaID: rule.ContainerID, Match: string(rule.Match), Keywords: rule.Keywords, Actions: actions,
		PublicReplyText: rule.PublicReplyText, PrivateReplyText: rule.PrivateReplyText, Priority: rule.Priority,
		CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt,
	}
}

func presentCommentRules(rules []*ca.Rule) []CommentRuleResponse {
	out := make([]CommentRuleResponse, 0, len(rules))
	for _, rule := range rules {
		out = append(out, presentCommentRule(rule))
	}
	return out
}

func (h *Handler) rulesReady(w http.ResponseWriter) bool {
	if h == nil || h.manageRules == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Comment rules are not available", nil)
		return false
	}
	return true
}

// @Summary	Listar regras de automação de comentários
// @Tags		Instagram
// @Param		id	path	string	true	"ID da conta do Instagram"
// @Produce	json
// @Success	200	{array}	map[string]interface{}
// @Security	BearerAuth
// @Router		/instagram/accounts/{id}/comment-rules [get]
func (h *Handler) ListCommentRules(w http.ResponseWriter, r *http.Request) {
	if !h.rulesReady(w) {
		return
	}
	rules, err := h.manageRules.List(r.Context(), middleware.GetWorkspaceID(r), shared.EntryTypeInstagram, mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to list comment rules")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentCommentRules(rules))
}

// @Summary	Criar regra de automação de comentários
// @Tags		Instagram
// @Param		id	path	string	true	"ID da conta do Instagram"
// @Accept		json
// @Produce	json
// @Success	201	{object}	map[string]interface{}
// @Security	BearerAuth
// @Router		/instagram/accounts/{id}/comment-rules [post]
func (h *Handler) CreateCommentRule(w http.ResponseWriter, r *http.Request) {
	if !h.rulesReady(w) {
		return
	}
	var req CommentRuleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rule, err := h.manageRules.Create(
		r.Context(),
		req.toDomain(middleware.GetWorkspaceID(r), mux.Vars(r)["id"], ""),
	)
	if err != nil {
		writeDomainError(w, err, "Failed to create comment rule")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentCommentRule(rule))
}

// @Summary	Atualizar regra de automação de comentários
// @Tags		Instagram
// @Param		id	path	string	true	"ID da conta do Instagram"
// @Param		ruleId	path	string	true	"ID da regra"
// @Accept		json
// @Produce	json
// @Success	200	{object}	map[string]interface{}
// @Security	BearerAuth
// @Router		/instagram/accounts/{id}/comment-rules/{ruleId} [put]
func (h *Handler) UpdateCommentRule(w http.ResponseWriter, r *http.Request) {
	if !h.rulesReady(w) {
		return
	}
	var req CommentRuleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	vars := mux.Vars(r)
	rule, err := h.manageRules.Update(
		r.Context(),
		req.toDomain(middleware.GetWorkspaceID(r), vars["id"], vars["ruleId"]),
	)
	if err != nil {
		writeDomainError(w, err, "Failed to update comment rule")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentCommentRule(rule))
}

// @Summary	Remover regra de automação de comentários
// @Tags		Instagram
// @Param		id	path	string	true	"ID da conta do Instagram"
// @Param		ruleId	path	string	true	"ID da regra"
// @Produce	json
// @Success	200	{object}	map[string]string
// @Security	BearerAuth
// @Router		/instagram/accounts/{id}/comment-rules/{ruleId} [delete]
func (h *Handler) DeleteCommentRule(w http.ResponseWriter, r *http.Request) {
	if !h.rulesReady(w) {
		return
	}
	vars := mux.Vars(r)
	if err := h.manageRules.Delete(r.Context(), middleware.GetWorkspaceID(r), shared.EntryTypeInstagram, vars["id"], vars["ruleId"]); err != nil {
		writeDomainError(w, err, "Failed to delete comment rule")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "deleted"})
}
