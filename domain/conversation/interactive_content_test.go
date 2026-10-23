package conversation

import (
	"encoding/json"
	"reflect"
	"testing"

	"vozko/domain/channel"
)

func listRequest() SendInteractiveRequest {
	return SendInteractiveRequest{
		Header: "Cartão", Body: "Selecione uma opção", Footer: "Atendimento", ListButton: "Ver opções",
		Options: []InteractiveOption{
			{ID: "beneficios", Title: "Saber sobre benefícios", Description: "Rede credenciada", Section: "Atendimento"},
			{ID: "agendar", Title: "Agendar consulta", Section: "Atendimento"},
			{ID: "pagar", Title: "Pagar mensalidade", Section: "Financeiro"},
		},
		Style: channel.InteractiveStyleList,
	}
}

func TestAListKeepsOnlyTheRowsTheChannelShowedGroupedBySection(t *testing.T) {
	req := listRequest()
	out := ShowingOptions(&SendOutcome{ProviderMessageID: "m1"}, req.ShowingAs(channel.InteractiveStyleList, req.Options[:2]))

	want := &InteractiveContent{
		Kind:        InteractiveKindList,
		Header:      &InteractiveHeader{Type: "text", Text: "Cartão"},
		Body:        "Selecione uma opção",
		Footer:      "Atendimento",
		ButtonLabel: "Ver opções",
		Sections: []InteractiveSection{{Title: "Atendimento", Rows: []InteractiveRow{
			{ID: "beneficios", Title: "Saber sobre benefícios", Description: "Rede credenciada"},
			{ID: "agendar", Title: "Agendar consulta"},
		}}},
	}
	if !reflect.DeepEqual(out.Interactive, want) {
		t.Fatalf("interactive = %+v", out.Interactive)
	}
	if !reflect.DeepEqual(out.ShownOptionIDs, []string{"beneficios", "agendar"}) || out.ProviderMessageID != "m1" {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestButtonsDropDescriptionsAndAComposedChannelShowsOneBody(t *testing.T) {
	req := listRequest()
	content := req.Composed().ShowingAs(channel.InteractiveStyleButtons, req.Options[:1])

	if content.Kind != InteractiveKindButtons || content.Header != nil || content.Footer != "" {
		t.Fatalf("content = %+v", content)
	}
	if content.Body != "Cartão\n\nSelecione uma opção\n\nAtendimento" {
		t.Fatalf("body = %q", content.Body)
	}
	if !reflect.DeepEqual(content.Buttons, []InteractiveAction{{Kind: InteractiveActionReply, ID: "beneficios", Title: "Saber sobre benefícios"}}) {
		t.Fatalf("buttons = %+v", content.Buttons)
	}
}

func TestTheHeaderCanBeFoldedIntoTheBodyAndTheListLabelDefaults(t *testing.T) {
	req := SendInteractiveRequest{Header: "Olá", Body: "Escolha", Footer: "Rodapé"}
	folded := req.WithHeaderInBody()
	if folded.Body != "Olá\n\nEscolha" || folded.Header != "" || folded.Footer != "Rodapé" {
		t.Fatalf("folded = %+v", folded)
	}
	if req.ListButtonLabel() != DefaultListButtonLabel || ListButtonLabel("  Menu ") != "Menu" {
		t.Fatal("the list label falls back to the default only when blank")
	}
	if ShowingOptions(nil, InteractiveContent{}) != nil {
		t.Fatal("no outcome stays no outcome")
	}
}

func TestStoredContentRoundTripsThroughTheWire(t *testing.T) {
	req := listRequest()
	content := req.ShowingAs(channel.InteractiveStyleList, req.Options)
	m := Message{ID: "m1", SentBy: SentByWorkflow("wf-1"), Text: "Selecione uma opção"}
	if err := m.AttachInteractive(&content); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Interactive      *InteractiveContent `json:"interactive"`
		InteractiveReply *InteractiveReply   `json:"interactive_reply"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wire.Interactive, &content) || wire.InteractiveReply != nil {
		t.Fatalf("wire = %s", raw)
	}
	if len(wire.Interactive.Sections) != 2 || wire.Interactive.Sections[1].Title != "Financeiro" {
		t.Fatalf("sections = %+v", wire.Interactive.Sections)
	}
}

func TestUnusableContentIsNeverStored(t *testing.T) {
	m := Message{SentBy: SentByWorkflow("wf-1")}
	for _, c := range []*InteractiveContent{nil, {Kind: "carousel", Body: "x"}, {Kind: InteractiveKindButtons, Body: "x"}, {Kind: InteractiveKindList, Body: "x", ButtonLabel: "Ver"}} {
		if err := m.AttachInteractive(c); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.Metadata) != 0 {
		t.Fatalf("metadata = %s", m.Metadata)
	}
}

func TestAnOperatorButtonSendStoresTheContentAndStaysResendable(t *testing.T) {
	m := Message{SentBy: SentByPerson("user-1"), MessageType: MessageTypeOperator, Text: "Escolha"}
	input := SendButtonInput{HeaderText: "Matrícula", BodyText: "Escolha", FooterText: "Rodapé", Buttons: []ButtonPayload{{ID: "sim", Title: "Sim"}}}
	if err := m.AttachButtons(input); err != nil {
		t.Fatal(err)
	}

	content, ok := m.Interactive()
	if !ok || content.Kind != InteractiveKindButtons || content.Header.Text != "Matrícula" || content.Buttons[0].ID != "sim" {
		t.Fatalf("content = %+v, %v", content, ok)
	}
	again, ok := m.Buttons()
	if !ok || again.HeaderType != "text" || again.HeaderText != "Matrícula" || again.BodyText != "Escolha" || again.FooterText != "Rodapé" || again.Buttons[0] != (ButtonPayload{ID: "sim", Title: "Sim"}) {
		t.Fatalf("resend input = %+v, %v", again, ok)
	}
}

func TestLegacyOperatorButtonsStillRead(t *testing.T) {
	m := Message{SentBy: SentByPerson("user-1"), Metadata: json.RawMessage(`{"buttons":{"body_text":"Escolha","buttons":[{"id":"sim","title":"Sim"}]}}`)}
	content, ok := m.Interactive()
	if !ok || content.Kind != InteractiveKindButtons || content.Body != "Escolha" || content.Buttons[0].Title != "Sim" {
		t.Fatalf("content = %+v, %v", content, ok)
	}
	if !m.HasButtons() {
		t.Fatal("legacy buttons stay resendable")
	}
}

func TestATemplateReadsAsTheSameContentWithItsButtons(t *testing.T) {
	m := Message{
		SentBy:      SentByCampaign("c-1"),
		MessageType: MessageTypeTemplate,
		Text:        "Olá Ana",
		Metadata: json.RawMessage(`{"template_name":"boas_vindas","body_text":"Olá Ana","header_media_url":"https://cdn.exemplo.com/h.jpg","components":[` +
			`{"type":"HEADER","format":"IMAGE"},{"type":"BODY","text":"Olá Ana"},{"type":"FOOTER","text":"Equipe"},` +
			`{"type":"BUTTONS","buttons":[{"type":"QUICK_REPLY","text":"Quero"},{"type":"URL","text":"Site","url":"https://exemplo.com"},{"type":"PHONE_NUMBER","text":"Ligar","phoneNumber":"+5511999990000"},{"type":"COPY_CODE","text":"Copiar código"},{"type":"URL","text":"Pedido","url":"https://exemplo.com/p/{{1}}"}]}]}`),
	}

	content, ok := m.Interactive()
	if !ok {
		t.Fatal("a template must read as interactive content")
	}
	want := InteractiveContent{
		Kind:         InteractiveKindTemplate,
		TemplateName: "boas_vindas",
		Header:       &InteractiveHeader{Type: "image", MediaURL: "https://cdn.exemplo.com/h.jpg"},
		Body:         "Olá Ana",
		Footer:       "Equipe",
		Buttons: []InteractiveAction{
			{Kind: InteractiveActionReply, Title: "Quero"},
			{Kind: InteractiveActionURL, Title: "Site", URL: "https://exemplo.com"},
			{Kind: InteractiveActionPhone, Title: "Ligar", Phone: "+5511999990000"},
			{Kind: InteractiveActionCopyCode, Title: "Copiar código"},
			{Kind: InteractiveActionURL, Title: "Pedido"},
		},
	}
	if !reflect.DeepEqual(content, want) {
		t.Fatalf("content = %+v", content)
	}

	plain := Message{SentBy: SentByCampaign("c-1"), MessageType: MessageTypeTemplate, Text: "oi"}
	if _, ok := plain.Interactive(); ok {
		t.Fatal("a template row without its template metadata has nothing to draw")
	}
}

func TestOnlyOutboundMessagesCarryContentAndOnlyInboundCarryAReply(t *testing.T) {
	content := InteractiveContent{Kind: InteractiveKindButtons, Body: "x", Buttons: []InteractiveAction{{Kind: InteractiveActionReply, ID: "a", Title: "A"}}}
	inbound := Message{SentBy: SentByContact("5511"), Text: "A"}
	if err := inbound.AttachInteractive(&content); err != nil {
		t.Fatal(err)
	}
	if _, ok := inbound.Interactive(); ok {
		t.Fatal("a customer message never draws options")
	}

	outbound := Message{SentBy: SentByWorkflow("wf-1"), Text: "x"}
	if err := outbound.AttachInteractiveReply(&InteractiveReply{Kind: InteractiveReplyList, OptionID: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := outbound.InteractiveReply(); ok {
		t.Fatal("a business message never reads as a customer choice")
	}
}

func TestACustomerChoiceReadsFromTheTypedKeyAndFromEveryLegacyShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want InteractiveReply
	}{
		{"typed", `{"interactive_reply":{"kind":"list","option_id":"agendar","title":"Agendar"}}`, InteractiveReply{Kind: InteractiveReplyList, OptionID: "agendar", Title: "Agendar"}},
		{"official list", `{"interactive":{"type":"list_reply","id":"agendar","title":"Agendar"}}`, InteractiveReply{Kind: InteractiveReplyList, OptionID: "agendar", Title: "Agendar"}},
		{"official button", `{"interactive":{"type":"button_reply","id":"sim","title":"Sim"}}`, InteractiveReply{Kind: InteractiveReplyButton, OptionID: "sim", Title: "Sim"}},
		{"template button", `{"button":{"payload":"vz1:1","text":"SAIR"}}`, InteractiveReply{Kind: InteractiveReplyTemplateButton, OptionID: "vz1:1", Title: "SAIR"}},
		{"unofficial", `{"providerEvent":"messages","selectedOptionId":"row_1"}`, InteractiveReply{Kind: InteractiveReplyOption, OptionID: "row_1", Title: "Agendar"}},
	} {
		m := Message{SentBy: SentByContact("5511"), Text: "Agendar", Metadata: json.RawMessage(tc.raw)}
		got, ok := m.InteractiveReply()
		if !ok || got != tc.want {
			t.Errorf("%s: reply = %+v, %v", tc.name, got, ok)
		}
	}

	plain := Message{SentBy: SentByContact("5511"), Text: "oi", Metadata: json.RawMessage(`{"providerEvent":"messages"}`)}
	if _, ok := plain.InteractiveReply(); ok {
		t.Fatal("a typed message is not a choice")
	}
}

func TestAReplyIsSentOnTheWire(t *testing.T) {
	m := Message{ID: "r1", SentBy: SentByContact("5511"), Text: "Agendar"}
	if err := m.AttachInteractiveReply(&InteractiveReply{Kind: InteractiveReplyList, OptionID: "agendar", Title: "Agendar"}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		InteractiveReply *InteractiveReply `json:"interactive_reply"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || wire.InteractiveReply == nil || wire.InteractiveReply.OptionID != "agendar" {
		t.Fatalf("wire = %s", raw)
	}
}

func TestOfficialListAndButtonInputsBecomeContent(t *testing.T) {
	list := ListMessageContent(SendListMessageInput{BodyText: "Escolha", ButtonText: "Ver", Sections: []ListSection{{Title: "A", Rows: []ListRow{{ID: "1", Title: "Um", Description: "d"}}}}})
	if list.Kind != InteractiveKindList || list.ButtonLabel != "Ver" || list.Sections[0].Rows[0].Description != "d" || list.Header != nil {
		t.Fatalf("list = %+v", list)
	}
	buttons := ButtonMessageContent(SendButtonMessageInput{HeaderType: HeaderTypeText, HeaderText: "Oi", BodyText: "Escolha", Buttons: []InteractiveButton{{Type: ButtonTypeReply, ID: "s", Title: "Sim"}}})
	if buttons.Header == nil || buttons.Header.Text != "Oi" || !reflect.DeepEqual(buttons.OptionIDs(), []string{"s"}) {
		t.Fatalf("buttons = %+v", buttons)
	}
}
