package commentanalysis

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	ca "vozko/domain/comment_analysis"
	workspace_domain "vozko/domain/workspace"
)

// The audience surface.
//
// Same engine, same rows, same use cases as the comment endpoints; the only
// difference is which subject kinds a request is allowed to see. The comment
// routes pin themselves to comments so nothing on those screens changes; these
// routes serve every kind unless asked to narrow, which is what makes an
// audience view possible across channels rather than per channel.
//
// Deliberately NOT a second handler with its own list and stats logic: the feed
// and the numbers above it have to describe the same rows, and that only stays
// true while one filter builder feeds both.

// audienceInput builds the filter for the audience routes.
//
// The kind comes from the query and defaults to EVERY kind. That default is the
// whole point of the surface: an operator asking "what is my audience saying"
// means comments and conversations, not one of them.
func audienceInput(r *http.Request) ca.ListInput {
	in := listInput(r)
	in.SubjectKinds = parseSubjectKinds(r.URL.Query().Get("subjectKind"))

	v := r.URL.Query()
	in.Interest = ca.Interest(strings.TrimSpace(v.Get("interest")))
	in.Disposition = ca.Disposition(strings.TrimSpace(v.Get("disposition")))
	in.Qualification = ca.Qualification(strings.TrimSpace(v.Get("qualification")))
	in.NextAction = ca.NextAction(strings.TrimSpace(v.Get("nextAction")))
	in.SubjectID = strings.TrimSpace(v.Get("subjectId"))
	return in
}

// parseSubjectKinds reads a comma-separated list. Unknown values are kept
// rather than dropped, so Validate refuses them with a message instead of the
// request silently matching everything.
func parseSubjectKinds(raw string) []ca.SubjectKind {
	var kinds []ca.SubjectKind
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			kinds = append(kinds, ca.SubjectKind(part))
		}
	}
	return kinds
}

// @Summary	Feed da audiência: comentários e conversas
// @Tags		Audience
// @Produce	json
// @Param		subjectKind	query	string	false	"comment|conversation (vazio = todos)"
// @Success	200	{array}	CommentResponse
// @Router		/audience [get]
func (h *Handler) AudienceList(w http.ResponseWriter, r *http.Request) {
	h.listFiltered(w, r, audienceInput(r))
}

// @Summary	Estatísticas da audiência
// @Tags		Audience
// @Produce	json
// @Param		subjectKind	query	string	false	"comment|conversation (vazio = todos)"
// @Success	200	{object}	StatsResponse
// @Router		/audience/stats [get]
func (h *Handler) AudienceStats(w http.ResponseWriter, r *http.Request) {
	h.statsFiltered(w, r, audienceInput(r))
}

// RegisterAudienceRoutes wires the channel-agnostic audience surface.
//
// It reuses the comment_analysis resource rather than inventing a second
// permission: it reads exactly the same rows, and a workspace that may read its
// comment analysis may read the conversations beside them.
func RegisterAudienceRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}
	res, read := workspace_domain.ResourceCommentAnalysis, workspace_domain.ActionRead

	r := protected.PathPrefix("/audience").Subrouter()
	r.HandleFunc("", ac(res, read, h.AudienceList)).Methods(http.MethodGet)
	r.HandleFunc("/stats", ac(res, read, h.AudienceStats)).Methods(http.MethodGet)
	// Trends, authors and spend are already channel-agnostic reads of the same
	// tables, so they are served from the comment-analysis prefix rather than
	// duplicated here.
}
