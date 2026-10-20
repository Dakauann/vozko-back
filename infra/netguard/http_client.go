package netguard

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"vozko/domain/shared"
)

const maxRedirects = 5

var ErrBlockedAddress = shared.ErrEgressBlocked

func NewHTTPClient(timeout time.Duration) *http.Client {
	return newGuardedClient(timeout, nil)
}

func NewLegacyTLSHTTPClient(timeout time.Duration) *http.Client {
	return newGuardedClient(timeout, &tls.Config{MaxVersion: tls.VersionTLS12})
}

func newGuardedClient(timeout time.Duration, tlsConfig *tls.Config) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: refuseBlocked}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
			TLSClientConfig:       tlsConfig,
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}
}

func refuseBlocked(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, address)
	}
	if IsBlockedIP(net.ParseIP(host)) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
	}
	return nil
}
