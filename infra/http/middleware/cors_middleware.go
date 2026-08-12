package middleware

import (
	"net/http"
	"strings"
)

type CORSMiddleware struct {
	trustedOrigins map[string]struct{}
}

func NewCORSMiddleware(trustedOrigins []string) *CORSMiddleware {
	m := &CORSMiddleware{
		trustedOrigins: make(map[string]struct{}, len(trustedOrigins)),
	}
	for _, o := range trustedOrigins {
		m.trustedOrigins[strings.TrimRight(o, "/")] = struct{}{}
	}
	return m
}

const (
	// corsAllowHeaders must list EVERY custom header the browser client sends.
	//
	// This list is a hard gate, not documentation: a header missing here fails
	// the preflight, so the real request never leaves the browser and never
	// reaches a handler or a log. The symptom is a bare "Failed to fetch" with
	// zero bytes transferred, which looks like a broken route rather than a
	// missing string. Adding a header to browser-client.ts means adding it here
	// in the same change; cors_middleware_test.go pins the pairing.
	corsAllowHeaders = "Authorization, Content-Type, X-Workspace-ID, X-Department-ID, X-Auth-Mode, Idempotency-Key"
	corsAllowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	corsMaxAge       = "86400"
)

func (c *CORSMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if origin != "" {
			normalized := strings.TrimRight(origin, "/")
			if _, trusted := c.trustedOrigins[normalized]; trusted {

				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			} else {

				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
			w.Header().Set("Access-Control-Allow-Methods", corsAllowMethods)
			w.Header().Set("Access-Control-Allow-Headers", corsAllowHeaders)
			w.Header().Set("Access-Control-Max-Age", corsMaxAge)
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
