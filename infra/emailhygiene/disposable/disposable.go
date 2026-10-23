package disposable

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"vozko/domain/emailhygiene"
)

const (
	UpstreamURL   = "https://raw.githubusercontent.com/disposable-email-domains/disposable-email-domains/main/disposable_email_blocklist.conf"
	fetchTimeout  = 30 * time.Second
	maxFetchBytes = 4 << 20
)

//go:embed snapshot.conf
var snapshot string

func Snapshot() string {
	return snapshot
}

var ErrTooLarge = errors.New("disposable: the downloaded list is larger than expected")

type Source struct {
	client *http.Client
	url    string
}

var _ emailhygiene.DisposableSource = (*Source)(nil)

func NewSource(client *http.Client, url string) *Source {
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}
	if url == "" {
		url = UpstreamURL
	}
	return &Source{client: client, url: url}
}

func (s *Source) Fetch(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("disposable: upstream answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxFetchBytes {
		return "", ErrTooLarge
	}
	return string(body), nil
}
