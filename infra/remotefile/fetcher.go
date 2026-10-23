package remotefile

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"vozko/domain/media"
)

type Fetcher struct {
	client   *http.Client
	maxBytes int64
}

func NewFetcher(client *http.Client, maxBytes int64) *Fetcher {
	return &Fetcher{client: client, maxBytes: maxBytes}
}

func (f *Fetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	file, err := boundedGet(ctx, f.client, url, f.maxBytes)
	return file.Data, err
}

type Downloader struct {
	client *http.Client
}

var _ media.RemoteFiles = (*Downloader)(nil)

func NewDownloader(client *http.Client) *Downloader {
	return &Downloader{client: client}
}

func (d *Downloader) Download(ctx context.Context, url string, maxBytes int64) (media.RemoteFile, error) {
	return boundedGet(ctx, d.client, url, maxBytes)
}

func boundedGet(ctx context.Context, client *http.Client, url string, maxBytes int64) (media.RemoteFile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return media.RemoteFile{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return media.RemoteFile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return media.RemoteFile{}, fmt.Errorf("%w (status %d)", media.ErrRemoteFileMissing, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return media.RemoteFile{}, fmt.Errorf("remote file answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return media.RemoteFile{}, err
	}
	if int64(len(data)) > maxBytes {
		return media.RemoteFile{}, fmt.Errorf("%w: remote file is larger than %d bytes", media.ErrMediaTooLarge, maxBytes)
	}
	return media.RemoteFile{Data: data, ContentType: resp.Header.Get("Content-Type")}, nil
}
