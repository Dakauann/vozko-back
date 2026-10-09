package studio

import (
	"errors"
	"io"
	"path"
)

const (
	MaxExportBytes    = 95 << 20
	ExportContentType = "video/mp4"
	exportsFolder     = "studio-exports"
	exportExtension   = ".mp4"
	mp4HeaderBytes    = 8
	mp4BrandOffset    = 4
	mp4Brand          = "ftyp"
)

var (
	ErrExportTooLarge = errors.New("studio: the export is larger than allowed")
	ErrInvalidExport  = errors.New("studio: the export is not a valid MP4")
)

func ExportKey(workspaceID, projectID, exportID string) string {
	return path.Join(exportsFolder, workspaceID, projectID, exportID+exportExtension)
}

type exportReader struct {
	body io.Reader
	head []byte
	read int64
}

func NewExportReader(body io.Reader) io.Reader {
	return &exportReader{body: body}
}

func (e *exportReader) Read(p []byte) (int, error) {
	n, err := e.body.Read(p)
	e.read += int64(n)
	if e.read > MaxExportBytes {
		return 0, ErrExportTooLarge
	}
	if len(e.head) < mp4HeaderBytes {
		e.head = append(e.head, p[:min(n, mp4HeaderBytes-len(e.head))]...)
		if len(e.head) == mp4HeaderBytes && string(e.head[mp4BrandOffset:]) != mp4Brand {
			return 0, ErrInvalidExport
		}
	}
	if errors.Is(err, io.EOF) && len(e.head) < mp4HeaderBytes {
		return 0, ErrInvalidExport
	}
	return n, err
}
