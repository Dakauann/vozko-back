package facebook

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	fbdomain "vozko/domain/facebook"
	"vozko/infra/http/middleware"
	fbuc "vozko/usecases/facebook"
)

const postAssetMaxAge = 10 * time.Minute

type PostAttachmentResponse struct {
	MediaType  string `json:"mediaType"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url,omitempty"`
	AssetIndex int    `json:"assetIndex"`
}

type PostResponse struct {
	ID                   string                   `json:"id"`
	Kind                 string                   `json:"kind"`
	Message              string                   `json:"message"`
	Story                string                   `json:"story,omitempty"`
	PermalinkURL         string                   `json:"permalinkUrl,omitempty"`
	CreatedTime          *time.Time               `json:"createdTime,omitempty"`
	UpdatedTime          *time.Time               `json:"updatedTime,omitempty"`
	IsPublished          bool                     `json:"isPublished"`
	ScheduledPublishTime *time.Time               `json:"scheduledPublishTime"`
	IsHidden             bool                     `json:"isHidden"`
	Editable             bool                     `json:"editable"`
	ReactionsCount       int                      `json:"reactionsCount"`
	CommentsCount        int                      `json:"commentsCount"`
	SharesCount          int                      `json:"sharesCount"`
	HasAsset             bool                     `json:"hasAsset"`
	Attachments          []PostAttachmentResponse `json:"attachments"`
}

type PostListResponse struct {
	Items      []PostResponse `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
	HasNext    bool           `json:"hasNext"`
}

type PublishRequestBody struct {
	Kind                 string              `json:"kind"`
	Message              string              `json:"message"`
	Link                 string              `json:"link"`
	Media                []fbdomain.MediaRef `json:"media"`
	VideoTitle           string              `json:"videoTitle"`
	ScheduledPublishTime *time.Time          `json:"scheduledPublishTime"`
}

type UpdatePostBody struct {
	Message              *string    `json:"message"`
	IsHidden             *bool      `json:"isHidden"`
	PublishNow           bool       `json:"publishNow"`
	ScheduledPublishTime *time.Time `json:"scheduledPublishTime"`
}

type PublishJobError struct {
	Reason    string `json:"reason,omitempty"`
	Code      int    `json:"code,omitempty"`
	Subcode   int    `json:"subcode,omitempty"`
	Message   string `json:"message"`
	Ambiguous bool   `json:"ambiguous"`
}

type PublishJobResponse struct {
	ID          string           `json:"id"`
	Kind        string           `json:"kind"`
	Status      string           `json:"status"`
	FBPostID    string           `json:"fbPostId,omitempty"`
	ScheduledAt *time.Time       `json:"scheduledAt,omitempty"`
	Error       *PublishJobError `json:"error"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

func presentPost(view *fbuc.PostView) PostResponse {
	remote := view.Remote
	out := PostResponse{
		ID:                   remote.FBPostID,
		Kind:                 string(view.Kind),
		Message:              remote.Message,
		Story:                remote.Story,
		PermalinkURL:         remote.PermalinkURL,
		CreatedTime:          remote.CreatedTime,
		UpdatedTime:          remote.UpdatedTime,
		IsPublished:          remote.IsPublished,
		ScheduledPublishTime: remote.ScheduledPublishTime,
		IsHidden:             remote.IsHidden,
		Editable:             view.Editable,
		ReactionsCount:       remote.ReactionsCount,
		CommentsCount:        remote.CommentsCount,
		SharesCount:          remote.SharesCount,
		HasAsset:             remote.AssetURL(false) != "",
		Attachments:          make([]PostAttachmentResponse, 0, len(remote.Attachments)),
	}
	for i, a := range remote.Attachments {
		out.Attachments = append(out.Attachments, PostAttachmentResponse{MediaType: a.MediaType, Title: a.Title, URL: a.URL, AssetIndex: i})
	}
	return out
}

func presentJob(job *fbdomain.PublishJob) PublishJobResponse {
	out := PublishJobResponse{
		ID: job.ID, Kind: string(job.Request.Kind), Status: string(job.Status), FBPostID: job.FBPostID,
		ScheduledAt: job.Request.ScheduledAt, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
	if job.Status == fbdomain.JobFailed {
		out.Error = &PublishJobError{Code: job.ErrorCode, Subcode: job.ErrorSubcode, Message: job.ErrorMessage, Ambiguous: job.Ambiguous}
		if job.Request.Kind == fbdomain.PublishReel && job.ErrorCode == fbdomain.CodeReelCap {
			out.Error.Reason = "reel_limit"
		}
	}
	return out
}

func (h *Handler) ListPosts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := fbdomain.ListPublished
	switch fbdomain.PostListKind(q.Get("kind")) {
	case fbdomain.ListScheduled:
		kind = fbdomain.ListScheduled
	case fbdomain.ListReels:
		kind = fbdomain.ListReels
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := h.posts.List(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], kind, limit, q.Get("after"))
	if err != nil {
		writeDomainError(w, err, "Failed to list Facebook posts")
		return
	}
	out := PostListResponse{Items: make([]PostResponse, 0, len(page.Items)), NextCursor: page.NextCursor, HasNext: page.HasNext}
	for _, item := range page.Items {
		out.Items = append(out.Items, presentPost(item))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *Handler) GetPost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	view, err := h.posts.Get(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["postId"])
	if err != nil {
		writeDomainError(w, err, "Failed to load the Facebook post")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentPost(view))
}

func (h *Handler) PostAsset(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	data, contentType, err := h.posts.Asset(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["postId"], r.URL.Query().Get("variant") == "thumb")
	if err != nil {
		writeDomainError(w, err, "Failed to load the Facebook post image")
		return
	}
	httpx.WriteBinary(w, data, contentType, "image/jpeg", postAssetMaxAge)
}

func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	var body PublishRequestBody
	if !decodeJSON(w, r, &body) {
		return
	}
	userID := ""
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	job, err := h.posts.Create(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], userID, fbdomain.PublishRequest{
		Kind: fbdomain.PublishKind(body.Kind), Message: body.Message, Link: body.Link,
		Media: body.Media, VideoTitle: body.VideoTitle, ScheduledAt: body.ScheduledPublishTime,
	})
	if err != nil {
		writeDomainError(w, err, "Failed to queue the Facebook post")
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, presentJob(job))
}

func (h *Handler) UpdatePost(w http.ResponseWriter, r *http.Request) {
	var body UpdatePostBody
	if !decodeJSON(w, r, &body) {
		return
	}
	vars := mux.Vars(r)
	err := h.posts.Update(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["postId"], fbdomain.PostUpdate{
		Message: body.Message, IsHidden: body.IsHidden, PublishNow: body.PublishNow, ScheduledAt: body.ScheduledPublishTime,
	})
	if err != nil {
		writeDomainError(w, err, "Failed to update the Facebook post")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) DeletePost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if err := h.posts.Delete(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["postId"]); err != nil {
		writeDomainError(w, err, "Failed to delete the Facebook post")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) ListPublishJobs(w http.ResponseWriter, r *http.Request) {
	status := fbdomain.JobStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))))
	jobs, err := h.posts.Jobs(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], status)
	if err != nil {
		writeDomainError(w, err, "Failed to list publish jobs")
		return
	}
	out := make([]PublishJobResponse, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, presentJob(job))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *Handler) GetPublishJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	job, err := h.posts.Job(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["jobId"])
	if err != nil {
		writeDomainError(w, err, "Failed to load the publish job")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentJob(job))
}
