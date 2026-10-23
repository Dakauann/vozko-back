package snstest

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"
)

var ErrUnexpectedCertificateURL = errors.New("snstest: unexpected certificate url")

type KeyPair struct {
	Key  *rsa.PrivateKey
	Cert *x509.Certificate
	PEM  []byte
}

func NewKeyPair(t testing.TB, notAfter time.Time) KeyPair {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sns.amazonaws.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return KeyPair{Key: key, Cert: cert, PEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func (k KeyPair) Sign(t testing.TB, canonical, signatureVersion string) string {
	t.Helper()
	hash := crypto.SHA1
	var digest []byte
	if signatureVersion == "2" {
		hash = crypto.SHA256
		sum := sha256.Sum256([]byte(canonical))
		digest = sum[:]
	} else {
		sum := sha1.Sum([]byte(canonical))
		digest = sum[:]
	}
	sig, err := rsa.SignPKCS1v15(rand.Reader, k.Key, hash, digest)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

type Fetcher struct {
	mu    sync.Mutex
	url   string
	cert  *x509.Certificate
	calls []string
}

func NewFetcher(certURL string, cert *x509.Certificate) *Fetcher {
	return &Fetcher{url: certURL, cert: cert}
}

func (f *Fetcher) Fetch(_ context.Context, certURL string) (*x509.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, certURL)
	if certURL != f.url {
		return nil, ErrUnexpectedCertificateURL
	}
	return f.cert, nil
}

func (f *Fetcher) Serve(cert *x509.Certificate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cert = cert
}

func (f *Fetcher) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}
