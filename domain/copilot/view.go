package copilot

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

type Surface string

const SurfaceAttendance Surface = "attendance"

const viewDateLayout = "2006-01-02"

var (
	ErrInvalidView = errors.New("copilot: invalid view")
	viewToken      = regexp.MustCompile(`^[A-Za-z0-9_:\-]{1,64}$`)
)

type View struct {
	Surface      Surface `json:"surface,omitempty"`
	DateFrom     string  `json:"dateFrom,omitempty"`
	DateTo       string  `json:"dateTo,omitempty"`
	DepartmentID string  `json:"departmentId,omitempty"`
	MemberID     string  `json:"memberId,omitempty"`
	Channel      string  `json:"channel,omitempty"`
}

func (v View) IsZero() bool {
	return v == View{}
}

func (v View) Validate() error {
	if v.IsZero() {
		return nil
	}
	if v.Surface != SurfaceAttendance {
		return fmt.Errorf("%w: unknown surface %q", ErrInvalidView, v.Surface)
	}
	for _, day := range []string{v.DateFrom, v.DateTo} {
		if day == "" {
			continue
		}
		if _, err := time.Parse(viewDateLayout, day); err != nil {
			return fmt.Errorf("%w: %q is not a YYYY-MM-DD date", ErrInvalidView, day)
		}
	}
	for _, token := range []string{v.DepartmentID, v.MemberID, v.Channel} {
		if token != "" && !viewToken.MatchString(token) {
			return fmt.Errorf("%w: %q is not an identifier", ErrInvalidView, token)
		}
	}
	return nil
}
