package media_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/media"
)

type filesStub struct {
	data      []byte
	downloads int
}

func (f *filesStub) KeyFromURL(url string) (string, bool) {
	return strings.TrimPrefix(url, "https://files.test/"), strings.HasPrefix(url, "https://files.test/")
}

func (f *filesStub) DownloadFile(context.Context, string) ([]byte, string, error) {
	f.downloads++
	return f.data, "text/csv", nil
}

func mediaAt(workspaceID, url string) media.GetMediaUseCase {
	return NewGetMediaUseCase(mediaByID{stored: &media.Media{ID: "m1", WorkspaceID: workspaceID, URL: url}})
}

func TestReadMediaReadsOnlyTheWorkspacesOwnFiles(t *testing.T) {
	files := &filesStub{data: []byte("numero,nome\n")}
	if _, err := NewReadMediaUseCase(mediaAt("ws2", "https://files.test/ws2/a.csv"), files).Read(context.Background(), "ws1", "m1"); !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("foreign media: %v", err)
	}
	got, err := NewReadMediaUseCase(mediaAt("ws1", "https://files.test/ws1/a.csv"), files).Read(context.Background(), "ws1", "m1")
	if err != nil || got.Name != "a.csv" || string(got.Data) != "numero,nome\n" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestReadMediaNeverFetchesAnOutsideURL(t *testing.T) {
	files := &filesStub{}
	if _, err := NewReadMediaUseCase(mediaAt("ws1", "http://169.254.169.254/x"), files).Read(context.Background(), "ws1", "m1"); !errors.Is(err, media.ErrMediaNotFound) || files.downloads != 0 {
		t.Fatalf("err %v downloads %d", err, files.downloads)
	}
}

func TestReadMediaRefusesOversizedFiles(t *testing.T) {
	files := &filesStub{data: make([]byte, media.MaxReadBytes+1)}
	if _, err := NewReadMediaUseCase(mediaAt("ws1", "https://files.test/ws1/big.csv"), files).Read(context.Background(), "ws1", "m1"); !errors.Is(err, media.ErrMediaTooLarge) {
		t.Fatalf("err = %v", err)
	}
}
