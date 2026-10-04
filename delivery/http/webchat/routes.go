package webchat

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}
	res := workspace_domain.ResourceWebchatWidgets
	wc := protected.PathPrefix("/webchat").Subrouter()

	wc.HandleFunc("/widgets", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	wc.HandleFunc("/widgets", ac(res, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	wc.HandleFunc("/widgets/{id}", ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	wc.HandleFunc("/widgets/{id}", ac(res, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	wc.HandleFunc("/widgets/{id}", ac(res, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	wc.HandleFunc("/widgets/{id}/identity-secret", ac(res, workspace_domain.ActionUpdate, h.RevealSecret)).Methods(http.MethodGet)
	wc.HandleFunc("/widgets/{id}/identity-secret", ac(res, workspace_domain.ActionUpdate, h.RotateSecret)).Methods(http.MethodPost)

	wc.HandleFunc("/conversations/{entryId}/block",
		ac(workspace_domain.ResourceConversations, workspace_domain.ActionUpdate, h.Block)).Methods(http.MethodPut)
}

func RegisterPublicRoutes(public *mux.Router, h *PublicHandler) {
	if h == nil {
		return
	}
	p := public.PathPrefix(PublicPrefix).Subrouter()

	p.HandleFunc("/loader.js", h.Loader).Methods(http.MethodGet)
	p.HandleFunc("/assets/{file}", h.Asset).Methods(http.MethodGet)

	p.HandleFunc("/session/intake", h.sameOrigin(h.Intake)).Methods(http.MethodPost)
	p.HandleFunc("/session/messages", h.sameOrigin(h.History)).Methods(http.MethodGet)
	p.HandleFunc("/session/messages", h.sameOrigin(h.Send)).Methods(http.MethodPost)
	p.HandleFunc("/session/media", h.sameOrigin(h.Upload)).Methods(http.MethodPost)
	p.HandleFunc("/session/handoff", h.sameOrigin(h.RequestHuman)).Methods(http.MethodPost)
	p.HandleFunc("/session/typing", h.sameOrigin(h.Typing)).Methods(http.MethodPost)
	p.HandleFunc("/session/stream", h.sameOrigin(h.Stream)).Methods(http.MethodGet)

	p.HandleFunc("/{publicKey}/frame", h.Frame).Methods(http.MethodGet)
	p.HandleFunc("/{publicKey}/challenge", h.sameOrigin(h.Challenge)).Methods(http.MethodPost)
	p.HandleFunc("/{publicKey}/session", h.sameOrigin(h.StartSession)).Methods(http.MethodPost)
}
