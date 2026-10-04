package webchat

import (
	"net"
	"net/url"
	"strings"
)

const wildcardLabel = "*."

type OriginPattern struct {
	scheme   string
	host     string
	port     string
	wildcard bool
}

func ParseOriginPattern(raw string) (OriginPattern, error) {
	trimmed := strings.TrimRight(strings.ToLower(strings.TrimSpace(raw)), "/")
	if trimmed == "" || strings.ContainsAny(trimmed, " \t\r\n;,'\"") {
		return OriginPattern{}, ErrOriginInvalid
	}

	wildcard := false
	if scheme, rest, ok := strings.Cut(trimmed, "://"+wildcardLabel); ok {
		wildcard = true
		trimmed = scheme + "://" + rest
	}

	pattern, err := parseOrigin(trimmed)
	if err != nil {
		return OriginPattern{}, err
	}
	pattern.wildcard = wildcard

	if strings.Contains(pattern.host, "*") {
		return OriginPattern{}, ErrOriginInvalid
	}
	if wildcard && (isLoopbackHost(pattern.host) || strings.Count(pattern.host, ".") < 1 || net.ParseIP(pattern.host) != nil) {
		return OriginPattern{}, ErrOriginInvalid
	}
	if pattern.scheme == "http" && !isLoopbackHost(pattern.host) {
		return OriginPattern{}, ErrOriginInsecure
	}
	return pattern, nil
}

func (p OriginPattern) String() string {
	var b strings.Builder
	b.WriteString(p.scheme)
	b.WriteString("://")
	if p.wildcard {
		b.WriteString(wildcardLabel)
	}
	b.WriteString(p.host)
	if p.port != "" {
		b.WriteString(":")
		b.WriteString(p.port)
	}
	return b.String()
}

func (p OriginPattern) Matches(origin string) bool {
	candidate, err := parseOrigin(strings.ToLower(strings.TrimSpace(origin)))
	if err != nil {
		return false
	}
	if candidate.scheme != p.scheme || candidate.port != p.port {
		return false
	}
	if p.wildcard {
		return strings.HasSuffix(candidate.host, "."+p.host)
	}
	return candidate.host == p.host
}

func parseOrigin(raw string) (OriginPattern, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return OriginPattern{}, ErrOriginInvalid
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return OriginPattern{}, ErrOriginInvalid
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return OriginPattern{}, ErrOriginInvalid
	}
	host := u.Hostname()
	if host == "" {
		return OriginPattern{}, ErrOriginInvalid
	}
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	return OriginPattern{scheme: u.Scheme, host: host, port: port}, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
