package media

import (
	"path"
	"strings"
)

func (m *Media) DisplayName() string {
	stored := path.Base(m.URL)
	name := strings.TrimSpace(m.Description)
	if name == "" {
		return stored
	}
	if path.Ext(name) == "" {
		name += path.Ext(stored)
	}
	return name
}
