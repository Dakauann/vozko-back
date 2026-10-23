package media

import "strings"

func (t MediaType) Sendable() bool {
	switch t {
	case MediaTypeProductImage, MediaTypeProductVideo, MediaTypeAudio, MediaTypeSticker,
		MediaTypeDocument, MediaTypeDocumentPdf, MediaTypeDocumentDoc:
		return true
	}
	return false
}

func (m *Media) SendableFor(workspaceID string) bool {
	workspaceID = strings.TrimSpace(workspaceID)
	return m != nil &&
		workspaceID != "" &&
		strings.TrimSpace(m.WorkspaceID) == workspaceID &&
		m.Type.Sendable() &&
		strings.TrimSpace(m.URL) != ""
}
