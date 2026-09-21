package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"vozko/delivery/http/response"
)

type CSRFMiddleware struct {
	trustedOrigins map[string]struct{}
}

func NewCSRFMiddleware(trustedOrigins []string) *CSRFMiddleware {
	m := &CSRFMiddleware{trustedOrigins: make(map[string]struct{}, len(trustedOrigins))}
	for _, o := range trustedOrigins {
		m.trustedOrigins[strings.TrimRight(o, "/")] = struct{}{}
	}
	return m
}

func (m *CSRFMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if csrfIsSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			next.ServeHTTP(w, r)
			return
		}

		if !csrfHasAuthCookie(r) {
			next.ServeHTTP(w, r)
			return
		}

		origin := csrfRequestOrigin(r)
		if origin == "" || (!m.isTrusted(origin) && !csrfIsSameOrigin(r, origin)) {
			response.WriteError(w, http.StatusForbidden, "Origin not allowed", nil)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (m *CSRFMiddleware) isTrusted(origin string) bool {
	_, ok := m.trustedOrigins[strings.TrimRight(origin, "/")]
	return ok
}

func csrfIsSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func csrfHasAuthCookie(r *http.Request) bool {
	if c, err := r.Cookie("accessToken"); err == nil && c.Value != "" {
		return true
	}
	if c, err := r.Cookie("refreshToken"); err == nil && c.Value != "" {
		return true
	}
	return false
}

func csrfRequestOrigin(r *http.Request) string {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	return ""
}

func csrfIsSameOrigin(r *http.Request, origin string) bool {
	self := csrfSelfOrigin(r)
	return self != "" && strings.TrimRight(origin, "/") == self
}

func csrfSelfOrigin(r *http.Request) string {
	host := r.Host
	if host == "" {
		return ""
	}
	scheme := "https"
	if r.TLS != nil {
		scheme = "https"
	} else if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		if i := strings.IndexByte(proto, ','); i >= 0 {
			proto = strings.TrimSpace(proto[:i])
		}
		scheme = proto
	} else if csrfIsLocalhost(host) {
		scheme = "http"
	}
	return scheme + "://" + host
}

func csrfIsLocalhost(host string) bool {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
