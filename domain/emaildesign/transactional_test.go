package emaildesign

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/lead"
)

func TestTransactionalRenderNeedsNoUnsubscribeLink(t *testing.T) {
	doc := docWith(headingBlock("h1", "Pedido {{field.pedido}} confirmado"))
	out, err := Render(doc, RenderInput{
		Personalization: Personalization{Fields: map[string]string{"pedido": "A-17"}},
		Transactional:   true,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, rendered := range []string{out.HTML, out.Text} {
		if !strings.Contains(rendered, "Pedido A-17 confirmado") {
			t.Fatalf("merge tag not expanded: %q", rendered)
		}
		if !strings.Contains(rendered, "Acme Ltda") {
			t.Fatalf("footer identity dropped: %q", rendered)
		}
		if strings.Contains(rendered, "Descadastrar") || strings.Contains(rendered, "unsubscribe") {
			t.Fatalf("transactional render carries an unsubscribe line: %q", rendered)
		}
	}
}

func TestTransactionalRenderAcceptsADocumentWithoutFooter(t *testing.T) {
	doc := Document{Version: CurrentVersion, Blocks: []Block{headingBlock("h1", "Seu código: 1234")}}
	if _, err := Render(doc, RenderInput{Transactional: true}); err != nil {
		t.Fatalf("transactional without footer: %v", err)
	}
	if _, err := Render(doc, RenderInput{UnsubscribeURL: "https://mail.example.com/u/x", UnsubscribeLabel: "Sair"}); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("marketing without footer must stay invalid, got %v", err)
	}
}

func TestTransactionalRenderKeepsEveryOtherRule(t *testing.T) {
	cases := []struct {
		name string
		doc  Document
		in   RenderInput
		want error
	}{
		{"broken document", Document{Version: 99}, RenderInput{Transactional: true}, ErrInvalidDocument},
		{"bad lang", docWith(headingBlock("h1", "Oi")), RenderInput{Transactional: true, Lang: "??"}, ErrLangInvalid},
		{"insecure view in browser", docWith(headingBlock("h1", "Oi")), RenderInput{Transactional: true, ViewInBrowserURL: "http://x.example.com", ViewInBrowserLabel: "Ver"}, ErrViewInBrowserURLInvalid},
		{"marketing still needs the link", docWith(headingBlock("h1", "Oi")), RenderInput{}, ErrUnsubscribeURLInvalid},
	}
	for _, tc := range cases {
		if _, err := Render(tc.doc, tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestExpandText(t *testing.T) {
	p := Personalization{FirstName: "Ana", Fields: map[string]string{"codigo": "9981"}}
	cases := []struct {
		name string
		text string
		want string
		err  error
	}{
		{"plain", "Olá", "Olá", nil},
		{"merge tags across lines", "Oi {{first_name}},\n\nseu código é {{field.codigo}}.", "Oi Ana,\n\nseu código é 9981.", nil},
		{"fallback", "Oi {{company|cliente}}", "Oi cliente", nil},
		{"unknown tag", "Oi {{nome}}", "", ErrInvalidDocument},
		{"empty", "  \n ", "", ErrInvalidDocument},
	}
	for _, tc := range cases {
		got, err := ExpandText(tc.text, p)
		if !errors.Is(err, tc.err) || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q, %v", tc.name, got, err, tc.want, tc.err)
		}
	}
}

func TestLeadPersonalization(t *testing.T) {
	facts := lead.EmailFacts{Name: "Ana Souza", Company: "Acme", CustomFields: map[string]any{"plano": "ouro", "vazio": "", "pontos": float64(12)}}
	p := LeadPersonalization(facts, "ana@acme.com.br", map[string]string{"plano": "prata", "pedido": "A-1"})
	if p.FirstName != "Ana" || p.Name != "Ana Souza" || p.Email != "ana@acme.com.br" || p.Company != "Acme" {
		t.Fatalf("identity = %+v", p)
	}
	want := map[string]string{"plano": "prata", "pontos": "12", "pedido": "A-1"}
	if len(p.Fields) != len(want) {
		t.Fatalf("fields = %v, want %v", p.Fields, want)
	}
	for k, v := range want {
		if p.Fields[k] != v {
			t.Fatalf("field %s = %q, want %q", k, p.Fields[k], v)
		}
	}
	if empty := LeadPersonalization(lead.EmailFacts{}, "x@y.com", nil); empty.Email != "x@y.com" || empty.Fields == nil {
		t.Fatalf("no lead = %+v", empty)
	}
}
