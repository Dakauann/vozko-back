package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"vozko/delivery/http/response"
)

// CSRFMiddleware enforces an Origin/Referer check on cookie-authenticated,
// state-changing requests. Session cookies are ambient (the browser attaches them
// on any request to the API host), so a cross-site page could otherwise forge a
// state-changing call using the victim's cookie. This is the primary CSRF defense
// (OWASP "Verifying Origin With Standard Headers"); SameSite=Lax on the cookies is
// defense-in-depth.
//
// Exemptions, by construction, leave the open API untouched:
//   - Safe methods (GET/HEAD/OPTIONS) carry no state change.
//   - Bearer-authenticated requests are not CSRF-able: the credential is not
//     ambient and a cross-site page cannot set the Authorization header. If a
//     Bearer is present, the auth middleware validates it and never falls back to
//     the cookie, so skipping the cookie CSRF check here is safe.
//   - Requests without a session cookie (login/register, mobile, third-party API)
//     cannot be CSRF'd via our cookies.
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

// csrfRequestOrigin returns the request origin from the Origin header, falling
// back to scheme://host of the Referer.
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

// csrfIsSameOrigin reports whether origin matches the host the request was sent
// to (scheme://host). A same-origin request is not cross-site and therefore not
// CSRF-able, so it is trusted regardless of the frontend allowlist. This matters
// because the API serves some of its own pages, e.g. the Meta Embedded Signup
// popup, that make cookie-authenticated POSTs back to the API; those carry the
// API's OWN origin, which is not (and should not be) in CORS_TRUSTED_ORIGINS.
// This is the canonical OWASP check: compare Origin against the target origin.
func csrfIsSameOrigin(r *http.Request, origin string) bool {
	self := csrfSelfOrigin(r)
	return self != "" && strings.TrimRight(origin, "/") == self
}

// csrfSelfOrigin reconstructs the origin the client used to reach us. TLS is
// terminated at the reverse proxy, so r.TLS is nil in production; the proxy's
// X-Forwarded-Proto carries the real scheme. r.Host is the client-supplied Host
// header, i.e. the public host the browser addressed, the same host it stamps
// into the Origin of a same-origin request. Only our proxy sets these; a browser
// cannot forge X-Forwarded-Proto on a cross-site request, so this is safe as a
// CSRF trust signal.
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
