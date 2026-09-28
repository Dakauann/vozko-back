package facebook

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	ca "vozko/domain/commentautomation"
	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	fbuc "vozko/usecases/facebook"
)

type CommentAuthorResponse struct {
	ID        string  `json:"id,omitempty"`
	Name      string  `json:"name,omitempty"`
	IsPage    bool    `json:"isPage"`
	ContactID *string `json:"contactId,omitempty"`
}

type CommentPrivateReplyResponse struct {
	Status   string     `json:"status"`
	Deadline *time.Time `json:"deadline,omitempty"`
}

type CommentAttachmentResponse struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

type CommentResponse struct {
	ID                string                      `json:"id"`
	ParentID          string                      `json:"parentId,omitempty"`
	Message           string                      `json:"message"`
	CreatedTime       *time.Time                  `json:"createdTime,omitempty"`
	From              *CommentAuthorResponse      `json:"from"`
	LikeCount         int                         `json:"likeCount"`
	ReplyCount        int                         `json:"replyCount"`
	IsHidden          bool                        `json:"isHidden"`
	IsOurs            bool                        `json:"isOurs"`
	LikedByPage       bool                        `json:"likedByPage"`
	CanHide           bool                        `json:"canHide"`
	CanRemove         bool                        `json:"canRemove"`
	CanReplyPrivately bool                        `json:"canReplyPrivately"`
	CanLike           bool                        `json:"canLike"`
	CanEdit           bool                        `json:"canEdit"`
	PrivateReply      CommentPrivateReplyResponse `json:"privateReply"`
	Attachment        *CommentAttachmentResponse  `json:"attachment,omitempty"`
}

type CommentListResponse struct {
	Items      []CommentResponse `json:"items"`
	NextCursor string            `json:"nextCursor,omitempty"`
	HasNext    bool              `json:"hasNext"`
}

type CommentTextBody struct {
	Message string `json:"message"`
}

type PrivateReplyBody struct {
	Text string `json:"text"`
}

type HiddenBody struct {
	Hidden bool `json:"hidden"`
}

type LikedBody struct {
	Liked bool `json:"liked"`
}

type CommentRuleBody struct {
	Name             string   `json:"name"`
	Enabled          bool     `json:"enabled"`
	PostID           string   `json:"postId"`
	Match            string   `json:"match"`
	Keywords         []string `json:"keywords"`
	Actions          []string `json:"actions"`
	PublicReplyText  string   `json:"publicReplyText"`
	PrivateReplyText string   `json:"privateReplyText"`
	Priority         int      `json:"priority"`
}

