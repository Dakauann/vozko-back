package media

import (
	"context"
	"errors"

	"vozko/domain/shared"
)

type RemoteFile struct {
	Data        []byte
	ContentType string
}

type RemoteFiles interface {
	Download(ctx context.Context, url string, maxBytes int64) (RemoteFile, error)
}

var (
	ErrRemoteFileMissing  = errors.New("the file does not exist at its origin")
	ErrRemoteFilesUnwired = errors.New("media downloads are not wired with the egress guard, so the server fetches no url")
)

func Download(ctx context.Context, files RemoteFiles, url string, maxBytes int64) (RemoteFile, error) {
	if files == nil {
		return RemoteFile{}, ErrRemoteFilesUnwired
	}
	return files.Download(ctx, url, maxBytes)
}

func DownloadRefused(err error) bool {
	return errors.Is(err, shared.ErrEgressBlocked) || errors.Is(err, ErrRemoteFilesUnwired)
}
