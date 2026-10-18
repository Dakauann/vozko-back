package leadcompany

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"vozko/domain/media"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

const (
	MaxNameLength   = 120
	DefaultPageSize = shared.DefaultPageSize
	MaxPageSize     = shared.MaxPageSize
)

var (
	ErrWorkspaceRequired = errors.New("leadcompany: workspace is required")
	ErrNameRequired      = errors.New("leadcompany: name is required")
	ErrNameTooLong       = errors.New("leadcompany: name is too long")
	ErrNotFound          = errors.New("leadcompany: company not found")
	ErrClosed            = errors.New("leadcompany: the company is not accepting new leads")
	ErrForbidden         = errors.New("leadcompany: not allowed")
	ErrImageInvalid      = errors.New("leadcompany: the image must be an image uploaded to this workspace")
	ErrListQueryInvalid  = errors.New("leadcompany: the list asks for a negative page or a search longer than a name")
)

type Image struct {
	MediaID    string
	URL        string
	PreviewURL string
}

type Company struct {
	ID           string
	WorkspaceID  string
	Name         string
	AcceptsLeads bool
	ImageMediaID string
	Image        *Image
	MembersCount int
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Draft struct {
	Name         string
	AcceptsLeads bool
	ImageMediaID string
}

type Patch struct {
	Name         *string
	AcceptsLeads *bool
	ImageMediaID *string
}

type ListQuery struct {
	Search   string
	Page     int
	PageSize int
}

type ReleasedLead struct {
	LeadID  string
	Version int64
}

type Reader interface {
	GetCompany(ctx context.Context, workspaceID, id string) (Company, error)
}

type Repository interface {
	Reader
	CreateCompany(ctx context.Context, c Company) error
	UpdateCompany(ctx context.Context, c Company) error
	ListCompanies(ctx context.Context, workspaceID string, q ListQuery) (*shared.PaginatedResult[Company], error)
	DeleteCompany(ctx context.Context, workspaceID, id string, release recordevent.Event, at time.Time) ([]ReleasedLead, error)
}

func New(workspaceID, createdBy string, d Draft, now time.Time) (Company, error) {
	c := Company{
		WorkspaceID:  strings.TrimSpace(workspaceID),
		Name:         d.Name,
		AcceptsLeads: d.AcceptsLeads,
		ImageMediaID: d.ImageMediaID,
		CreatedBy:    strings.TrimSpace(createdBy),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	return c.normalized()
}

func (c Company) Apply(p Patch, now time.Time) (Company, error) {
	next := c
	if p.Name != nil {
		next.Name = *p.Name
	}
	if p.AcceptsLeads != nil {
		next.AcceptsLeads = *p.AcceptsLeads
	}
	if p.ImageMediaID != nil {
		next.ImageMediaID, next.Image = *p.ImageMediaID, nil
	}
	next.UpdatedAt = now
	return next.normalized()
}

func (p Patch) NewImage() (string, bool) {
	if p.ImageMediaID == nil {
		return "", false
	}
	id := strings.TrimSpace(*p.ImageMediaID)
	return id, id != ""
}

func (c Company) Admits(workspaceID string) error {
	if c.WorkspaceID != strings.TrimSpace(workspaceID) {
		return ErrNotFound
	}
	if !c.AcceptsLeads {
		return ErrClosed
	}
	return nil
}

func ImageOf(workspaceID string, m *media.Media) (Image, error) {
	if m == nil || m.WorkspaceID != workspaceID || m.Type != media.MediaTypeProductImage {
		return Image{}, ErrImageInvalid
	}
	return Image{MediaID: m.ID, URL: m.URL, PreviewURL: m.PreviewURL}, nil
}

func (q ListQuery) Normalize() (ListQuery, error) {
	q.Search = strings.Join(strings.Fields(q.Search), " ")
	if q.Page < 0 || q.PageSize < 0 || utf8.RuneCountInString(q.Search) > MaxNameLength {
		return ListQuery{}, ErrListQueryInvalid
	}
	page := shared.NormalizePaginationWithin(q.Pagination(), MaxPageSize)
	q.Page, q.PageSize = page.Page, page.PageSize
	return q, nil
}

func (q ListQuery) Pagination() shared.Pagination {
	return shared.Pagination{Page: q.Page, PageSize: q.PageSize}
}

func (c Company) normalized() (Company, error) {
	c.Name = strings.Join(strings.Fields(c.Name), " ")
	c.ImageMediaID = strings.TrimSpace(c.ImageMediaID)
	switch {
	case c.WorkspaceID == "":
		return Company{}, ErrWorkspaceRequired
	case c.Name == "":
		return Company{}, ErrNameRequired
	case utf8.RuneCountInString(c.Name) > MaxNameLength:
		return Company{}, ErrNameTooLong
	}
	if c.ImageMediaID != "" {
		if _, err := uuid.Parse(c.ImageMediaID); err != nil {
			return Company{}, ErrImageInvalid
		}
	}
	return c, nil
}

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrWorkspaceRequired, "workspace_required"},
	{ErrNameRequired, "lead_company_name_required"},
	{ErrNameTooLong, "lead_company_name_too_long"},
	{ErrNotFound, "lead_company_not_found"},
	{ErrClosed, "lead_company_closed"},
	{ErrForbidden, "forbidden"},
	{ErrImageInvalid, "lead_company_image_invalid"},
	{ErrListQueryInvalid, "lead_company_list_invalid"},
}

var inputRefusals = []error{ErrNameRequired, ErrNameTooLong, ErrImageInvalid, ErrListQueryInvalid}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}

func IsInputRefusal(err error) bool {
	for _, refusal := range inputRefusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}
