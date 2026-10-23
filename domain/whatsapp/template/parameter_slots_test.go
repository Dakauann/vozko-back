package template

import (
	"encoding/json"
	"reflect"
	"testing"
)

func headerAndBody(header, body string) *Template {
	return &Template{Name: "t", Components: []TemplateComponent{
		{Type: "BODY", Text: body, Example: &TemplateExample{BodyText: [][]string{{"Maria", "1º ano"}}, BodyTextNamed: []NamedParamExample{{ParamName: "nome", Example: "Maria"}, {ParamName: "pedido", Example: "123"}}}},
		{Type: "HEADER", Format: "TEXT", Text: header, Example: &TemplateExample{HeaderText: []string{"Prisma"}, HeaderTextNamed: []NamedParamExample{{ParamName: "loja", Example: "Prisma"}}}},
	}}
}

func TestParameterSlotsPutTheHeaderVariableFirstThenTheBody(t *testing.T) {
	cases := []struct {
		name string
		tmpl *Template
		want []ParameterSlot
	}{
		{name: "positional body", tmpl: headerAndBody("Oferta", "Olá {{1}}, série {{2}} e {{1}}"), want: []ParameterSlot{{Name: "1", Example: "Maria"}, {Name: "2", Example: "1º ano"}}},
		{name: "positional header and body", tmpl: headerAndBody("Loja {{1}}", "Olá {{1}}, série {{2}}"), want: []ParameterSlot{{Name: "1", Header: true, Example: "Prisma"}, {Name: "1", Example: "Maria"}, {Name: "2", Example: "1º ano"}}},
		{name: "named header and body", tmpl: headerAndBody("Loja {{loja}}", "Olá {{nome}}, pedido {{pedido}}"), want: []ParameterSlot{{Name: "loja", Header: true, Example: "Prisma"}, {Name: "nome", Example: "Maria"}, {Name: "pedido", Example: "123"}}},
		{name: "no variable", tmpl: headerAndBody("Oferta", "Olá"), want: []ParameterSlot{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.tmpl.ParameterSlots()
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("slots = %+v\nwant %+v", got, tc.want)
			}
			if len(got) != tc.tmpl.ParameterCount() {
				t.Fatalf("%d slots for %d parameters", len(got), tc.tmpl.ParameterCount())
			}
		})
	}
}

func TestSplitParametersSendsTheHeaderSlotsAsHeaderAndTheRestAsBody(t *testing.T) {
	tmpl := headerAndBody("Loja {{loja}}", "Olá {{nome}}, pedido {{pedido}}")
	body, header := tmpl.SplitParameters([]string{"Prisma", "Ana", "77"})
	if !reflect.DeepEqual(body, []string{"Ana", "77"}) || !reflect.DeepEqual(header, []string{"Prisma"}) {
		t.Fatalf("body %q header %q", body, header)
	}
	plain := headerAndBody("Oferta", "Olá {{1}}")
	body, header = plain.SplitParameters([]string{"Ana"})
	if !reflect.DeepEqual(body, []string{"Ana"}) || header != nil {
		t.Fatalf("body %q header %q", body, header)
	}
	if body, header := tmpl.SplitParameters(nil); body != nil || header != nil {
		t.Fatalf("no values = %q %q", body, header)
	}
}

func TestTheCampaignSendCarriesTheHeaderVariableInTheHeader(t *testing.T) {
	tmpl := headerAndBody("Loja {{loja}}", "Olá {{nome}}, pedido {{pedido}}")
	body, header := tmpl.SplitParameters([]string{"Prisma", "Ana", "77"})
	in, err := tmpl.BuildSendInput(SendInputParams{To: "5511900000001", BodyParams: body, HeaderParams: header})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.HeaderTextParams, []string{"Prisma"}) || !reflect.DeepEqual(in.Parameters, []string{"Ana", "77"}) || !reflect.DeepEqual(in.ParameterNames, []string{"nome", "pedido"}) || !in.IsNamedParameterFormat {
		t.Fatalf("send input = %+v", in)
	}
}

func TestTheSentRecordFillsTheHeaderAndTheBodyFromTheSlotValues(t *testing.T) {
	cases := []struct {
		name   string
		tmpl   *Template
		header string
		body   string
	}{
		{name: "positional", tmpl: headerAndBody("Loja {{1}}", "Olá {{1}}, série {{2}}"), header: "Loja Prisma", body: "Olá Ana, série 77"},
		{name: "named", tmpl: headerAndBody("Loja {{loja}}", "Olá {{nome}}, pedido {{pedido}}"), header: "Loja Prisma", body: "Olá Ana, pedido 77"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, metadata, err := tc.tmpl.SlotsHistoryRecord([]string{"Prisma", "Ana", "77"})
			if err != nil {
				t.Fatal(err)
			}
			if body != tc.body {
				t.Fatalf("body = %q", body)
			}
			var facts struct {
				Components []RenderedComponent `json:"components"`
			}
			if err := json.Unmarshal(metadata, &facts); err != nil {
				t.Fatal(err)
			}
			if facts.Components[1].Text != tc.header || facts.Components[0].Text != tc.body {
				t.Fatalf("components = %+v", facts.Components)
			}
			if info := tc.tmpl.RenderSlots([]string{"Prisma", "Ana", "77"}); info["body_text"] != tc.body {
				t.Fatalf("info = %+v", info)
			}
		})
	}
}

func TestTheHistoryOfASingleSendFillsTheHeaderFromItsOwnValues(t *testing.T) {
	tmpl := headerAndBody("Loja {{1}}", "Olá {{1}}, série {{2}}")
	body, metadata, err := tmpl.HistoryRecord([]string{"Ana", "77"}, []string{"Prisma"})
	if err != nil || body != "Olá Ana, série 77" {
		t.Fatalf("body = %q, %v", body, err)
	}
	var facts struct {
		Components []RenderedComponent `json:"components"`
	}
	_ = json.Unmarshal(metadata, &facts)
	if facts.Components[1].Text != "Loja Prisma" {
		t.Fatalf("components = %+v", facts.Components)
	}
	if info := tmpl.RenderInfo([]string{"Ana", "77"}, nil); info["body_text"] != "Olá Ana, série 77" {
		t.Fatalf("body only = %+v", info)
	}
}
