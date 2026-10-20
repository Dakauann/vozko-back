package whatsappform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type flowJSON struct {
	Version string `json:"version"`
	Screens []struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Terminal  bool     `json:"terminal"`
		Sensitive []string `json:"sensitive"`
		Layout    struct {
			Type     string           `json:"type"`
			Children []map[string]any `json:"children"`
		} `json:"layout"`
	} `json:"screens"`
}

func compileOK(t *testing.T, doc Document, assets Assets) (Compiled, flowJSON) {
	t.Helper()
	compiled, err := Compile(doc, assets)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var out flowJSON
	if err := json.Unmarshal(compiled.JSON, &out); err != nil {
		t.Fatalf("compiled JSON does not decode: %v\n%s", err, compiled.JSON)
	}
	return compiled, out
}

func component(t *testing.T, children []map[string]any, kind string) map[string]any {
	t.Helper()
	for _, c := range children {
		if c["type"] == kind {
			return c
		}
	}
	t.Fatalf("no %s in %+v", kind, children)
	return nil
}

func action(t *testing.T, footer map[string]any) map[string]any {
	t.Helper()
	a, ok := footer["on-click-action"].(map[string]any)
	if !ok {
		t.Fatalf("footer without action: %+v", footer)
	}
	return a
}

func TestCompilesASingleScreenFormThatCompletesWithEveryAnswer(t *testing.T) {
	compiled, out := compileOK(t, oneScreen(fieldBlock(textField("f_name", "Nome"))), nil)
	if out.Version != FlowJSONVersion || compiled.FlowVersion != FlowJSONVersion {
		t.Fatalf("version = %q, want %q", out.Version, FlowJSONVersion)
	}
	screen := out.Screens[0]
	if screen.ID != "SCREEN_A" || !screen.Terminal || screen.Title != "Seus dados" || screen.Layout.Type != "SingleColumnLayout" {
		t.Fatalf("screen = %+v", screen)
	}
	input := component(t, screen.Layout.Children, "TextInput")
	want := map[string]any{"type": "TextInput", "name": "f_name", "label": "Nome", "input-type": "text", "required": true}
	if !reflect.DeepEqual(input, want) {
		t.Fatalf("input = %+v, want %+v", input, want)
	}
	footer := component(t, screen.Layout.Children, "Footer")
	if footer["label"] != "Enviar" {
		t.Fatalf("footer label = %v", footer["label"])
	}
	a := action(t, footer)
	if a["name"] != "complete" || !reflect.DeepEqual(a["payload"], map[string]any{"f_name": "${form.f_name}"}) {
		t.Fatalf("terminal action = %+v", a)
	}
}

func TestAnswersOfEarlierScreensTravelToTheEndThroughGlobalReferences(t *testing.T) {
	_, out := compileOK(t, twoScreens(), nil)
	first := action(t, component(t, out.Screens[0].Layout.Children, "Footer"))
	if first["name"] != "navigate" || !reflect.DeepEqual(first["next"], map[string]any{"type": "screen", "name": "SCREEN_B"}) {
		t.Fatalf("first screen action = %+v", first)
	}
	if out.Screens[0].Terminal {
		t.Fatal("only the last screen is terminal")
	}
	last := action(t, component(t, out.Screens[1].Layout.Children, "Footer"))
	want := map[string]any{"f_name": "${screen.SCREEN_A.form.f_name}", "f_topic": "${form.f_topic}"}
	if !reflect.DeepEqual(last["payload"], want) {
		t.Fatalf("complete payload = %+v, want %+v", last["payload"], want)
	}
}

