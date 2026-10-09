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
)

var skillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type Skill struct {
	Name        string
	Description string
	Body        string
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
	}
	return nil
}
