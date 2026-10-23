package conversation

import (
	"bytes"
	"encoding/json"
)

const ButtonsMetadataKey = "buttons"

var buttonsMetadataMarker = []byte(`"` + ButtonsMetadataKey + `"`)

func (m *Message) AttachButtons(input SendButtonInput) error {
	content := ButtonInputContent(input)
	return m.AttachInteractive(&content)
}

func (m *Message) Buttons() (SendButtonInput, bool) {
	if content, ok := m.storedInteractive(); ok {
		return content.buttonInput()
	}
	return m.legacyButtons()
}

func (m *Message) legacyButtons() (SendButtonInput, bool) {
	if m == nil || !bytes.Contains(m.Metadata, buttonsMetadataMarker) {
		return SendButtonInput{}, false
	}
	stored, ok := metadataFields(m.Metadata)[ButtonsMetadataKey]
	if !ok {
		return SendButtonInput{}, false
	}
	var input SendButtonInput
	if err := json.Unmarshal(stored, &input); err != nil || len(input.Buttons) == 0 {
		return SendButtonInput{}, false
	}
	return input, true
}

func (m *Message) HasButtons() bool {
	_, ok := m.Buttons()
	return ok
}

func (m *Message) AttachMetadata(key string, value any) error {
	updated, err := withMetadataValue(m.Metadata, key, value)
	if err != nil {
		return err
	}
	m.Metadata = updated
	return nil
}
