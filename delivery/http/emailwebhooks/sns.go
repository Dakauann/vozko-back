package emailwebhooks

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	typeNotification    = "Notification"
	typeSubscription    = "SubscriptionConfirmation"
	typeUnsubscription  = "UnsubscribeConfirmation"
	maxCertificateBytes = 64 << 10
	certCacheTTL        = 6 * time.Hour
	certFetchTimeout    = 10 * time.Second
)

var (
	ErrUnknownType    = errors.New("sns: unknown message type")
	ErrBadCertURL     = errors.New("sns: signing certificate url is not the pinned sns host")
	ErrBadSignature   = errors.New("sns: signature verification failed")
	ErrUnknownVersion = errors.New("sns: unsupported signature version")
	ErrBadSubscribe   = errors.New("sns: subscribe url is not the pinned sns host")
)

type Envelope struct {
	Type             string  `json:"Type"`
	MessageID        string  `json:"MessageId"`
	Token            string  `json:"Token"`
	TopicArn         string  `json:"TopicArn"`
	Subject          *string `json:"Subject"`
	Message          string  `json:"Message"`
	SubscribeURL     string  `json:"SubscribeURL"`
	Timestamp        string  `json:"Timestamp"`
	SignatureVersion string  `json:"SignatureVersion"`
	Signature        string  `json:"Signature"`
	SigningCertURL   string  `json:"SigningCertURL"`
}

type CertificateFetcher interface {
	Fetch(ctx context.Context, certURL string) (*x509.Certificate, error)
}

type Verifier struct {
	host    string
	fetcher CertificateFetcher
	now     func() time.Time
}

func NewVerifier(region string, fetcher CertificateFetcher) (*Verifier, error) {
	region = strings.TrimSpace(region)
	if region == "" || strings.ContainsAny(region, "./:@ ") {
		return nil, errors.New("sns: invalid region")
	}
	if fetcher == nil {
		fetcher = NewHTTPCertificateFetcher(nil)
	}
	return &Verifier{host: "sns." + region + ".amazonaws.com", fetcher: fetcher, now: time.Now}, nil
}

func (v *Verifier) pinned(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !strings.EqualFold(u.Host, v.host) {
		return nil, false
	}
	return u, true
}

func (v *Verifier) Verify(ctx context.Context, e Envelope) error {
	if e.Type != typeNotification && e.Type != typeSubscription && e.Type != typeUnsubscription {
		return ErrUnknownType
	}
	var hash crypto.Hash
	switch e.SignatureVersion {
	case "1":
		hash = crypto.SHA1
	case "2":
		hash = crypto.SHA256
	default:
		return ErrUnknownVersion
	}
	u, ok := v.pinned(e.SigningCertURL)
	if !ok || !strings.HasSuffix(u.Path, ".pem") {
		return ErrBadCertURL
	}
	signature, err := base64.StdEncoding.DecodeString(e.Signature)
	if err != nil || len(signature) == 0 {
		return ErrBadSignature
	}
	cert, err := v.fetcher.Fetch(ctx, e.SigningCertURL)
	if err != nil {
		return fmt.Errorf("sns: fetch certificate: %w", err)
	}
	now := v.now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return fmt.Errorf("%w: certificate outside its validity", ErrBadSignature)
	}
	key, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: certificate key is not rsa", ErrBadSignature)
	}
	digest := digestOf(hash, CanonicalString(e))
	if err := rsa.VerifyPKCS1v15(key, hash, digest, signature); err != nil {
		return ErrBadSignature
	}
	return nil
}

func (v *Verifier) ValidSubscribeURL(raw string) bool {
	_, ok := v.pinned(raw)
	return ok
}

func digestOf(hash crypto.Hash, payload string) []byte {
	if hash == crypto.SHA1 {
		sum := sha1.Sum([]byte(payload))
		return sum[:]
	}
	sum := sha256.Sum256([]byte(payload))
	return sum[:]
}

func CanonicalString(e Envelope) string {
	var b strings.Builder
	add := func(k, v string) {
		b.WriteString(k)
		b.WriteByte('\n')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	add("Message", e.Message)
	add("MessageId", e.MessageID)
	if e.Type == typeNotification {
		if e.Subject != nil {
			add("Subject", *e.Subject)
		}
		add("Timestamp", e.Timestamp)
		add("TopicArn", e.TopicArn)
		add("Type", e.Type)
		return b.String()
	}
	add("SubscribeURL", e.SubscribeURL)
	add("Timestamp", e.Timestamp)
	add("Token", e.Token)
	add("TopicArn", e.TopicArn)
	add("Type", e.Type)
	return b.String()
}

type cachedCert struct {
	cert    *x509.Certificate
	fetched time.Time
}

type HTTPCertificateFetcher struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[string]cachedCert
	now    func() time.Time
}

func NewHTTPCertificateFetcher(client *http.Client) *HTTPCertificateFetcher {
	if client == nil {
		client = &http.Client{
			Timeout: certFetchTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &HTTPCertificateFetcher{client: client, cache: map[string]cachedCert{}, now: time.Now}
}

func (f *HTTPCertificateFetcher) Fetch(ctx context.Context, certURL string) (*x509.Certificate, error) {
	f.mu.Lock()
	if c, ok := f.cache[certURL]; ok && f.now().Sub(c.fetched) < certCacheTTL {
		f.mu.Unlock()
		return c.cert, nil
	}
	f.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, certURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("certificate status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxCertificateBytes))
	if err != nil {
		return nil, err
	}
	cert, err := ParseCertificate(raw)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.cache[certURL] = cachedCert{cert: cert, fetched: f.now()}
	f.mu.Unlock()
	return cert, nil
}

func ParseCertificate(raw []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("certificate is not pem")
	}
	return x509.ParseCertificate(block.Bytes)
}