func TestChoiceComponentsCarryTheirOptionsAsDataSource(t *testing.T) {
	multi := choiceField("f_c", FieldCheckbox, "o_a", "o_b")
	multi.Min, multi.Max = 1, 2
	multi.Options[1].Description = "Detalhe"
	_, out := compileOK(t, oneScreen(fieldBlock(multi)), nil)
	group := component(t, out.Screens[0].Layout.Children, "CheckboxGroup")
	source := []any{
		map[string]any{"id": "o_a", "title": "Opção o_a"},
		map[string]any{"id": "o_b", "title": "Opção o_b", "description": "Detalhe"},
	}
	if !reflect.DeepEqual(group["data-source"], source) || group["min-selected-items"] != float64(1) || group["max-selected-items"] != float64(2) {
		t.Fatalf("checkbox group = %+v", group)
	}
	for kind, component_ := range map[FieldKind]string{FieldRadio: "RadioButtonsGroup", FieldDropdown: "Dropdown", FieldMultiChips: "ChipsSelector"} {
		_, out := compileOK(t, oneScreen(fieldBlock(choiceField("f_c", kind, "o_a", "o_b"))), nil)
		component(t, out.Screens[0].Layout.Children, component_)
	}
}

func TestEveryFieldKindMapsToItsMetaComponent(t *testing.T) {
	cases := []struct {
		field *Field
		want  map[string]any
	}{
		{&Field{Key: "f_e", Kind: FieldEmail, Label: "E-mail"}, map[string]any{"type": "TextInput", "name": "f_e", "label": "E-mail", "input-type": "email", "required": false}},
		{&Field{Key: "f_p", Kind: FieldPhone, Label: "Telefone"}, map[string]any{"type": "TextInput", "name": "f_p", "label": "Telefone", "input-type": "phone", "required": false}},
		{&Field{Key: "f_n", Kind: FieldNumber, Label: "Idade", Min: 1, Max: 3, Help: "Só números", Pattern: "^[0-9]+$"}, map[string]any{
			"type": "TextInput", "name": "f_n", "label": "Idade", "input-type": "number", "required": false,
			"min-chars": float64(1), "max-chars": float64(3), "helper-text": "Só números", "pattern": "^[0-9]+$"}},
		{&Field{Key: "f_l", Kind: FieldLongText, Label: "Mensagem", Max: 300}, map[string]any{"type": "TextArea", "name": "f_l", "label": "Mensagem", "required": false, "max-length": float64(300)}},
		{&Field{Key: "f_d", Kind: FieldDate, Label: "Data", MinDate: "2026-01-01", MaxDate: "2026-12-31"}, map[string]any{
			"type": "DatePicker", "name": "f_d", "label": "Data", "required": false, "min-date": "2026-01-01", "max-date": "2026-12-31"}},
		{&Field{Key: "f_r", Kind: FieldDateRange, Label: "Período"}, map[string]any{"type": "CalendarPicker", "name": "f_r", "label": "Período", "mode": "range", "required": false}},
		{&Field{Key: "f_o", Kind: FieldOptIn, Label: "Aceito receber ofertas", TermsURL: "https://example.com/termos"}, map[string]any{
			"type": "OptIn", "name": "f_o", "label": "Aceito receber ofertas", "required": false,
			"on-click-action": map[string]any{"name": "open_url", "url": "https://example.com/termos"}}},
		{&Field{Key: "f_ph", Kind: FieldPhoto, Label: "Fotos", Help: "Até 3", Min: 1, Max: 3}, map[string]any{
			"type": "PhotoPicker", "name": "f_ph", "label": "Fotos", "description": "Até 3", "photo-source": "camera_gallery",
			"min-uploaded-photos": float64(1), "max-uploaded-photos": float64(3)}},
		{&Field{Key: "f_doc", Kind: FieldDocument, Label: "Documento", MimeTypes: []string{"application/pdf"}}, map[string]any{
			"type": "DocumentPicker", "name": "f_doc", "label": "Documento", "min-uploaded-documents": float64(0),
			"max-uploaded-documents": float64(1), "allowed-mime-types": []any{"application/pdf"}}},
	}
	for _, tc := range cases {
		t.Run(string(tc.field.Kind), func(t *testing.T) {
			_, out := compileOK(t, oneScreen(fieldBlock(tc.field)), nil)
			got := component(t, out.Screens[0].Layout.Children, tc.want["type"].(string))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("component = %+v\nwant        %+v", got, tc.want)
			}
		})
	}
}

