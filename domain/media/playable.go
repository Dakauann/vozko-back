package media

import "strings"

func (m *Media) PlayableOnCallFor(workspaceID string) bool {
	workspaceID = strings.TrimSpace(workspaceID)
	return m != nil &&
		workspaceID != "" &&
		strings.TrimSpace(m.WorkspaceID) == workspaceID &&
		m.Type == MediaTypeAudio &&
		strings.TrimSpace(m.URL) != ""
}
