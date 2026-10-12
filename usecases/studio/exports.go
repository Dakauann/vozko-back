package studio_usecase

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/google/uuid"

	"vozko/domain/media"
	"vozko/domain/studio"
)

type ExportLibrary interface {
	RegisterStored(workspaceID, id, key string, mediaType media.MediaType, description string) (media.Media, error)
}

type ExportService struct {
	projects studio.Repository
	storage  media.StreamStorage
	library  ExportLibrary
	newID    func() string
}

var exportMediaTypes = map[studio.ExportFormat]media.MediaType{
	studio.ExportMP4:  media.MediaTypeProductVideo,
	studio.ExportGIF:  media.MediaTypeProductImage,
	studio.ExportWebP: media.MediaTypeProductImage,
	studio.ExportSVG:  media.MediaTypeProductImage,
	studio.ExportPDF:  media.MediaTypeDocumentPdf,
	studio.ExportPNG:  media.MediaTypeProductImage,
	studio.ExportJPEG: media.MediaTypeProductImage,
}

func NewExportService(projects studio.Repository, storage media.StreamStorage, library ExportLibrary) (*ExportService, error) {
	if projects == nil || storage == nil || library == nil {
		return nil, ErrMissingDependency
	}
	return &ExportService{projects: projects, storage: storage, library: library, newID: uuid.NewString}, nil
}

func (s *ExportService) Save(ctx context.Context, workspaceID, projectID, description string, file io.Reader) (media.Media, error) {
	project, err := s.projects.Get(ctx, workspaceID, projectID)
	if err != nil {
		return media.Media{}, err
	}
	named, err := studio.ExportDescription(description, project.Name)
	if err != nil {
		return media.Media{}, err
	}
	body := bufio.NewReaderSize(file, studio.ExportHeadBytes)
	format, err := exportFormat(body, project.Kind)
	if err != nil {
		return media.Media{}, err
	}
	content, err := exportContent(format, studio.NewExportReader(body))
	if err != nil {
		return media.Media{}, err
	}
	id := s.newID()
	key := studio.ExportKey(workspaceID, project.ID, id, format)
	if err := s.storage.PutStream(ctx, key, format.ContentType(), content); err != nil {
		return media.Media{}, err
	}
	saved, err := s.library.RegisterStored(workspaceID, id, key, exportMediaTypes[format], named)
	if err != nil {
		return media.Media{}, errors.Join(err, s.storage.DeleteFile(ctx, key))
	}
	return saved, nil
}

func exportFormat(body *bufio.Reader, kind studio.Kind) (studio.ExportFormat, error) {
	head, err := body.Peek(studio.ExportHeadBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	format, ok := studio.DetectExport(head)
	if !ok {
		return "", studio.ErrInvalidExport
	}
	if !studio.ExportAllowed(kind, format) {
		return "", studio.ErrExportFormat
	}
	return format, nil
}

func exportContent(format studio.ExportFormat, body io.Reader) (io.Reader, error) {
	if format != studio.ExportSVG {
		return body, nil
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	if err := studio.ValidateExportSVG(raw); err != nil {
		return nil, err
	}
	return bytes.NewReader(raw), nil
}