func TestAQuestionIsRenderedAsBodyTextAboveItsField(t *testing.T) {
	f := textField("f_name", "Nome")
	f.Question = "Como podemos te chamar?"
	_, out := compileOK(t, oneScreen(fieldBlock(f)), nil)
	children := out.Screens[0].Layout.Children
	if children[0]["type"] != "TextBody" || children[0]["text"] != "Como podemos te chamar?" || children[1]["type"] != "TextInput" {
		t.Fatalf("children = %+v", children)
	}
}

func TestDisplayBlocksCompileToTheirTextComponents(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G'}
	doc := oneScreen(
		displayBlock("b_h", DisplayHeading, "Bem-vindo"),
		displayBlock("b_s", DisplaySubheading, "Sub"),
		displayBlock("b_p", DisplayParagraph, "Texto"),
		displayBlock("b_c", DisplayCaption, "Legenda"),
		Block{Key: "b_i", Display: &Display{Kind: DisplayImage, MediaID: "m1", Text: "Logo"}},
		Block{Key: "b_l", Display: &Display{Kind: DisplayLink, Text: "Termos", URL: "https://example.com"}},
		fieldBlock(textField("f_name", "Nome")),
	)
	_, out := compileOK(t, doc, Assets{"m1": image})
	children := out.Screens[0].Layout.Children
	want := []map[string]any{
		{"type": "TextHeading", "text": "Bem-vindo"},
		{"type": "TextSubheading", "text": "Sub"},
		{"type": "TextBody", "text": "Texto"},
		{"type": "TextCaption", "text": "Legenda"},
		{"type": "Image", "src": base64.StdEncoding.EncodeToString(image), "alt-text": "Logo", "scale-type": "contain"},
		{"type": "EmbeddedLink", "text": "Termos", "on-click-action": map[string]any{"name": "open_url", "url": "https://example.com"}},
	}
	for i, w := range want {
		if !reflect.DeepEqual(children[i], w) {
			t.Fatalf("child %d = %+v, want %+v", i, children[i], w)
		}
	}
}

func TestImagesComeFromAssetsAndRespectMetasSize(t *testing.T) {
	doc := oneScreen(fieldBlock(textField("f_name", "Nome")), Block{Key: "b_i", Display: &Display{Kind: DisplayImage, MediaID: "m1"}})
	if _, err := Compile(doc, nil); !errors.Is(err, ErrAssetMissing) {
		t.Fatalf("a missing image must fail the compile, got %v", err)
	}
	_, err := Compile(doc, Assets{"m1": bytes.Repeat([]byte{1}, MaxImageBytes+1)})
	var problems Problems
	if !errors.As(err, &problems) || !hasProblem(problems, "b_i", "mediaId", ProblemTooLong) {
		t.Fatalf("an image above Meta's size must be a builder problem, got %v", err)
	}
}

func TestBranchingCompilesToASwitchWithOneFooterPerOption(t *testing.T) {
	doc := Document{
		Schema:     DocumentSchema,
		Categories: []Category{CategorySurvey},
		Screens: []Screen{
			{Key: "s_q", Title: "Pergunta", Blocks: []Block{fieldBlock(choiceField("f_c", FieldRadio, "o_yes", "o_no"))},
				Route: Route{Next: "s_b", BranchOn: "f_c", Branches: []Branch{{OptionID: "o_yes", Next: "s_a"}}}, SubmitLabel: "Continuar"},
			{Key: "s_a", Title: "Sim", Blocks: []Block{fieldBlock(textField("f_why", "Por quê"))}, SubmitLabel: "Enviar"},
			{Key: "s_b", Title: "Não", Blocks: []Block{fieldBlock(textField("f_other", "Outro"))}, SubmitLabel: "Enviar"},
		},
	}
	_, out := compileOK(t, doc, nil)
	sw := component(t, out.Screens[0].Layout.Children, "Switch")
	if sw["value"] != "${form.f_c}" {
		t.Fatalf("switch value = %v", sw["value"])
	}
	cases := sw["cases"].(map[string]any)
	next := func(option string) any {
		footer := cases[option].([]any)[0].(map[string]any)
		return footer["on-click-action"].(map[string]any)["next"].(map[string]any)["name"]
	}
	if next("o_yes") != "SCREEN_B" || next("o_no") != "SCREEN_C" {
		t.Fatalf("cases = %+v", cases)
	}
	last := action(t, component(t, out.Screens[2].Layout.Children, "Footer"))
	payload := last["payload"].(map[string]any)
	if payload["f_c"] != "${screen.SCREEN_A.form.f_c}" || payload["f_why"] != "${screen.SCREEN_B.form.f_why}" || payload["f_other"] != "${form.f_other}" {
		t.Fatalf("a terminal screen collects every answer of the form, got %+v", payload)
	}
	if !out.Screens[1].Terminal || !out.Screens[2].Terminal {
		t.Fatal("both branch ends are terminal")
	}
}