type CommentRuleResponse struct {
	ID               string    `json:"id"`
	PageID           string    `json:"pageId"`
	Name             string    `json:"name"`
	Enabled          bool      `json:"enabled"`
	PostID           string    `json:"postId,omitempty"`
	Match            string    `json:"match"`
	Keywords         []string  `json:"keywords"`
	Actions          []string  `json:"actions"`
	PublicReplyText  string    `json:"publicReplyText,omitempty"`
	PrivateReplyText string    `json:"privateReplyText,omitempty"`
	Priority         int       `json:"priority"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func presentComment(pageFBID string, view *fbuc.CommentView) CommentResponse {
	c := view.Remote
	out := CommentResponse{
		ID: c.FBCommentID, ParentID: c.ParentID, Message: c.Message, CreatedTime: c.CreatedTime,
		LikeCount: c.LikeCount, ReplyCount: c.CommentCount, IsHidden: c.IsHidden, LikedByPage: c.UserLikes,
		CanHide: c.CanHide, CanRemove: c.CanRemove, CanReplyPrivately: c.CanReplyPrivately, CanLike: c.CanLike,
		PrivateReply: CommentPrivateReplyResponse{Status: "NONE", Deadline: view.Deadline},
	}
	if c.FromID != "" {
		isPage := c.FromID == pageFBID
		out.From = &CommentAuthorResponse{ID: c.FromID, Name: c.FromName, IsPage: isPage, ContactID: view.ContactID}
		out.IsOurs, out.CanEdit = isPage, isPage
	}
	if view.PrivateReply != nil {
		out.PrivateReply.Status = string(view.PrivateReply.Status)
		out.CanReplyPrivately = out.CanReplyPrivately && !view.PrivateReply.Consumed()
	}
	if c.AttachmentType != "" {
		out.Attachment = &CommentAttachmentResponse{Type: c.AttachmentType, URL: c.AttachmentURL}
	}
	return out
}

func presentRule(rule *ca.Rule) CommentRuleResponse {
	actions := make([]string, 0, len(rule.Actions))
	for _, a := range rule.Actions {
		actions = append(actions, string(a))
	}
	return CommentRuleResponse{
		ID: rule.ID, PageID: rule.AccountID, Name: rule.Name, Enabled: rule.Enabled, PostID: rule.ContainerID,
		Match: string(rule.Match), Keywords: rule.Keywords, Actions: actions,
		PublicReplyText: rule.PublicReplyText, PrivateReplyText: rule.PrivateReplyText, Priority: rule.Priority,
		CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt,
	}
}

func (b CommentRuleBody) toRule(workspaceID, pageID, id string) *ca.Rule {
	actions := make([]ca.Action, 0, len(b.Actions))
	for _, a := range b.Actions {
		actions = append(actions, ca.Action(a))
	}
	return &ca.Rule{
		ID: id, WorkspaceID: workspaceID, Source: shared.EntryTypeFacebook, AccountID: pageID, ContainerID: b.PostID,
		Name: b.Name, Enabled: b.Enabled, Match: ca.Match(b.Match), Keywords: b.Keywords, Actions: actions,
		PublicReplyText: b.PublicReplyText, PrivateReplyText: b.PrivateReplyText, Priority: b.Priority,
	}
}

func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	vars, q := mux.Vars(r), r.URL.Query()
	filter := fbdomain.CommentsStream
	if q.Get("filter") == string(fbdomain.CommentsTopLevel) {
		filter = fbdomain.CommentsTopLevel
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	ctx, ws := r.Context(), middleware.GetWorkspaceID(r)
	page, err := h.pages.Get(ctx, ws, vars["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to load the Facebook page")
		return
	}
	list, err := h.comments.List(ctx, ws, vars["id"], vars["postId"], filter, limit, q.Get("after"))
	if err != nil {
		writeDomainError(w, err, "Failed to list comments")
		return
	}
	out := CommentListResponse{Items: make([]CommentResponse, 0, len(list.Items)), NextCursor: list.NextCursor, HasNext: list.HasNext}
	for _, item := range list.Items {
		out.Items = append(out.Items, presentComment(page.FBPageID, item))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *Handler) CommentOnPost(w http.ResponseWriter, r *http.Request) {
	var body CommentTextBody
	if !decodeJSON(w, r, &body) {
		return
	}
	vars := mux.Vars(r)
	id, err := h.comments.CommentAsPage(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["postId"], body.Message)
	if err != nil {
		writeDomainError(w, err, "Failed to comment")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) ReplyToComment(w http.ResponseWriter, r *http.Request) {
	var body CommentTextBody
	if !decodeJSON(w, r, &body) {
		return
	}
	vars := mux.Vars(r)
	id, err := h.comments.Reply(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["commentId"], body.Message)
	if err != nil {
		writeDomainError(w, err, "Failed to reply to the comment")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) EditComment(w http.ResponseWriter, r *http.Request) {
	var body CommentTextBody
	if !decodeJSON(w, r, &body) {
		return
	}
	vars := mux.Vars(r)
	if err := h.comments.Edit(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["commentId"], body.Message); err != nil {
		writeDomainError(w, err, "Failed to edit the comment")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) HideComment(w http.ResponseWriter, r *http.Request) {
	var body HiddenBody
	if !decodeJSON(w, r, &body) {
		return
	}
	vars := mux.Vars(r)
	if err := h.comments.SetHidden(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["commentId"], body.Hidden); err != nil {
		writeDomainError(w, err, "Failed to change the comment visibility")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) LikeComment(w http.ResponseWriter, r *http.Request) {
	var body LikedBody
	if !decodeJSON(w, r, &body) {
		return
	}
	vars := mux.Vars(r)
	if err := h.comments.SetLiked(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["commentId"], body.Liked); err != nil {
		writeDomainError(w, err, "Failed to like the comment")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if err := h.comments.Delete(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["commentId"]); err != nil {
		writeDomainError(w, err, "Failed to delete the comment")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) SendCommentPrivateReply(w http.ResponseWriter, r *http.Request) {
	var body PrivateReplyBody
	if !decodeJSON(w, r, &body) {
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	vars := mux.Vars(r)
	out, err := h.comments.SendPrivateReply(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["commentId"],
		conversation.SentByPerson(claims.UserID), body.Text)
	if err != nil {
		writeDomainError(w, err, "Failed to send the private reply")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"conversationId": out.ConversationID, "messageId": out.MessageID})
}

func (h *Handler) ListCommentRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.rules.List(r.Context(), middleware.GetWorkspaceID(r), shared.EntryTypeFacebook, mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to list comment rules")
		return
	}
	out := make([]CommentRuleResponse, 0, len(rules))
	for _, rule := range rules {
		out = append(out, presentRule(rule))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *Handler) CreateCommentRule(w http.ResponseWriter, r *http.Request) {
	var body CommentRuleBody
	if !decodeJSON(w, r, &body) {
		return
	}
	rule, err := h.rules.Create(r.Context(), body.toRule(middleware.GetWorkspaceID(r), mux.Vars(r)["id"], ""))
	if err != nil {
		writeDomainError(w, err, "Failed to create the comment rule")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentRule(rule))
}

func (h *Handler) UpdateCommentRule(w http.ResponseWriter, r *http.Request) {
	var body CommentRuleBody
	if !decodeJSON(w, r, &body) {
		return
	}
	vars := mux.Vars(r)
	rule, err := h.rules.Update(r.Context(), body.toRule(middleware.GetWorkspaceID(r), vars["id"], vars["ruleId"]))
	if err != nil {
		writeDomainError(w, err, "Failed to update the comment rule")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentRule(rule))
}

func (h *Handler) DeleteCommentRule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if err := h.rules.Delete(r.Context(), middleware.GetWorkspaceID(r), shared.EntryTypeFacebook, vars["id"], vars["ruleId"]); err != nil {
		writeDomainError(w, err, "Failed to delete the comment rule")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"ok": true})
}
