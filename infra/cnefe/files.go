package cnefe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"vozko/domain/georef"
)

var ErrFileMissing = errors.New("cnefe: the file is not on disk and downloads are off")

type Files struct {
	Dir        string
	Download   bool
	Downloader *Downloader
}

func (f Files) ensure(ctx context.Context, url, name string) (string, error) {
	path := filepath.Join(f.Dir, name)
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return path, nil
	}
	if !f.Download || f.Downloader == nil {
		return "", fmt.Errorf("%w: %s", ErrFileMissing, path)
	}
	if err := f.Downloader.Fetch(ctx, url, path); err != nil {
		return "", err
	}
	return path, nil
}

func (f Files) Stream(ctx context.Context, uf georef.UFFile, each func(georef.Record)) (int64, error) {
	path, err := f.ensure(ctx, BaseURL+uf.FileName(), uf.FileName())
	if err != nil {
		return 0, err
	}
	return StreamZip(path, each)
}

func (f Files) Municipalities(ctx context.Context) (map[string]georef.Municipality, error) {
	path, err := f.ensure(ctx, MunicipalityURL, "municipios.json")
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ReadMunicipalities(file)
}
