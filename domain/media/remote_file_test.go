package media

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"vozko/domain/shared"
)

type stubRemoteFiles struct {
	file RemoteFile
	err  error
	max  int64
}

func (s *stubRemoteFiles) Download(_ context.Context, _ string, maxBytes int64) (RemoteFile, error) {
	s.max = maxBytes
	return s.file, s.err
}

func TestWithoutWiredDownloadsTheServerFetchesNothing(t *testing.T) {
	_, err := Download(context.Background(), nil, "https://cdn.example.com/a.png", 10)
	if !errors.Is(err, ErrRemoteFilesUnwired) {
		t.Fatalf("err = %v, an unwired downloader must refuse instead of fetching with a plain client", err)
	}
}

func TestDownloadPassesTheLimitToTheDownloader(t *testing.T) {
	files := &stubRemoteFiles{file: RemoteFile{Data: []byte("x"), ContentType: "image/png"}}
	got, err := Download(context.Background(), files, "https://cdn.example.com/a.png", 42)
	if err != nil || string(got.Data) != "x" || got.ContentType != "image/png" || files.max != 42 {
		t.Fatalf("got = %+v, err = %v, max = %d", got, err, files.max)
	}
}

func TestOnlyGuardRefusalsStopASendInsteadOfFallingBackToTheLink(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"blocked address", fmt.Errorf("download: %w", shared.ErrEgressBlocked), true},
		{"no guarded downloader", ErrRemoteFilesUnwired, true},
		{"missing at origin", ErrRemoteFileMissing, false},
		{"timeout", errors.New("context deadline exceeded"), false},
		{"no error", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DownloadRefused(tc.err); got != tc.want {
				t.Fatalf("DownloadRefused(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
