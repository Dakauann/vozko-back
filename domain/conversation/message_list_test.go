package conversation

import (
	"encoding/json"
	"testing"
)

func TestAListMessageKeepsItsButtonTextSectionsAndRowDescriptions(t *testing.T) {
	input := SendListMessageInput{
		HeaderText: "Atendimento", BodyText: "Escolha uma opção", FooterText: "Responda pela lista", ButtonText: "Ver opções",
		Sections: []ListSection{
			{Title: "Cursos", Rows: []ListRow{{ID: "grad", Title: "Graduação", Description: "Presencial e EAD"}, {ID: "pos", Title: "Pós"}}},
			{Title: "Outros", Rows: []ListRow{{ID: "humano", Title: "Falar com alguém"}}},
		},
	}
	var m Message
	if err := m.AttachList(input); err != nil {
		t.Fatal(err)
	}

	list, ok := m.List()
	if !ok {
		t.Fatalf("metadata = %s, want a list", m.Metadata)
	}
	if list.ButtonText != "Ver opções" || list.BodyText != "Escolha uma opção" || list.HeaderText != "Atendimento" || list.FooterText != "Responda pela lista" {
		t.Fatalf("list = %+v", list)
	}
	if len(list.Sections) != 2 || list.Sections[0].Title != "Cursos" || len(list.Sections[0].Rows) != 2 {
		t.Fatalf("sections = %+v", list.Sections)
	}
	if row := list.Sections[0].Rows[0]; row.ID != "grad" || row.Title != "Graduação" || row.Description != "Presencial e EAD" {
		t.Fatalf("row = %+v", row)
	}

	var wire map[string]map[string]any
	if err := json.Unmarshal(m.Metadata, &wire); err != nil {
		t.Fatal(err)
	}
	if wire[ListMetadataKey]["button_text"] != "Ver opções" {
		t.Fatalf("wire = %v, want snake_case like the buttons metadata", wire)
	}
}

func TestAListAttachesBesideTheButtonsTheResendRuleReads(t *testing.T) {
	var m Message
	if err := m.AttachButtons(SendButtonInput{BodyText: "Escolha", Buttons: []ButtonPayload{{ID: "a", Title: "A"}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.AttachList(SendListMessageInput{BodyText: "Escolha", ButtonText: "Ver", Sections: []ListSection{{Rows: []ListRow{{ID: "a", Title: "A"}}}}}); err != nil {
		t.Fatal(err)
	}
	if !m.HasButtons() {
		t.Fatal("the list replaced the buttons metadata")
	}
	if _, ok := m.List(); !ok {
		t.Fatal("the list is missing")
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
