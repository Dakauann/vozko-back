package cmcom

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultGatewayURL = "https://gw.messaging.cm.com"
	DefaultAPIURL     = "https://api.cm.com"
	requestTimeout    = 30 * time.Second
)

var ErrConfigInvalid = errors.New("cmcom: invalid configuration")

var guidPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

type Config struct {
	ProductToken string
	AccountID    string
	GatewayURL   string
	APIURL       string
	HTTPClient   *http.Client
}

func (c Config) withDefaults() Config {
	c.ProductToken = strings.TrimSpace(c.ProductToken)
	c.AccountID = strings.TrimSpace(c.AccountID)
	c.GatewayURL = strings.TrimRight(strings.TrimSpace(c.GatewayURL), "/")
	c.APIURL = strings.TrimRight(strings.TrimSpace(c.APIURL), "/")
	if c.GatewayURL == "" {
		c.GatewayURL = DefaultGatewayURL
	}
	if c.APIURL == "" {
		c.APIURL = DefaultAPIURL
	}
	return c
}

func (c Config) Validate() error {
	c = c.withDefaults()
	if !guidPattern.MatchString(c.ProductToken) {
		return fmt.Errorf("%w: the product token must be a hyphenated guid", ErrConfigInvalid)
	}
	if !guidPattern.MatchString(c.AccountID) {
		return fmt.Errorf("%w: the account id must be a guid", ErrConfigInvalid)
	}
	if err := validateBaseURL("gateway url", c.GatewayURL); err != nil {
		return err
	}
	return validateBaseURL("api url", c.APIURL)
}

func validateBaseURL(field, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("%w: the %s must be a bare scheme and host", ErrConfigInvalid, field)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if loopbackHost(u.Hostname()) {
			return nil
		}
	}
	return fmt.Errorf("%w: the %s must use https", ErrConfigInvalid, field)
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
