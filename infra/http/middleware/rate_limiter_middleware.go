package middleware

import (
	"log"
	"net/http"
	"strings"
	"time"

	"vozko/delivery/http/response"
	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/metrics"
)

type RateLimiterMiddleware struct {
	limiter      cache.RateLimiter
	skipPrefixes []string
	name         string
	metrics      metrics.RateLimitMetricsRecorder
	identifyUser func(*http.Request) (string, bool)
}

func NewRateLimiterMiddleware(limiter cache.RateLimiter) *RateLimiterMiddleware {
	return &RateLimiterMiddleware{limiter: limiter, name: "unnamed"}
}

func (rlm *RateLimiterMiddleware) SkipPathPrefixes(prefixes ...string) *RateLimiterMiddleware {
	rlm.skipPrefixes = append(rlm.skipPrefixes, prefixes...)
	return rlm
}

// Named sets the limiter's label used in logs and the rate_limited_total metric
// (e.g. "global", "login"), so rejections can be attributed to a specific limiter.
func (rlm *RateLimiterMiddleware) Named(name string) *RateLimiterMiddleware {
	rlm.name = name
	return rlm
}

// WithMetrics wires the recorder that counts rejections by limiter, client IP and
// reason. Safe to pass nil (metrics simply disabled).
func (rlm *RateLimiterMiddleware) WithMetrics(rec metrics.RateLimitMetricsRecorder) *RateLimiterMiddleware {
	rlm.metrics = rec
	return rlm
}

// WithUserIdentity makes the limiter identity-aware: when the request carries a
// valid auth token, it is limited per USER (its own budget); otherwise it falls
// back to per-IP. This is the industry-standard split, rate-limit authenticated
// traffic by stable identity, reserve per-IP for anonymous traffic, so that many
// users behind one shared IP (an office NAT, or first-party SSR calls) each get an
// independent budget instead of colliding on one per-IP bucket. Anonymous floods
// (no/invalid token) still hit the per-IP path, so DoS protection is preserved.
func (rlm *RateLimiterMiddleware) WithUserIdentity(fn func(*http.Request) (string, bool)) *RateLimiterMiddleware {
	rlm.identifyUser = fn
	return rlm
}

// UserFromToken builds an identity function for WithUserIdentity from the same
// token verifier the auth middleware uses. It only checks the signature (not
// revocation/version), enough to pick a stable per-user rate-limit key cheaply;
// any revoked/expired token still gets rejected downstream by the auth middleware.
func UserFromToken(verifier auth.TokenVerifier) func(*http.Request) (string, bool) {
	return func(r *http.Request) (string, bool) {
		token := tokenFromRequest(r)
		if token == "" {
			return "", false
		}
		claims, err := verifier.Verify(token)
		if err != nil || claims == nil || claims.UserID == "" {
			return "", false
		}
		return claims.UserID, true
	}
}

// tokenFromRequest mirrors AuthMiddleware.Authenticate's extraction order:
// Authorization: Bearer, then ?token=, then the accessToken cookie.
func tokenFromRequest(r *http.Request) string {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if parts := strings.Split(authHeader, " "); len(parts) == 2 && parts[0] == "Bearer" {
			return parts[1]
		}
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	if cookie, err := r.Cookie("accessToken"); err == nil {
		return cookie.Value
	}
	return ""
}

// observeRejection emits the structured log line (pinpoints the offending IP) and
// increments the metric (powers the dashboard/alert). Kept in one place so both the
// fail-closed and limit-exceeded branches report identically.
func (rlm *RateLimiterMiddleware) observeRejection(r *http.Request, ip, reason string, retryAfter time.Duration) {
	log.Printf("[rate-limit] REJECT limiter=%s ip=%s method=%s path=%s reason=%s retry_after=%s",
		rlm.name, ip, r.Method, r.URL.Path, reason, retryAfter)
	if rlm.metrics != nil {
		rlm.metrics.IncRateLimited(rlm.name, ip, reason)
	}
}

func (rlm *RateLimiterMiddleware) Validate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range rlm.skipPrefixes {
			if strings.HasPrefix(r.URL.Path, p) {
				next.ServeHTTP(w, r)
				return
			}
		}

		ip := getClientIP(r)

		// Authenticated traffic is limited per user (its own budget); anonymous
		// traffic falls back to per-IP. Keys are namespaced so a user id can never
		// collide with an IP. The client IP is still what we log/meter, so the
		// dashboard keeps pinpointing offending IPs regardless of the key used.
		key := ip
		if rlm.identifyUser != nil {
			if uid, ok := rlm.identifyUser(r); ok {
				key = "u:" + uid
			}
		}

		allowed, retryAfter, err := rlm.limiter.Allow(key)
		if err != nil {

			log.Printf("rate-limiter error (%T): %v, rejecting request (fail-closed)", err, err)
			rlm.observeRejection(r, ip, metrics.RateLimitReasonError, retryAfter)
			response.WriteError(w, http.StatusTooManyRequests, "Service temporarily unavailable, please retry", nil)
			return
		}

		if !allowed {
			rlm.observeRejection(r, ip, metrics.RateLimitReasonExceeded, retryAfter)
			response.WriteError(w, http.StatusTooManyRequests, "Rate limit exceeded", map[string]string{
				"retry_after": retryAfter.String(),
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func GetClientIP(r *http.Request) string {
	return getClientIP(r)
}

func getClientIP(r *http.Request) string {
	// CF-Connecting-IP is set authoritatively by Cloudflare and strips any
	// client-supplied value, so it cannot be spoofed for traffic proxied through
	// Cloudflare. Prefer it, then the reverse-proxy's X-Real-IP. We deliberately
	// avoid trusting the FIRST X-Forwarded-For entry as the primary source, since
	// that is attacker-controllable; it is kept only as a last-resort fallback so a
	// proxy that sets neither header still yields a usable key.
	if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
		return cf
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if ips := strings.Split(forwarded, ","); len(ips) > 0 {
			if first := strings.TrimSpace(ips[0]); first != "" {
				return first
			}
		}
	}
	ip := r.RemoteAddr
	if colonIndex := strings.LastIndex(ip, ":"); colonIndex != -1 {
		ip = ip[:colonIndex]
	}
	return strings.Trim(ip, "[]")
}
