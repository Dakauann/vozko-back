package metaplatform

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	mp "vozko/domain/metaplatform"
	"vozko/pkg/metasignedrequest"
	mpuc "vozko/usecases/metaplatform"
)

type Handler struct {
	service *mpuc.Service
	secrets map[mp.App][]string
}

func NewHandler(service *mpuc.Service, secrets map[mp.App][]string) *Handler {
	cleaned := make(map[mp.App][]string, len(secrets))
	for app, list := range secrets {
		for _, s := range list {
			if s = strings.TrimSpace(s); s != "" {
				cleaned[app] = append(cleaned[app], s)
			}
		}
	}
	return &Handler{service: service, secrets: cleaned}
}

func RegisterPublicRoutes(r *mux.Router, h *Handler) {
	if h == nil {
		return
	}
	r.HandleFunc("/webhooks/meta/deauthorize", h.deauthorize(mp.AppMeta)).Methods(http.MethodPost)
	r.HandleFunc("/webhooks/meta/data-deletion", h.dataDeletion(mp.AppMeta)).Methods(http.MethodPost)
	r.HandleFunc("/webhooks/instagram/deauthorize", h.deauthorize(mp.AppInstagram)).Methods(http.MethodPost)
	r.HandleFunc("/webhooks/instagram/data-deletion", h.dataDeletion(mp.AppInstagram)).Methods(http.MethodPost)
	r.HandleFunc("/meta/data-deletion/{code}", h.DeletionStatus).Methods(http.MethodGet)
}

func (h *Handler) deauthorize(app mp.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := h.verifiedUser(w, r, app)
		if !ok {
			return
		}
		if err := h.service.Deauthorize(r.Context(), app, userID); err != nil {
			log.Printf("[meta-platform] %s deauthorize for %s failed: %v", app, userID, err)
			response.WriteError(w, http.StatusInternalServerError, "Deauthorization failed", nil)
			return
		}
		response.WriteSuccess(w, http.StatusOK, map[string]bool{"success": true})
	}
}

func (h *Handler) dataDeletion(app mp.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := h.verifiedUser(w, r, app)
		if !ok {
			return
		}
		out, err := h.service.RequestDeletion(r.Context(), app, userID)
		if err != nil {
			log.Printf("[meta-platform] %s data deletion for %s failed: %v", app, userID, err)
			response.WriteError(w, http.StatusInternalServerError, "Data deletion request failed", nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"url":               out.StatusURL,
			"confirmation_code": out.Code,
		})
	}
}

type deletionStatusResponse struct {
	Code        string     `json:"code"`
	Status      string     `json:"status"`
	RequestedAt time.Time  `json:"requestedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

func (h *Handler) DeletionStatus(w http.ResponseWriter, r *http.Request) {
	req, err := h.service.Status(r.Context(), mux.Vars(r)["code"])
	if errors.Is(err, mp.ErrRequestNotFound) {
		response.WriteError(w, http.StatusNotFound, "Deletion request not found", nil)
		return
	}
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to read deletion request", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, deletionStatusResponse{
		Code:        req.Code,
		Status:      string(req.Status),
		RequestedAt: req.RequestedAt,
		CompletedAt: req.CompletedAt,
	})
}

func (h *Handler) verifiedUser(w http.ResponseWriter, r *http.Request, app mp.App) (string, bool) {
	if err := r.ParseForm(); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid form", nil)
		return "", false
	}
	raw := r.PostForm.Get("signed_request")
	for _, secret := range h.secrets[app] {
		if payload, err := metasignedrequest.Parse(raw, secret); err == nil {
			return payload.UserID, true
		}
	}
	log.Printf("[meta-platform] %s callback rejected: signed_request did not verify", app)
	response.WriteError(w, http.StatusBadRequest, "Invalid signed_request", nil)
	return "", false
}
