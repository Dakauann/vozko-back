package commentautomationhttp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"vozko/delivery/http/response"
	ca "vozko/domain/commentautomation"
	"vozko/domain/shared"
)

const maxRuleBody = 64 << 10

type RuleManager interface {
	List(ctx context.Context, workspaceID string, source shared.EntryType, accountID string) ([]*ca.Rule, error)
	Create(ctx context.Context, rule *ca.Rule) (*ca.Rule, error)
	Update(ctx context.Context, rule *ca.Rule) (*ca.Rule, error)
	Delete(ctx context.Context, workspaceID string, source shared.EntryType, accountID, id string) error
}

type OtherErrors func(w http.ResponseWriter, err error, fallback string)

type RulesHandler struct {
	source shared.EntryType
	rules  RuleManager
	other  OtherErrors
}

func NewRulesHandler(source shared.EntryType, rules RuleManager, other OtherErrors) *RulesHandler {
	if !source.IsKnown() || rules == nil || other == nil {
		panic("comment rules http: a known source, the rule manager and an error writer are required")
	}
	return &RulesHandler{source: source, rules: rules, other: other}
}

func (h *RulesHandler) fail(w http.ResponseWriter, err error, fallback string) {
	if !WriteError(w, err) {
		h.other(w, err, fallback)
	}
}

func (h *RulesHandler) decode(w http.ResponseWriter, r *http.Request) (RuleRequest, bool) {
	var req RuleRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRuleBody)).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return req, false
	}
	return req, true
}

func (h *RulesHandler) toRule(req RuleRequest, workspaceID, accountID, id string) *ca.Rule {
	actions := make([]ca.Action, 0, len(req.Actions))
	for _, a := range req.Actions {
		actions = append(actions, ca.Action(a))
	}
	return &ca.Rule{
		ID: id, WorkspaceID: workspaceID, Source: h.source, AccountID: accountID, ContainerID: req.ContainerID,
		Name: req.Name, Enabled: req.Enabled, Match: ca.Match(req.Match), Keywords: req.Keywords, Actions: actions,
		PublicReplyText: req.PublicReplyText, PrivateReplyText: req.PrivateReplyText, Priority: req.Priority,
	}
}

func PresentRule(rule *ca.Rule) RuleResponse {
	actions := make([]string, 0, len(rule.Actions))
	for _, a := range rule.Actions {
		actions = append(actions, string(a))
	}
	keywords := rule.Keywords
	if keywords == nil {
		keywords = []string{}
	}
	return RuleResponse{
		ID: rule.ID, AccountID: rule.AccountID, Source: string(rule.Source), Name: rule.Name, Enabled: rule.Enabled,
		ContainerID: rule.ContainerID, Match: string(rule.Match), Keywords: keywords, Actions: actions,
		PublicReplyText: rule.PublicReplyText, PrivateReplyText: rule.PrivateReplyText, Priority: rule.Priority,
		CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt,
	}
}

func (h *RulesHandler) List(w http.ResponseWriter, r *http.Request, workspaceID, accountID string) {
	rules, err := h.rules.List(r.Context(), workspaceID, h.source, accountID)
	if err != nil {
		h.fail(w, err, "Failed to list comment rules")
		return
	}
	out := make([]RuleResponse, 0, len(rules))
	for _, rule := range rules {
		out = append(out, PresentRule(rule))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *RulesHandler) Create(w http.ResponseWriter, r *http.Request, workspaceID, accountID string) {
	req, ok := h.decode(w, r)
	if !ok {
		return
	}
	rule, err := h.rules.Create(r.Context(), h.toRule(req, workspaceID, accountID, ""))
	if err != nil {
		h.fail(w, err, "Failed to create the comment rule")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, PresentRule(rule))
}

func (h *RulesHandler) Update(w http.ResponseWriter, r *http.Request, workspaceID, accountID, ruleID string) {
	req, ok := h.decode(w, r)
	if !ok {
		return
	}
	rule, err := h.rules.Update(r.Context(), h.toRule(req, workspaceID, accountID, ruleID))
	if err != nil {
		h.fail(w, err, "Failed to update the comment rule")
		return
	}
	response.WriteSuccess(w, http.StatusOK, PresentRule(rule))
}

func (h *RulesHandler) Delete(w http.ResponseWriter, r *http.Request, workspaceID, accountID, ruleID string) {
	if err := h.rules.Delete(r.Context(), workspaceID, h.source, accountID, ruleID); err != nil {
		h.fail(w, err, "Failed to delete the comment rule")
		return
	}
	response.WriteSuccess(w, http.StatusOK, RuleDeletedResponse{Status: "deleted"})
}
