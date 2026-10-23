package conversation

import (
	"bytes"
	"encoding/json"
)

const ListMetadataKey = "list"

var listMetadataMarker = []byte(`"` + ListMetadataKey + `"`)

type ListRowPayload struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type ListSectionPayload struct {
	Title string           `json:"title,omitempty"`
	Rows  []ListRowPayload `json:"rows"`
}

type ListPayload struct {
	HeaderText string               `json:"header_text,omitempty"`
	BodyText   string               `json:"body_text"`
	FooterText string               `json:"footer_text,omitempty"`
	ButtonText string               `json:"button_text"`
	Sections   []ListSectionPayload `json:"sections"`
}

func (m *Message) List() (ListPayload, bool) {
	if m == nil || !bytes.Contains(m.Metadata, listMetadataMarker) {
		return ListPayload{}, false
	}
	stored, ok := metadataFields(m.Metadata)[ListMetadataKey]
	if !ok {
		return ListPayload{}, false
	}
	var list ListPayload
	if err := json.Unmarshal(stored, &list); err != nil || len(list.Sections) == 0 {
		return ListPayload{}, false
	}
	return list, true
}
