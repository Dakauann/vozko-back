package remotefile

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

type Fetcher struct {
	client   *http.Client
	maxBytes int64
}

func NewFetcher(client *http.Client, maxBytes int64) *Fetcher {
	return &Fetcher{client: client, maxBytes: maxBytes}
}

func (f *Fetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("remote file answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > f.maxBytes {
		return nil, fmt.Errorf("remote file is larger than %d bytes", f.maxBytes)
	}
	return data, nil
}
