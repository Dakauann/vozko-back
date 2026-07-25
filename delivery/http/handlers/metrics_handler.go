package handlers

import (
	"net/http"
)

type HTTPHandler interface {
	Handler() http.Handler
}

type MetricsHandler struct {
	prometheusService HTTPHandler
}

func NewMetricsHandler(prometheusService HTTPHandler) *MetricsHandler {
	return &MetricsHandler{
		prometheusService: prometheusService,
	}
}

func (h *MetricsHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.prometheusService.Handler().ServeHTTP(w, r)
}
