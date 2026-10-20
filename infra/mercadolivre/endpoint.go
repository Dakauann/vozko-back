package mercadolivre

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var errEndpointURL = errors.New("must be an absolute http or https url with a host and no query or fragment")

func endpointURL(raw, fallback string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return fallback, nil
	}
	u, err := url.Parse(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("%w: %q", errEndpointURL, raw)
	}
	return trimmed, nil
}
