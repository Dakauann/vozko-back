package cep

import (
	"net/http"

	"github.com/gorilla/mux"
)

type RateLimiter interface {
	Validate(next http.Handler) http.Handler
}

func RegisterPublicRoutes(public *mux.Router, h *CEPHandler, rl RateLimiter) {
	public.Handle("/cep/search", rl.Validate(http.HandlerFunc(h.Search))).Methods(http.MethodGet)
}
