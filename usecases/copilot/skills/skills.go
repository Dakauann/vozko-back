package skills

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"vozko/domain/copilot"
)

//go:embed library/*/SKILL.md
var shipped embed.FS

var errNoSkills = errors.New("copilot skills: the library is empty")

type Library struct {
	ordered []copilot.Skill
	byName  map[string]copilot.Skill
}

func Load() (*Library, error) {
	return load(shipped)
}

func load(files fs.FS) (*Library, error) {
	paths, err := fs.Glob(files, "library/*/SKILL.md")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	lib := &Library{byName: make(map[string]copilot.Skill, len(paths))}
	for _, path := range paths {
		raw, err := fs.ReadFile(files, path)
		if err != nil {
			return nil, err
		}
		skill, err := parse(string(raw))
		if err != nil {
			return nil, fmt.Errorf("copilot skills: %s: %w", path, err)
		}
		if _, taken := lib.byName[skill.Name]; taken {
			return nil, fmt.Errorf("copilot skills: %s: name %q is used twice", path, skill.Name)
		}
		lib.byName[skill.Name] = skill
		lib.ordered = append(lib.ordered, skill)
	}
	if len(lib.ordered) == 0 {
		return nil, errNoSkills
	}
	return lib, nil
}

func parse(raw string) (copilot.Skill, error) {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return copilot.Skill{}, errors.New("missing frontmatter")
	}
	header, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return copilot.Skill{}, errors.New("unterminated frontmatter")
	}
	var skill copilot.Skill
	for _, line := range strings.Split(header, "\n") {
		key, value, _ := strings.Cut(line, ":")
		switch strings.TrimSpace(key) {
		case "name":
			skill.Name = strings.TrimSpace(value)
		case "description":
			skill.Description = strings.TrimSpace(value)
		}
	}
	skill.Body = strings.TrimSpace(body)
	return skill, skill.Validate()
}

func (l *Library) All() []copilot.Skill {
	return append([]copilot.Skill(nil), l.ordered...)
}

func (l *Library) Find(name string) (copilot.Skill, bool) {
	skill, ok := l.byName[strings.TrimSpace(name)]
	return skill, ok
}
