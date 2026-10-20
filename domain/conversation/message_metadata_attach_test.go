package conversation

import (
	"encoding/json"
	"testing"
)

func TestAttachMetadataKeepsWhatIsAlreadyThere(t *testing.T) {
	m := &Message{}
	if err := m.AttachButtons(SendButtonInput{BodyText: "x", Buttons: []ButtonPayload{{ID: "a", Title: "A"}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.AttachMetadata("form", map[string]string{"kind": "message"}); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(m.Metadata, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["form"]) != `{"kind":"message"}` || !m.HasButtons() {
		t.Fatalf("metadata = %s", m.Metadata)
	}
}
