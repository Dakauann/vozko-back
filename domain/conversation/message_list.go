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

func ListPayloadOf(input SendListMessageInput) ListPayload {
	payload := ListPayload{
		HeaderText: input.HeaderText,
		BodyText:   input.BodyText,
		FooterText: input.FooterText,
		ButtonText: input.ButtonText,
		Sections:   make([]ListSectionPayload, 0, len(input.Sections)),
	}
	for _, section := range input.Sections {
		rows := make([]ListRowPayload, 0, len(section.Rows))
		for _, row := range section.Rows {
			rows = append(rows, ListRowPayload{ID: row.ID, Title: row.Title, Description: row.Description})
		}
		payload.Sections = append(payload.Sections, ListSectionPayload{Title: section.Title, Rows: rows})
	}
	return payload
}

func (m *Message) AttachList(input SendListMessageInput) error {
	return m.AttachMetadata(ListMetadataKey, ListPayloadOf(input))
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
