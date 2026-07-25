package shortlink_usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"

	"vozko/domain/shortlink"
)

var generateCode = shortlink.GenerateCode

const resolveCachePrefix = "shortlink:resolve:"

func resolveCacheKey(code string) string {
	return resolveCachePrefix + code
}

func uniqueVisitorKey(shortLinkID, ipHash string) string {
	return "shortlink:uniq:" + shortLinkID + ":" + ipHash
}

func HashIP(salt, ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(salt + "|" + ip))
	return hex.EncodeToString(sum[:])
}

func RefererDomain(referer string) string {
	referer = strings.TrimSpace(referer)
	if referer == "" {
		return ""
	}
	u, err := url.Parse(referer)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func resolveRedirectType(raw string) (shortlink.RedirectType, error) {
	if raw == "" {
		return shortlink.RedirectTemporary, nil
	}
	rt := shortlink.RedirectType(strings.TrimSpace(raw))
	if !rt.IsValid() {
		return "", shortlink.ErrInvalidRedirectType
	}
	return rt, nil
}
