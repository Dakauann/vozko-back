package ticket

import (
	"errors"
	"strings"
	"time"
)

type Status string

const (
	StatusWaitingDocuments Status = "waiting_documents"
	StatusWaitingApproval  Status = "waiting_approval"
	StatusReadyForLabel    Status = "ready_for_label"
	StatusLabelGenerated   Status = "label_generated"
)

var allowedStatuses = map[Status]struct{}{
	StatusWaitingDocuments: {},
	StatusWaitingApproval:  {},
	StatusReadyForLabel:    {},
	StatusLabelGenerated:   {},
}

var (
	ErrTicketNotFound            = errors.New("ticket not found")
	ErrTicketAlreadyExists       = errors.New("ticket already exists for order")
	ErrInvalidTicketStatus       = errors.New("invalid ticket status")
	ErrInvalidStatusTransition   = errors.New("invalid ticket status transition")
	ErrTicketUnauthorized        = errors.New("not allowed to access this ticket")
	ErrDocumentUploadRequired    = errors.New("at least one document must be provided")
	ErrDocumentUnsupportedFormat = errors.New("document format is not supported")
	ErrDocumentNotFound          = errors.New("ticket document not found")
	ErrDocumentTypeRequired      = errors.New("document type is required")
	ErrInvalidDocumentType       = errors.New("invalid document type")
	ErrTrackingCodeRequired      = errors.New("tracking code is required when generating label")
)

type DocumentType string

const (
	DocumentTypeRG                  DocumentType = "rg"
	DocumentTypeAddressProof        DocumentType = "address_proof"
	DocumentTypeMedicalPrescription DocumentType = "medical_prescription"
)

var (
	requiredDocumentTypes = []DocumentType{
		DocumentTypeRG,
		DocumentTypeAddressProof,
	}
	allowedDocumentTypes = map[DocumentType]struct{}{
		DocumentTypeRG:                  {},
		DocumentTypeAddressProof:        {},
		DocumentTypeMedicalPrescription: {},
	}
)

type Ticket struct {
	ID             string     `json:"id"`
	OrderID        string     `json:"orderId"`
	UserID         string     `json:"userId"`
	Status         Status     `json:"status"`
	TrackingCode   *string    `json:"trackingCode,omitempty"`
	ShippingCost   *float64   `json:"shippingCost,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	Documents      []Document `json:"documents,omitempty"`
	LastStatusBy   *string    `json:"lastStatusBy,omitempty"`
	LastStatusAt   *time.Time `json:"lastStatusAt,omitempty"`
	LastRevertedBy *string    `json:"lastRevertedBy,omitempty"`
}

type Document struct {
	ID          string       `json:"id"`
	TicketID    string       `json:"ticketId"`
	Type        DocumentType `json:"type"`
	FileName    string       `json:"fileName"`
	ContentType string       `json:"contentType"`
	URL         string       `json:"url"`
	UploadedBy  string       `json:"uploadedBy"`
	CreatedAt   time.Time    `json:"createdAt"`
}

func ValidateStatus(raw string) (Status, error) {
	status := Status(strings.ToLower(raw))
	if _, ok := allowedStatuses[status]; !ok {
		return "", ErrInvalidTicketStatus
	}
	return status, nil
}

func CanTransition(current, next Status) bool {
	switch current {
	case StatusWaitingDocuments:
		return next == StatusWaitingApproval
	case StatusWaitingApproval:
		return next == StatusReadyForLabel || next == StatusWaitingDocuments
	case StatusReadyForLabel:
		return next == StatusLabelGenerated || next == StatusWaitingDocuments
	case StatusLabelGenerated:
		return next == StatusWaitingDocuments
	default:
		return false
	}
}

func ValidateDocumentType(raw string) (DocumentType, error) {
	if raw == "" {
		return "", ErrDocumentTypeRequired
	}
	docType := DocumentType(strings.ToLower(raw))
	if _, ok := allowedDocumentTypes[docType]; !ok {
		return "", ErrInvalidDocumentType
	}
	return docType, nil
}

func HasRequiredDocuments(docs []Document) bool {
	if len(requiredDocumentTypes) == 0 {
		return true
	}
	found := make(map[DocumentType]bool, len(requiredDocumentTypes))
	for _, doc := range docs {
		found[doc.Type] = true
	}
	for _, req := range requiredDocumentTypes {
		if !found[req] {
			return false
		}
	}
	return true
}