func TestVisibilityCompilesToAnExpression(t *testing.T) {
	cases := []struct {
		name string
		doc  Document
		want string
	}{
		{"same screen choice", oneScreen(fieldBlock(choiceField("f_c", FieldRadio, "o_a", "o_b")),
			Block{Key: "b_d", Field: textField("f_d", "Detalhe"), VisibleIf: &Condition{FieldKey: "f_c", Value: "o_a"}}),
			"`${form.f_c} == 'o_a'`"},
		{"negated choice", oneScreen(fieldBlock(choiceField("f_c", FieldRadio, "o_a", "o_b")),
			Block{Key: "b_d", Field: textField("f_d", "Detalhe"), VisibleIf: &Condition{FieldKey: "f_c", Value: "o_a", Negate: true}}),
			"`${form.f_c} != 'o_a'`"},
		{"opt-in checked", oneScreen(fieldBlock(&Field{Key: "f_ok", Kind: FieldOptIn, Label: "Sim"}),
			Block{Key: "b_d", Field: textField("f_d", "Detalhe"), VisibleIf: &Condition{FieldKey: "f_ok", Value: "true"}}),
			"${form.f_ok}"},
		{"opt-in unchecked", oneScreen(fieldBlock(&Field{Key: "f_ok", Kind: FieldOptIn, Label: "Sim"}),
			Block{Key: "b_d", Field: textField("f_d", "Detalhe"), VisibleIf: &Condition{FieldKey: "f_ok", Value: "false"}}),
			"`!${form.f_ok}`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, out := compileOK(t, tc.doc, nil)
			children := out.Screens[0].Layout.Children
			if got := children[1]["visible"]; got != tc.want {
				t.Fatalf("visible = %v, want %v", got, tc.want)
			}
		})
	}
	doc := twoScreens()
	doc.Screens[0].Blocks[0] = fieldBlock(choiceField("f_c", FieldRadio, "o_a", "o_b"))
	doc.Screens[1].Blocks[0].VisibleIf = &Condition{FieldKey: "f_c", Value: "o_b"}
	_, out := compileOK(t, doc, nil)
	if got := out.Screens[1].Layout.Children[0]["visible"]; got != "`${screen.SCREEN_A.form.f_c} == 'o_b'`" {
		t.Fatalf("a condition on an earlier screen uses a global reference, got %v", got)
	}
	question := choiceField("f_q", FieldRadio, "o_a", "o_b")
	detail := textField("f_d", "Detalhe")
	detail.Question = "Conte mais"
	_, out = compileOK(t, oneScreen(fieldBlock(question), Block{Key: "b_d", Field: detail, VisibleIf: &Condition{FieldKey: "f_q", Value: "o_a"}}), nil)
	children := out.Screens[0].Layout.Children
	if children[1]["visible"] != children[2]["visible"] || children[1]["type"] != "TextBody" {
		t.Fatalf("a hidden field hides its question too, got %+v", children)
	}
}

