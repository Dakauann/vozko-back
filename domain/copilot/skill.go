package copilot

import (
	"errors"
	"regexp"
	"strings"

	"vozko/domain/shared"
)

var (
	ErrSkillName        = errors.New("copilot skill: name must be lowercase words joined by hyphens")
	ErrSkillDescription = errors.New("copilot skill: description is required")
	ErrSkillBody        = errors.New("copilot skill: body is required")
	ErrSkillDash        = errors.New("copilot skill: em and en dashes are not allowed")
	ErrSkillSurface     = errors.New("copilot skill: surface must be empty, studio, panel or email")
)

var skillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type SkillSurface string

const (
	SkillEverywhere SkillSurface = ""
	SkillStudioOnly SkillSurface = "studio"
	SkillPanelOnly  SkillSurface = "panel"
	SkillEmailWork  SkillSurface = "email"
)

type Skill struct {
	Name        string
	Description string
	Body        string
	Surface     SkillSurface
}

func (s Skill) OfferedOn(view View) bool {
	switch s.Surface {
	case SkillStudioOnly:
		return view.OnStudio()
	case SkillPanelOnly:
		return !view.Focused()
	case SkillEmailWork:
		return view.OnEmailDocument() || !view.Focused()
	}
	return true
}

func (s Skill) Title() string {
	for _, line := range strings.Split(s.Body, "\n") {
		if heading, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok && strings.TrimSpace(heading) != "" {
			return strings.TrimSpace(heading)
		}
	}
	return s.Name
}

func (s Skill) Validate() error {
	switch {
	case !skillName.MatchString(s.Name):
		return ErrSkillName
	case strings.TrimSpace(s.Description) == "":
		return ErrSkillDescription
	case strings.TrimSpace(s.Body) == "":
		return ErrSkillBody
	case shared.HasLongDash(s.Description + s.Body):
		return ErrSkillDash
	case s.Surface != SkillEverywhere && s.Surface != SkillStudioOnly && s.Surface != SkillPanelOnly && s.Surface != SkillEmailWork:
		return ErrSkillSurface
	}
	return nil
}
