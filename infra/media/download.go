package media_infra

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"vozko/domain/mediagen"
)

func downloadBounded(ctx context.Context, client *http.Client, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("file larger than %d bytes", maxBytes)
	}
	return body, nil
}

func soleSource(ctx context.Context, client *http.Client, sources []mediagen.Source, maxBytes int64) ([]byte, error) {
	if len(sources) != 1 {
		return nil, fmt.Errorf("media: %d sources resolved where one is required", len(sources))
	}
	return downloadBounded(ctx, client, sources[0].URL, maxBytes)
}
