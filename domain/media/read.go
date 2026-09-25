package media

import (
	"context"
	"errors"
)

const MaxReadBytes = 25 << 20

var ErrMediaTooLarge = errors.New("media: file is too large to read")

type FileReader interface {
	KeyFromURL(url string) (string, bool)
	DownloadFile(ctx context.Context, key string) ([]byte, string, error)
}

type Content struct {
	Media       *Media
	Name        string
	ContentType string
	Data        []byte
}

type ReadMediaUseCase interface {
	Read(ctx context.Context, workspaceID, mediaID string) (*Content, error)
}