func TestSensitiveFieldsAreListedOnTheirScreen(t *testing.T) {
	secret := textField("f_s", "Senha")
	secret.Sensitive = true
	photo := &Field{Key: "f_ph", Kind: FieldPhoto, Label: "Foto"}
	_, out := compileOK(t, oneScreen(fieldBlock(textField("f_name", "Nome")), fieldBlock(secret), fieldBlock(photo)), nil)
	if !reflect.DeepEqual(out.Screens[0].Sensitive, []string{"f_s", "f_ph"}) {
		t.Fatalf("sensitive = %v", out.Screens[0].Sensitive)
	}
}

func TestCompileIsDeterministic(t *testing.T) {
	doc := twoScreens()
	a, err := Compile(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Compile(doc, nil)
	if !bytes.Equal(a.JSON, b.JSON) || a.Hash != b.Hash || len(a.Hash) != 64 {
		t.Fatalf("two compiles of one document differ or the hash is not sha256 hex: %s vs %s", a.Hash, b.Hash)
	}
	doc.Screens[0].Title = "Outro"
	c, _ := Compile(doc, nil)
	if c.Hash == a.Hash {
		t.Fatal("a change in the document changes the hash")
	}
}

func TestCompileRefusesAnInvalidDocument(t *testing.T) {
	_, err := Compile(Document{}, nil)
	if !errors.Is(err, ErrDocumentInvalid) {
		t.Fatalf("an invalid document never reaches Meta, got %v", err)
	}
}

func TestScreenIDsStayUniqueBeyondTheAlphabet(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 60; i++ {
		id := screenID(i)
		if seen[id] {
			t.Fatalf("screen id %s repeats at %d", id, i)
		}
		seen[id] = true
	}
	if screenID(0) != "SCREEN_A" || screenID(25) != "SCREEN_Z" || screenID(26) != "SCREEN_AA" {
		t.Fatalf("screen ids: %s %s %s", screenID(0), screenID(25), screenID(26))
	}
}

func TestSourceMapLocatesMetaPointersOnBlocks(t *testing.T) {
	f := textField("f_name", "Nome")
	f.Question = "Pergunta"
	compiled, _ := compileOK(t, oneScreen(displayBlock("b_h", DisplayHeading, "Olá"), fieldBlock(f)), nil)
	cases := []struct {
		path          string
		screen, block string
	}{
		{"screens[0].layout.children[0].text", "s_one", "b_h"},
		{"/screens/0/layout/children/1", "s_one", "b_f_name"},
		{"screens[0].layout.children[2].label", "s_one", "b_f_name"},
		{"screens[0].layout.children[3].label", "s_one", ""},
		{"screens[0].title", "s_one", ""},
		{"version", "", ""},
	}
	for _, tc := range cases {
		screen, block := compiled.SourceMap.Locate(tc.path)
		if screen != tc.screen || block != tc.block {
			t.Errorf("Locate(%q) = %q, %q; want %q, %q", tc.path, screen, block, tc.screen, tc.block)
		}
	}
}

func TestMetaValidationErrorsBecomeBuilderProblems(t *testing.T) {
	compiled, _ := compileOK(t, oneScreen(fieldBlock(textField("f_name", "Nome"))), nil)
	problems := compiled.SourceMap.Problems([]MetaValidationError{
		{Message: "Label too long", Paths: []string{"screens[0].layout.children[0].label"}},
		{Message: "Unknown", Paths: nil},
	})
	want := Problems{
		{Screen: "s_one", Block: "b_f_name", Code: ProblemMetaValidation, Message: "Label too long"},
		{Code: ProblemMetaValidation, Message: "Unknown"},
	}
	if !reflect.DeepEqual(problems, want) {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestAnOversizeImageProblemNamesItsScreen(t *testing.T) {
	doc := oneScreen(fieldBlock(textField("f_name", "Nome")), Block{Key: "b_i", Display: &Display{Kind: DisplayImage, MediaID: "m1"}})
	_, err := Compile(doc, Assets{"m1": bytes.Repeat([]byte{1}, MaxImageBytes+1)})
	var problems Problems
	if !errors.As(err, &problems) || problems[0].Screen != "s_one" {
		t.Fatalf("problems = %+v", problems)
	}
}
