package remotefile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

var ErrNoRanges = errors.New("remote file is not served in ranges")

type Remote struct {
	ctx    context.Context
	client *http.Client
	url    string
	size   int64
}

func (f *Fetcher) Open(ctx context.Context, url string) (*Remote, error) {
	remote := &Remote{ctx: ctx, client: f.client, url: url}
	resp, err := remote.get(0, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	size, err := totalSize(resp.Header.Get("Content-Range"))
	if err != nil {
		return nil, err
	}
	remote.size = size
	return remote, nil
}

func (r *Remote) Size() int64 { return r.size }

func (r *Remote) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("remote file: negative offset %d", off)
	}
	if off >= r.size {
		return 0, io.EOF
	}
	want := min(int64(len(p)), r.size-off)
	if want == 0 {
		return 0, nil
	}
	resp, err := r.get(off, off+want-1)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	n, err := io.ReadFull(resp.Body, p[:want])
	if err != nil {
		return n, err
	}
	if want < int64(len(p)) {
		return n, io.EOF
	}
	return n, nil
}

func (r *Remote) get(first, last int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", "bytes="+strconv.FormatInt(first, 10)+"-"+strconv.FormatInt(last, 10))
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusPartialContent:
		return resp, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		resp.Body.Close()
		return nil, ErrNoRanges
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("remote file answered %d", resp.StatusCode)
	}
}

func totalSize(contentRange string) (int64, error) {
	_, total, ok := strings.Cut(contentRange, "/")
	if !ok {
		return 0, ErrNoRanges
	}
	size, err := strconv.ParseInt(strings.TrimSpace(total), 10, 64)
	if err != nil || size <= 0 {
		return 0, ErrNoRanges
	}
	return size, nil
}
