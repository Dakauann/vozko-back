package scriptvm

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type fetchOptions struct {
	URL     string                 `json:"url"`
	Method  string                 `json:"method"`
	Headers map[string]string      `json:"headers"`
	Body    interface{}            `json:"body"`
	Timeout int                    `json:"timeout_ms"`
	Query   map[string]interface{} `json:"query"`
}

type fetchResponse struct {
	Status     int               `json:"status"`
	OK         bool              `json:"ok"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	URL        string            `json:"url"`
	DurationMs int64             `json:"duration_ms"`
}

var blockedNetworks = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
		"::ffff:127.0.0.0/104",
		"::ffff:10.0.0.0/104",
		"::ffff:169.254.0.0/112",
		"::ffff:192.168.0.0/112",
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}()

func ipIsBlocked(ip net.IP) bool {
	for _, n := range blockedNetworks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func hostMatchesAllowlist(host string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, entry := range allowlist {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, ".") {
			if strings.HasSuffix(host, entry) || host == strings.TrimPrefix(entry, ".") {
				return true
			}
			continue
		}
		if host == entry {
			return true
		}
	}
	return false
}

func safeDialer(parent context.Context, timeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: timeout}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := (&net.Resolver{}).LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if ipIsBlocked(ip.IP) {
				return nil, fmt.Errorf("scriptvm: blocked private/loopback address %s", ip.IP)
			}
		}

		first := ips[0].IP.String()
		return d.DialContext(ctx, network, net.JoinHostPort(first, port))
	}
}

func makeSafeFetchClient(parent context.Context, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext:           safeDialer(parent, timeout),
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: 1 * time.Second,
		DisableKeepAlives:     true,
		MaxIdleConns:          0,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}

			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				req.Header.Del("Authorization")
			}

			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("disallowed redirect scheme: %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

type fetchCallState struct {
	calls atomic.Int32
}

func validateFetchURL(raw string, limits Limits) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("scriptvm: fetch: missing url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("scriptvm: fetch: invalid url: %w", err)
	}
	if u.Scheme != "https" && !(limits.AllowHTTPInsecure && u.Scheme == "http") {
		return nil, fmt.Errorf("scriptvm: fetch: scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("scriptvm: fetch: missing host")
	}

	if ip := net.ParseIP(host); ip != nil && ipIsBlocked(ip) {
		return nil, fmt.Errorf("scriptvm: fetch: blocked address %s", host)
	}
	if !hostMatchesAllowlist(host, limits.EgressAllowlist) {
		return nil, fmt.Errorf("scriptvm: fetch: host %q not in egress allowlist", host)
	}
	return u, nil
}

func readCappedBody(r io.Reader, max int) (string, error) {
	if max <= 0 {
		max = 1024 * 1024
	}
	lr := io.LimitReader(r, int64(max)+1)
	buf, err := io.ReadAll(lr)
	if err != nil {
		return "", err
	}
	if len(buf) > max {
		return "", fmt.Errorf("scriptvm: fetch: response body exceeds %d bytes", max)
	}
	return string(buf), nil
}
