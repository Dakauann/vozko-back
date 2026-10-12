package studio

import (
	"bytes"
	"errors"
	"io"
	"path"
	"strings"
	"unicode/utf8"
)

type ExportFormat string

const (
	ExportMP4  ExportFormat = "mp4"
	ExportGIF  ExportFormat = "gif"
	ExportWebP ExportFormat = "webp"
	ExportPDF  ExportFormat = "pdf"
	ExportSVG  ExportFormat = "svg"
	ExportPNG  ExportFormat = "png"
	ExportJPEG ExportFormat = "jpeg"
)

const (
	MaxExportBytes            = 512 << 20
	MaxExportDescriptionRunes = 255
	ExportHeadBytes           = 21
	exportsFolder             = "studio-exports"
	webpAnimationFlag         = 0x02
	webpFlagsOffset           = 20
)

var (
	ErrExportTooLarge = errors.New("studio: the export is larger than allowed")
	ErrInvalidExport  = errors.New("studio: the export is not a file the editor makes")
)

type exportSpec struct {
	contentType string
	extension   string
	kind        Kind
}

var exportSpecs = map[ExportFormat]exportSpec{
	ExportMP4:  {contentType: "video/mp4", extension: ".mp4", kind: KindVideo},
	ExportGIF:  {contentType: "image/gif", extension: ".gif", kind: KindVideo},
	ExportWebP: {contentType: "image/webp", extension: ".webp", kind: KindVideo},
	ExportPDF:  {contentType: "application/pdf", extension: ".pdf", kind: KindImage},
	ExportSVG:  {contentType: "image/svg+xml", extension: ".svg", kind: KindImage},
	ExportPNG:  {contentType: "image/png", extension: ".png", kind: KindVideo},
	ExportJPEG: {contentType: "image/jpeg", extension: ".jpg", kind: KindVideo},
}

var (
	pngSignature  = []byte("\x89PNG\r\n\x1a\n")
	jpegSignature = []byte{0xff, 0xd8, 0xff}
)

func (f ExportFormat) ContentType() string {
	return exportSpecs[f].contentType
}

func ExportAllowed(kind Kind, format ExportFormat) bool {
	spec, ok := exportSpecs[format]
	return ok && spec.kind == kind
}

func DetectExport(head []byte) (ExportFormat, bool) {
	switch {
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return ExportMP4, true
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return ExportGIF, true
	case animatedWebPHead(head):
		return ExportWebP, true
	case bytes.HasPrefix(head, []byte("%PDF-")):
		return ExportPDF, true
	case bytes.HasPrefix(head, []byte("<svg ")):
		return ExportSVG, true
	case bytes.HasPrefix(head, pngSignature):
		return ExportPNG, true
	case bytes.HasPrefix(head, jpegSignature):
		return ExportJPEG, true
	}
	return "", false
}

func animatedWebPHead(head []byte) bool {
	return len(head) > webpFlagsOffset && string(head[0:4]) == "RIFF" && string(head[8:16]) == "WEBPVP8X" && head[webpFlagsOffset]&webpAnimationFlag != 0
}

func ExportKey(workspaceID, projectID, exportID string, format ExportFormat) string {
	return path.Join(exportsFolder, workspaceID, projectID, exportID+exportSpecs[format].extension)
}

func ExportDescription(requested, projectName string) (string, error) {
	clean := strings.Join(strings.Fields(requested), " ")
	if clean == "" {
		return projectName, nil
	}
	if utf8.RuneCountInString(clean) > MaxExportDescriptionRunes {
		return "", ErrInvalidExport
	}
	return clean, nil
}

type exportReader struct {
	body io.Reader
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
	return n, err
}
