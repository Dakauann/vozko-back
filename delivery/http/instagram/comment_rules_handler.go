package instagram

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	igdomain "vozko/domain/instagram"
	"vozko/infra/http/middleware"
)

// CommentRuleRequest is the create/update payload for a comment automation.
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

func (r CommentRuleRequest) toDomain(workspaceID, accountID, id string) *igdomain.CommentRule {
	actions := make([]igdomain.CommentRuleAction, 0, len(r.Actions))
	for _, a := range r.Actions {
		actions = append(actions, igdomain.CommentRuleAction(a))
	}
	return &igdomain.CommentRule{
		ID:               id,
		WorkspaceID:      workspaceID,
		IGAccountID:      accountID,
		Name:             r.Name,
		Enabled:          r.Enabled,
		IGMediaID:        r.IGMediaID,
		Match:            igdomain.CommentRuleMatch(r.Match),
		Keywords:         r.Keywords,
		Actions:          actions,
		PublicReplyText:  r.PublicReplyText,
		PrivateReplyText: r.PrivateReplyText,
		Priority:         r.Priority,
	}
}

// rulesReady guards every comment-rule endpoint.
//
// A nil usecase means the channel was wired without rule support. Reporting it
// as unavailable keeps a configuration mistake to one failing endpoint instead
// of a nil dereference that panics the request goroutine.
func (h *Handler) rulesReady(w http.ResponseWriter) bool {
	if h == nil || h.manageRules == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Comment rules are not available", nil)
		return false
	}
	return true
}

// @Summary	Listar regras de automação de comentários
// @Tags		Instagram
// @Security	BearerAuth
// @Router		/instagram/accounts/{id}/comment-rules [get]
func (h *Handler) ListCommentRules(w http.ResponseWriter, r *http.Request) {
	if !h.rulesReady(w) {
		return
	}
	rules, err := h.manageRules.List(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to list comment rules")
		return
	}
	response.WriteSuccess(w, http.StatusOK, rules)
}

// @Summary	Criar regra de automação de comentários
// @Tags		Instagram
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
	response.WriteSuccess(w, http.StatusCreated, rule)
}

// @Summary	Atualizar regra de automação de comentários
// @Tags		Instagram
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
	response.WriteSuccess(w, http.StatusOK, rule)
}

// @Summary	Remover regra de automação de comentários
// @Tags		Instagram
// @Security	BearerAuth
// @Router		/instagram/accounts/{id}/comment-rules/{ruleId} [delete]
func (h *Handler) DeleteCommentRule(w http.ResponseWriter, r *http.Request) {
	if !h.rulesReady(w) {
		return
	}
	if err := h.manageRules.Delete(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["ruleId"]); err != nil {
		writeDomainError(w, err, "Failed to delete comment rule")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "deleted"})
}
