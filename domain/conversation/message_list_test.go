package conversation

import (
	"encoding/json"
	"testing"
)

func TestAStoredLegacyListStillReadsAsAList(t *testing.T) {
	m := Message{SentBy: SentByWorkflow("wf"), Metadata: json.RawMessage(`{"buttons":{"body_text":"Escolha","buttons":[{"id":"grad","title":"Graduação"}]},"list":{"header_text":"Atendimento","body_text":"Escolha","button_text":"Ver cursos","sections":[{"title":"Cursos","rows":[{"id":"grad","title":"Graduação","description":"EAD"}]}]}}`)}

	content, ok := m.Interactive()
	if !ok || content.Kind != InteractiveKindList || content.ButtonLabel != "Ver cursos" || content.Header == nil || content.Header.Text != "Atendimento" {
		t.Fatalf("content = %+v, %v", content, ok)
	}
	if row := content.Sections[0].Rows[0]; content.Sections[0].Title != "Cursos" || row.ID != "grad" || row.Description != "EAD" {
		t.Fatalf("sections = %+v", content.Sections)
	}
}

func TestAMessageWithoutAListSaysSo(t *testing.T) {
	for _, raw := range []string{``, `{"buttons":{"buttons":[]}}`, `{"list":{"sections":[]}}`, `{"list":"x"}`} {
		m := Message{Metadata: json.RawMessage(raw)}
		if _, ok := m.List(); ok {
			t.Errorf("metadata %q read as a list", raw)
		}
	}
}
