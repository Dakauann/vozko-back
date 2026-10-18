package notificationhttp

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/notification"
	"vozko/infra/http/middleware"
)

const uploadOverhead = 1 << 20

type BroadcastService interface {
	Create(ctx context.Context, adminID string, d notification.BroadcastDraft) (notification.Broadcast, error)
	List(ctx context.Context, cursor string, limit int) (notification.BroadcastPage, error)
	Preview(ctx context.Context, d notification.BroadcastDraft) (notification.BroadcastReach, error)
	Recipients(ctx context.Context, broadcastID, status, cursor string, limit int) (notification.RecipientPage, error)
}

type ImageUploader interface {
	Upload(ctx context.Context, data []byte) (string, error)
}

type AdminHandler struct {
	broadcasts BroadcastService
	images     ImageUploader
}

func NewAdminHandler(broadcasts BroadcastService, images ImageUploader) *AdminHandler {
	return &AdminHandler{broadcasts: broadcasts, images: images}
}

func (h *AdminHandler) CreateBroadcast(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil || strings.TrimSpace(claims.UserID) == "" {
		writeError(w, notification.ErrViewerRequired)
		return
	}
	var body BroadcastRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	created, err := h.broadcasts.Create(r.Context(), strings.TrimSpace(claims.UserID), body.domain())
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentBroadcast(created))
}

func (h *AdminHandler) ListBroadcasts(w http.ResponseWriter, r *http.Request) {
	limit, ok := optionalInt(w, r.URL.Query().Get("limit"))
	if !ok {
		return
	}
	page, err := h.broadcasts.List(r.Context(), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]BroadcastResponse, 0, len(page.Items))
	for _, b := range page.Items {
		items = append(items, presentBroadcast(b))
	}
	response.WriteSuccess(w, http.StatusOK, BroadcastListResponse{Items: items, Next: page.Next})
}

func (h *AdminHandler) UploadImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, notification.MaxImageBytes+uploadOverhead)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, codeImageMissing, "send the image in the multipart field file", nil)
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, _, err := r.FormFile("file")
	if err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, codeImageMissing, "send the image in the multipart field file", nil)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, notification.MaxImageBytes+1))
	if err != nil {
		writeError(w, err)
		return
	}
	url, err := h.images.Upload(r.Context(), data)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, ImageResponse{URL: url})
}

func (h *AdminHandler) PreviewBroadcast(w http.ResponseWriter, r *http.Request) {
	var body BroadcastRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	reach, err := h.broadcasts.Preview(r.Context(), body.domain())
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentReach(reach))
}

func (h *AdminHandler) ListRecipients(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	limit, ok := optionalInt(w, params.Get("limit"))
	if !ok {
		return
	}
	page, err := h.broadcasts.Recipients(r.Context(), mux.Vars(r)["id"], params.Get("status"), params.Get("cursor"), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentRecipients(page))
}
