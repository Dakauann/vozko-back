package customerportfoliohttp

import (
	"time"

	"vozko/delivery/http/response"
	cp "vozko/domain/customerportfolio"
	"vozko/domain/shared"
)

type PlaceRequest struct {
	HolderID string `json:"holderId,omitempty" example:"7a1c2b3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"`
}

type MoveRequest struct {
	HolderID string `json:"holderId" example:"7a1c2b3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"`
}

type SubjectResponse struct {
	Kind        string `json:"kind" enums:"lead,contact" example:"contact"`
	LeadID      string `json:"leadId,omitempty" example:"3c2b1a00-7d1a-4c61-9a0e-2f8d4c1b2a10"`
	ContactType string `json:"contactType,omitempty" enums:"unofficial_whatsapp,telegram,instagram,facebook,webchat" example:"instagram"`
	ContactID   string `json:"contactId,omitempty" example:"0b9a8c7d-6e5f-4a3b-9c2d-1e0f9a8b7c6d"`
}

type EntryResponse struct {
	ID         string          `json:"id" example:"9f8e7d6c-5b4a-4321-8fed-cba987654321"`
	HolderID   string          `json:"holderId" example:"7a1c2b3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"`
	HolderName string          `json:"holderName,omitempty" example:"Marina Costa"`
	AddedBy    string          `json:"addedBy,omitempty"`
	Subject    SubjectResponse `json:"subject"`
	AddedAt    time.Time       `json:"addedAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

type HoldingResponse struct {
	Subject SubjectResponse `json:"subject"`
	Entry   *EntryResponse  `json:"entry"`
}

type DisplayResponse struct {
	Channel   string `json:"channel,omitempty" example:"instagram"`
	Name      string `json:"name,omitempty" example:"Joana Lima"`
	Handle    string `json:"handle,omitempty" example:"joana.lima"`
	Number    string `json:"number,omitempty" example:"5511987654321"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

type RowResponse struct {
	ID             string          `json:"id"`
	HolderID       string          `json:"holderId"`
	HolderName     string          `json:"holderName,omitempty"`
	HolderReceives bool            `json:"holderReceives"`
	AddedBy        string          `json:"addedBy,omitempty"`
	AddedByName    string          `json:"addedByName,omitempty"`
	Subject        SubjectResponse `json:"subject"`
	Display        DisplayResponse `json:"display"`
	AddedAt        time.Time       `json:"addedAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type ListResponse struct {
	Data []RowResponse           `json:"data"`
	Meta response.PaginationMeta `json:"meta"`
}

func toSubject(s cp.Subject) SubjectResponse {
	return SubjectResponse{Kind: string(s.Kind()), LeadID: s.LeadID, ContactType: string(s.ContactType), ContactID: s.ContactID}
}

func toEntry(e cp.Entry, holderName string) *EntryResponse {
	return &EntryResponse{
		ID: e.ID, HolderID: e.HolderID, HolderName: holderName, AddedBy: e.AddedBy,
		Subject: toSubject(e.Subject), AddedAt: e.AddedAt, UpdatedAt: e.UpdatedAt,
	}
}

func toHolding(h cp.Holding) HoldingResponse {
	out := HoldingResponse{Subject: toSubject(h.Subject)}
	if h.Entry != nil {
		out.Entry = toEntry(*h.Entry, h.HolderName)
	}
	return out
}

func toList(page *shared.PaginatedResult[cp.Row]) ListResponse {
	rows := make([]RowResponse, 0, len(page.Items))
	for _, r := range page.Items {
		rows = append(rows, RowResponse{
			ID: r.ID, HolderID: r.HolderID, HolderName: r.HolderName, HolderReceives: r.HolderReceives,
			AddedBy: r.AddedBy, AddedByName: r.AddedByName, Subject: toSubject(r.Subject),
			Display: DisplayResponse{
				Channel: string(r.Display.Channel), Name: r.Display.Name, Handle: r.Display.Handle,
				Number: r.Display.Number, AvatarURL: r.Display.AvatarURL,
			},
			AddedAt: r.AddedAt, UpdatedAt: r.UpdatedAt,
		})
	}
	return ListResponse{Data: rows, Meta: response.PaginationMeta{
		Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages, TotalItems: page.TotalItems,
	}}
}
