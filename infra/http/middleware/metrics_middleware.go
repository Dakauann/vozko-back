package middleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vozko/domain/metrics"
)

type HTTPMetricsConfig struct {
	SkipPrefixes []string

	NormalizePaths bool
}

type HTTPMetrics struct {
	recorder metrics.HTTPMetricsRecorder
	config   HTTPMetricsConfig
}

func NewHTTPMetrics(recorder metrics.HTTPMetricsRecorder, config ...HTTPMetricsConfig) *HTTPMetrics {
	cfg := HTTPMetricsConfig{NormalizePaths: true}
	if len(config) > 0 {
		cfg = config[0]
	}
	return &HTTPMetrics{recorder: recorder, config: cfg}
}

func (m *HTTPMetrics) Record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.recorder == nil {
			next.ServeHTTP(w, r)
			return
		}

		path := r.URL.Path
		for _, p := range m.config.SkipPrefixes {
			if strings.HasPrefix(path, p) {
				next.ServeHTTP(w, r)
				return
			}
		}

		if m.config.NormalizePaths {
			path = normalizePath(path)
		}

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		m.recorder.IncHTTPInFlight(r.Method, path)
		defer m.recorder.DecHTTPInFlight(r.Method, path)

		start := time.Now()
		next.ServeHTTP(rw, r)
		elapsed := time.Since(start)

		m.recorder.ObserveHTTPLatency(r.Method, path, strconv.Itoa(rw.statusCode), elapsed)
		m.recorder.IncHTTPRequests(r.Method, path, strconv.Itoa(rw.statusCode))
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	wrote      bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wrote {
		rw.statusCode = code
		rw.wrote = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wrote {
		rw.statusCode = http.StatusOK
		rw.wrote = true
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errNotHijacker
}

var errNotHijacker = errors.New("response does not implement http.Hijacker")

func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func normalizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "" {
			continue
		}
		if looksLikeID(p) {
			parts[i] = ":id"
		}
	}
	return strings.Join(parts, "/")
}

func looksLikeID(s string) bool {
	if len(s) >= 32 && strings.Contains(s, "-") {
		return true
	}
	if _, err := strconv.ParseInt(s, 10, 64); err == nil && len(s) > 4 {
		return true
	}
	if len(s) >= 16 {
		return true
	}
	return false
}
