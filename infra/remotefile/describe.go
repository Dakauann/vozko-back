package remotefile

import (
	"context"
	"errors"
	"io"
	"net/http"
)

const sniffBytes = 512

func (f *Fetcher) Describe(ctx context.Context, url string) (string, int64, error) {
	remote, err := f.Open(ctx, url)
	if err != nil {
		return "", 0, err
	}
	head := make([]byte, min(sniffBytes, remote.Size()))
	n, err := remote.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", 0, err
	}
	return http.DetectContentType(head[:n]), remote.Size(), nil
}
