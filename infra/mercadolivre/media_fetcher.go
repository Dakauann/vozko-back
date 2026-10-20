package mercadolivre

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	mldomain "vozko/domain/mercadolivre"
)

type MediaFetcher struct {
	http *http.Client
}

var _ mldomain.MediaFetcher = (*MediaFetcher)(nil)

func NewMediaFetcher(client *http.Client) *MediaFetcher {
	if client == nil {
		panic("mercadolivre: media fetcher requires a guarded http client")
	}
	return &MediaFetcher{http: client}
}

func (f *MediaFetcher) Fetch(ctx context.Context, raw string, maxBytes int64) (*mldomain.AttachmentDownload, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("%w: media url %q", mldomain.ErrMediaUnsupported, raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return nil, &Error{Code: CodeTransport, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &Error{HTTPStatus: resp.StatusCode, Code: CodeTransport, Err: errors.New("media download refused")}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, &Error{Code: CodeTransport, Err: err}
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: over %d bytes", mldomain.ErrMediaTooLarge, maxBytes)
	}
	return &mldomain.AttachmentDownload{Data: data, ContentType: resp.Header.Get("Content-Type")}, nil
}
