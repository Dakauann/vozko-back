package copilot

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"vozko/domain/crmfilter"
)

type Surface string

const (
	SurfaceAttendance Surface = "attendance"
	SurfaceStudio     Surface = "studio"
	SurfaceLeads      Surface = "leads"
)

type StudioKind string

const (
	StudioImage StudioKind = "image"
	StudioVideo StudioKind = "video"
)

const viewDateLayout = "2006-01-02"

var (
	ErrInvalidView = errors.New("copilot: invalid view")
	viewToken      = regexp.MustCompile(`^[A-Za-z0-9_:\-]{1,64}$`)
)

type View struct {
	Surface      Surface    `json:"surface,omitempty"`
	DateFrom     string     `json:"dateFrom,omitempty"`
	DateTo       string     `json:"dateTo,omitempty"`
	DepartmentID string     `json:"departmentId,omitempty"`
	MemberID     string     `json:"memberId,omitempty"`
	Channel      string     `json:"channel,omitempty"`
	CampaignID   string     `json:"campaignId,omitempty"`
	CampaignType string     `json:"campaignType,omitempty"`
	IncludeAI    *bool      `json:"includeAi,omitempty"`
	ProjectID    string     `json:"projectId,omitempty"`
	ProjectKind  StudioKind `json:"projectKind,omitempty"`

	LeadFilter    *crmfilter.Filter `json:"leadFilter,omitempty"`
	SelectedLeads int               `json:"selectedLeads,omitempty"`
}

func (v View) IsZero() bool {
	return v == View{}
}

func (v View) OnStudio() bool {
	return v.Surface == SurfaceStudio
}

func (v View) Validate() error {
	switch v.Surface {
	case "":
		if !v.IsZero() {
			return fmt.Errorf("%w: filters without a surface", ErrInvalidView)
		}
		return nil
	case SurfaceAttendance:
		return v.validateAttendance()
	case SurfaceStudio:
		return v.validateStudio()
	case SurfaceLeads:
		return v.validateLeads()
	}
	return fmt.Errorf("%w: unknown surface %q", ErrInvalidView, v.Surface)
}

func (v View) validateAttendance() error {
	if v.ProjectID != "" || v.ProjectKind != "" {
		return fmt.Errorf("%w: a studio project on the attendance surface", ErrInvalidView)
	}
	if v.LeadFilter != nil || v.SelectedLeads != 0 {
		return fmt.Errorf("%w: a lead selection on the attendance surface", ErrInvalidView)
	}
	for _, day := range []string{v.DateFrom, v.DateTo} {
		if day == "" {
			continue
		}
		if _, err := time.Parse(viewDateLayout, day); err != nil {
			return fmt.Errorf("%w: %q is not a YYYY-MM-DD date", ErrInvalidView, day)
		}
	}
	for _, token := range []string{v.DepartmentID, v.MemberID, v.Channel, v.CampaignID, v.CampaignType} {
		if token != "" && !viewToken.MatchString(token) {
			return fmt.Errorf("%w: %q is not an identifier", ErrInvalidView, token)
		}
	}
	return nil
}

func (v View) validateStudio() error {
	if !(View{Surface: v.Surface, ProjectID: v.ProjectID, ProjectKind: v.ProjectKind} == v) {
		return fmt.Errorf("%w: attendance filters on the studio surface", ErrInvalidView)
	}
	if !uuidPattern.MatchString(v.ProjectID) {
		return fmt.Errorf("%w: the studio project is not an id", ErrInvalidView)
	}
	if v.ProjectKind != StudioImage && v.ProjectKind != StudioVideo {
		return fmt.Errorf("%w: unknown studio project kind %q", ErrInvalidView, v.ProjectKind)
	}
	return nil
}
