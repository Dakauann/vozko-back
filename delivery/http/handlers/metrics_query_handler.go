package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

type MetricsQueryHandler struct {
	prometheusURL string
	httpClient    *http.Client
}

func NewMetricsQueryHandler(prometheusURL string, httpClient *http.Client) *MetricsQueryHandler {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &MetricsQueryHandler{
		prometheusURL: prometheusURL,
		httpClient:    httpClient,
	}
}

func (h *MetricsQueryHandler) QueryRange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("query")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	step := r.URL.Query().Get("step")

	if query == "" {
		http.Error(w, "missing query parameter", http.StatusBadRequest)
		return
	}
	if startStr == "" || endStr == "" {
		http.Error(w, "missing start or end parameter", http.StatusBadRequest)
		return
	}

	if _, err := strconv.ParseInt(startStr, 10, 64); err != nil {
		http.Error(w, "invalid start timestamp", http.StatusBadRequest)
		return
	}
	if _, err := strconv.ParseInt(endStr, 10, 64); err != nil {
		http.Error(w, "invalid end timestamp", http.StatusBadRequest)
		return
	}

	if step == "" {
		step = "60s"
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("start", startStr)
	params.Set("end", endStr)
	params.Set("step", step)

	promURL := h.prometheusURL + "/api/v1/query_range?" + params.Encode()

	resp, err := h.httpClient.Get(promURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to query prometheus: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "failed to read prometheus response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (h *MetricsQueryHandler) QueryInstant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("query")
	timeStr := r.URL.Query().Get("time")

	if query == "" {
		http.Error(w, "missing query parameter", http.StatusBadRequest)
		return
	}

	if timeStr != "" {
		if _, err := strconv.ParseInt(timeStr, 10, 64); err != nil {
			http.Error(w, "invalid time timestamp", http.StatusBadRequest)
			return
		}
	}

	params := url.Values{}
	params.Set("query", query)
	if timeStr != "" {
		params.Set("time", timeStr)
	}

	promURL := h.prometheusURL + "/api/v1/query?" + params.Encode()

	resp, err := h.httpClient.Get(promURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to query prometheus: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "failed to read prometheus response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (h *MetricsQueryHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.QueryRange(w, r)
}
