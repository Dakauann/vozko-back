package cnefe

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/georef"
)

func TestFilesRefuseAMissingFileWhenDownloadsAreOff(t *testing.T) {
	files := Files{Dir: t.TempDir()}
	if _, err := files.Stream(context.Background(), georef.UFFile{Code: "14", State: "RR"}, func(georef.Record) {}); !errors.Is(err, ErrFileMissing) {
		t.Fatalf("Stream() err = %v, want ErrFileMissing", err)
	}
	if _, err := files.Municipalities(context.Background()); !errors.Is(err, ErrFileMissing) {
		t.Fatalf("Municipalities() err = %v, want ErrFileMissing", err)
	}
}
