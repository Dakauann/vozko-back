package ticket_usecase

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/ticket"
)

var allowedContentTypes = map[string]struct{}{
	"application/pdf": {},
	"image/png":       {},
	"image/jpeg":      {},
	"image/jpg":       {},
}

type uploadTicketDocumentUseCase struct {
	ticketRepo  ticket.Repository
	fileStorage ticket.FileStorage
}

func NewUploadTicketDocumentUseCase(ticketRepo ticket.Repository, fileStorage ticket.FileStorage) ticket.UploadTicketDocumentUseCase {
	return &uploadTicketDocumentUseCase{
		ticketRepo:  ticketRepo,
		fileStorage: fileStorage,
	}
}

func (uc *uploadTicketDocumentUseCase) Execute(userID, ticketID, documentType, fileName, contentType string, content []byte) (*ticket.Document, error) {
	if len(content) == 0 {
		return nil, ticket.ErrDocumentUploadRequired
	}

	if _, ok := allowedContentTypes[strings.ToLower(contentType)]; !ok {
		return nil, ticket.ErrDocumentUnsupportedFormat
	}

	t, err := uc.ticketRepo.FindByID(ticketID)
	if err != nil {
		return nil, err
	}

	if t.UserID != userID {
		return nil, ticket.ErrTicketUnauthorized
	}

	docType, err := ticket.ValidateDocumentType(documentType)
	if err != nil {
		return nil, err
	}

	documentID := uuid.New().String()
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		switch strings.ToLower(contentType) {
		case "application/pdf":
			ext = ".pdf"
		case "image/png":
			ext = ".png"
		default:
			ext = ".jpg"
		}
	}

	safeName := strings.ReplaceAll(strings.ToLower(fileName), " ", "_")
	if safeName == "" || safeName == ext {
		safeName = documentID + ext
	} else if !strings.HasSuffix(safeName, ext) {
		safeName += ext
	}
	key := fmt.Sprintf("tickets/%s/%s/%s", t.ID, docType, safeName)

	if err := uc.fileStorage.UploadFile(key, content, contentType); err != nil {
		return nil, err
	}

	now := time.Now()
	doc := &ticket.Document{
		ID:          documentID,
		TicketID:    t.ID,
		Type:        docType,
		FileName:    fileName,
		ContentType: strings.ToLower(contentType),
		URL:         uc.fileStorage.GetFileURL(key),
		UploadedBy:  userID,
		CreatedAt:   now,
	}

	if err := uc.ticketRepo.AddDocument(doc); err != nil {
		return nil, err
	}

	if t.Status == ticket.StatusWaitingDocuments {
		t.Documents = append(t.Documents, *doc)
		if ticket.HasRequiredDocuments(t.Documents) {
			t.Status = ticket.StatusWaitingApproval
			t.LastStatusBy = &userID
			timestamp := now
			t.LastStatusAt = &timestamp
			if err := uc.ticketRepo.Update(t); err != nil {
				return nil, err
			}
		}
	}

	return doc, nil
}
