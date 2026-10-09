package media

import (
	"context"
	"io"
)

type FileStorage interface {
	UploadFile(key string, data []byte, contentType string) error

	GetFileURL(key string) string
}

type StreamStorage interface {
	PutStream(ctx context.Context, key, contentType string, body io.Reader) error
	DeleteFile(ctx context.Context, key string) error
	GetFileURL(key string) string
}
