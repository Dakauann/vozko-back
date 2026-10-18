package lead

import (
	"bytes"
	"encoding/json"

	"vozko/delivery/http/response"
	"vozko/domain/leadcompany"
	"vozko/domain/shared"
)

type SetLeadCompanyRequest struct {
	CompanyID json.RawMessage `json:"companyId" swaggertype:"string" example:"3c2b1a00-7d1a-4c61-9a0e-2f8d4c1b2a10"`
}

func (r SetLeadCompanyRequest) companyID() (string, bool) {
	raw := bytes.TrimSpace(r.CompanyID)
	if len(raw) == 0 {
		return "", false
	}
	if bytes.Equal(raw, []byte("null")) {
		return "", true
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return "", false
	}
	return id, true
}

type CreateLeadCompanyRequest struct {
	Name         string `json:"name" example:"Acme Indústria"`
	AcceptsLeads *bool  `json:"acceptsLeads,omitempty" example:"true"`
	ImageMediaID string `json:"imageMediaId,omitempty" example:"5a1b2c3d-0000-4e5f-8a9b-0c1d2e3f4a5b"`
}

func (r CreateLeadCompanyRequest) toDomain() leadcompany.Draft {
	accepts := r.AcceptsLeads == nil || *r.AcceptsLeads
	return leadcompany.Draft{Name: r.Name, AcceptsLeads: accepts, ImageMediaID: r.ImageMediaID}
}

type UpdateLeadCompanyRequest struct {
	Name         *string `json:"name,omitempty" example:"Acme Indústria"`
	AcceptsLeads *bool   `json:"acceptsLeads,omitempty" example:"false"`
	ImageMediaID *string `json:"imageMediaId,omitempty" example:""`
}

func (r UpdateLeadCompanyRequest) toDomain() leadcompany.Patch {
	return leadcompany.Patch{Name: r.Name, AcceptsLeads: r.AcceptsLeads, ImageMediaID: r.ImageMediaID}
}

type LeadCompanyImageResponse struct {
	MediaID    string `json:"mediaId"`
	URL        string `json:"url"`
	PreviewURL string `json:"previewUrl,omitempty"`
}

type LeadCompanyResponse struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name" example:"Acme Indústria"`
	AcceptsLeads bool                      `json:"acceptsLeads"`
	Image        *LeadCompanyImageResponse `json:"image,omitempty"`
	MembersCount int                       `json:"membersCount" example:"12"`
	CreatedBy    string                    `json:"createdBy,omitempty"`
	CreatedAt    string                    `json:"createdAt"`
	UpdatedAt    string                    `json:"updatedAt"`
}

type LeadCompanyListResponse struct {
	Data []LeadCompanyResponse   `json:"data"`
	Meta response.PaginationMeta `json:"meta"`
}

func toLeadCompany(c leadcompany.Company) LeadCompanyResponse {
	out := LeadCompanyResponse{
		ID:           c.ID,
		Name:         c.Name,
		AcceptsLeads: c.AcceptsLeads,
		MembersCount: c.MembersCount,
		CreatedBy:    c.CreatedBy,
		CreatedAt:    fmtRFC3339(c.CreatedAt),
		UpdatedAt:    fmtRFC3339(c.UpdatedAt),
	}
	if c.Image != nil {
		out.Image = &LeadCompanyImageResponse{MediaID: c.Image.MediaID, URL: c.Image.URL, PreviewURL: c.Image.PreviewURL}
	}
	return out
}

func toLeadCompanyList(page *shared.PaginatedResult[leadcompany.Company]) LeadCompanyListResponse {
	items := make([]LeadCompanyResponse, 0, len(page.Items))
	for _, c := range page.Items {
		items = append(items, toLeadCompany(c))
	}
	return LeadCompanyListResponse{Data: items, Meta: response.PaginationMeta{
		Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages, TotalItems: page.TotalItems,
	}}
}
