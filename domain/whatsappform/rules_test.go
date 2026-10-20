package whatsappform

import (
	"errors"
	"strings"
	"testing"
)

func TestAWellFormedDocumentHasNoProblems(t *testing.T) {
	for name, doc := range map[string]Document{
		"one screen":  oneScreen(fieldBlock(textField("f_name", "Nome"))),
		"two screens": twoScreens(),
	} {
		if problems := ValidateStructure(doc); len(problems) > 0 {
			t.Fatalf("%s: unexpected problems %+v", name, problems)
		}
	}
}

func TestStructureProblemsAreAnchoredWhereTheBuilderShowsThem(t *testing.T) {
	cases := []struct {
		name                  string
		mutate                func(*Document)
		block, property, code string
	}{
		{"no screens", func(d *Document) { d.Screens = nil }, "", "screens", ProblemRequired},
		{"no category", func(d *Document) { d.Categories = nil }, "", "categories", ProblemRequired},
		{"unknown category", func(d *Document) { d.Categories = []Category{"MARKETING"} }, "", "categories", ProblemInvalid},
		{"no input anywhere", func(d *Document) {
			d.Screens[0].Blocks = []Block{displayBlock("b_h", DisplayHeading, "Olá")}
		}, "", "blocks", ProblemNoInput},
		{"screen without title", func(d *Document) { d.Screens[0].Title = " " }, "", "title", ProblemRequired},
		{"submit label too long", func(d *Document) { d.Screens[0].SubmitLabel = strings.Repeat("x", MaxFooterLabel+1) }, "", "submitLabel", ProblemTooLong},
		{"label too long for a text input", func(d *Document) {
			d.Screens[0].Blocks[0].Field.Label = strings.Repeat("x", MaxTextInputLabel+1)
		}, "b_f_name", "label", ProblemTooLong},
		{"label missing", func(d *Document) { d.Screens[0].Blocks[0].Field.Label = "" }, "b_f_name", "label", ProblemRequired},
		{"unknown field kind", func(d *Document) { d.Screens[0].Blocks[0].Field.Kind = "slider" }, "b_f_name", "kind", ProblemInvalid},
		{"help too long", func(d *Document) { d.Screens[0].Blocks[0].Field.Help = strings.Repeat("x", MaxHelpLength+1) }, "b_f_name", "help", ProblemTooLong},
		{"pattern without help", func(d *Document) { d.Screens[0].Blocks[0].Field.Pattern = "^[0-9]{8}$" }, "b_f_name", "help", ProblemRequired},
		{"pattern that is not a regex", func(d *Document) {
			f := d.Screens[0].Blocks[0].Field
			f.Pattern, f.Help = "([", "8 dígitos"
		}, "b_f_name", "pattern", ProblemInvalid},
		{"min above max", func(d *Document) {
			f := d.Screens[0].Blocks[0].Field
			f.Min, f.Max = 10, 5
		}, "b_f_name", "min", ProblemInvalid},
		{"field key with spaces", func(d *Document) { d.Screens[0].Blocks[0].Field.Key = "my field" }, "b_f_name", "key", ProblemInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := oneScreen(fieldBlock(textField("f_name", "Nome")))
			tc.mutate(&doc)
			problems := ValidateStructure(doc)
			if !hasProblem(problems, tc.block, tc.property, tc.code) {
				t.Fatalf("want %s.%s %s, got %+v", tc.block, tc.property, tc.code, problems)
			}
		})
	}
}

func TestChoiceFieldsFollowMetasOptionBounds(t *testing.T) {
	cases := []struct {
		name  string
		field *Field
		code  string
	}{
		{"radio without options", choiceField("f_c", FieldRadio), ProblemTooFew},
		{"chips need two options", choiceField("f_c", FieldMultiChips, "o_a"), ProblemTooFew},
		{"radio with too many options", choiceField("f_c", FieldRadio, manyIDs(MaxChoiceGroupOptions+1)...), ProblemTooMany},
		{"dropdown takes up to two hundred", choiceField("f_c", FieldDropdown, manyIDs(MaxDropdownOptions+1)...), ProblemTooMany},
		{"duplicated option id", choiceField("f_c", FieldRadio, "o_a", "o_a"), ProblemDuplicate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := ValidateStructure(oneScreen(fieldBlock(tc.field)))
			if !hasProblem(problems, "b_f_c", "options", tc.code) {
				t.Fatalf("want options %s, got %+v", tc.code, problems)
			}
		})
	}
	long := choiceField("f_c", FieldRadio, "o_a")
	long.Options[0].Title = strings.Repeat("x", MaxOptionTitle+1)
	if !hasProblem(ValidateStructure(oneScreen(fieldBlock(long))), "b_f_c", "options.title", ProblemTooLong) {
		t.Fatal("an option title above Meta's limit must be refused")
	}
	textWithOptions := textField("f_c", "Nome")
	textWithOptions.Options = []Option{{ID: "o_a", Title: "A"}}
	if !hasProblem(ValidateStructure(oneScreen(fieldBlock(textWithOptions))), "b_f_c", "options", ProblemNotAllowed) {
		t.Fatal("a text field cannot carry options")
	}
}

func manyIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "o_" + strings.Repeat("a", i+1)
	}
	return ids
}

func TestPerScreenComponentLimits(t *testing.T) {
	photo := &Field{Key: "f_p", Kind: FieldPhoto, Label: "Foto"}
	doc := &Field{Key: "f_d", Kind: FieldDocument, Label: "Documento"}
	problems := ValidateStructure(oneScreen(fieldBlock(photo), fieldBlock(doc)))
	if !hasProblem(problems, "b_f_d", "kind", ProblemTooMany) {
		t.Fatalf("a photo and a document on the same screen must be refused, got %+v", problems)
	}
	var optIns []Block
	for i := 0; i <= MaxOptInsPerScreen; i++ {
		key := "f_o" + strings.Repeat("x", i)
		optIns = append(optIns, fieldBlock(&Field{Key: key, Kind: FieldOptIn, Label: "Aceito"}))
	}
	if !hasProblem(ValidateStructure(oneScreen(optIns...)), optIns[MaxOptInsPerScreen].Key, "kind", ProblemTooMany) {
		t.Fatal("more opt-ins than Meta allows on a screen must be refused")
	}
	var links []Block
	links = append(links, fieldBlock(textField("f_name", "Nome")))
	for i := 0; i <= MaxLinksPerScreen; i++ {
		links = append(links, Block{Key: "b_l" + strings.Repeat("x", i), Display: &Display{Kind: DisplayLink, Text: "Saiba mais", URL: "https://example.com"}})
	}
	if !hasProblem(ValidateStructure(oneScreen(links...)), links[len(links)-1].Key, "kind", ProblemTooMany) {
		t.Fatal("more links than Meta allows on a screen must be refused")
	}
}

func TestComponentCountIncludesTheQuestionTextOfEachField(t *testing.T) {
	var blocks []Block
	for i := 0; i < MaxComponentsPerScreen/2+1; i++ {
		f := textField("f_"+strings.Repeat("a", i+1), "Campo")
		f.Question = "Pergunta"
		blocks = append(blocks, fieldBlock(f))
	}
	if !hasProblem(ValidateStructure(oneScreen(blocks...)), "", "blocks", ProblemTooMany) {
		t.Fatal("a field with a question renders two components, and the screen total must respect Meta's limit")
	}
}

func TestMediaFields(t *testing.T) {
	photo := &Field{Key: "f_p", Kind: FieldPhoto, Label: "Foto", Max: MaxUploads + 1}
	if !hasProblem(ValidateStructure(oneScreen(fieldBlock(photo))), "b_f_p", "max", ProblemTooMany) {
		t.Fatal("more uploads than Meta allows must be refused")
	}
	doc := &Field{Key: "f_d", Kind: FieldDocument, Label: "Documento", MimeTypes: []string{"application/x-msdownload"}}
	if !hasProblem(ValidateStructure(oneScreen(fieldBlock(doc))), "b_f_d", "mimeTypes", ProblemNotAllowed) {
		t.Fatal("a MIME type outside Meta's list must be refused")
	}
}

func TestDisplayBlocks(t *testing.T) {
	cases := []struct {
		name     string
		block    Block
		property string
		code     string
	}{
		{"empty heading", displayBlock("b_h", DisplayHeading, " "), "text", ProblemRequired},
		{"long heading", displayBlock("b_h", DisplayHeading, strings.Repeat("x", MaxHeadingLength+1)), "text", ProblemTooLong},
		{"image without media", Block{Key: "b_h", Display: &Display{Kind: DisplayImage}}, "mediaId", ProblemRequired},
		{"link without https url", Block{Key: "b_h", Display: &Display{Kind: DisplayLink, Text: "Termos", URL: "http://x.com"}}, "url", ProblemInvalid},
		{"unknown display kind", Block{Key: "b_h", Display: &Display{Kind: "video", Text: "x"}}, "kind", ProblemInvalid},
		{"block that is both display and field", Block{Key: "b_h", Display: &Display{Kind: DisplayHeading, Text: "x"}, Field: textField("f_x", "X")}, "", ProblemInvalid},
		{"empty block", Block{Key: "b_h"}, "", ProblemInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := oneScreen(fieldBlock(textField("f_name", "Nome")), tc.block)
			if !hasProblem(ValidateStructure(doc), "b_h", tc.property, tc.code) {
				t.Fatalf("want b_h.%s %s, got %+v", tc.property, tc.code, ValidateStructure(doc))
			}
		})
	}
}

func TestKeysAreUniqueAcrossTheWholeDocument(t *testing.T) {
	doc := twoScreens()
	doc.Screens[1].Blocks[0].Field.Key = "f_name"
	if !hasProblem(ValidateStructure(doc), "b_f_topic", "key", ProblemDuplicate) {
		t.Fatalf("a field key used twice must be refused: %+v", ValidateStructure(doc))
	}
	doc = twoScreens()
	doc.Screens[1].Key = "s_one"
	if !hasProblem(ValidateStructure(doc), "", "key", ProblemDuplicate) {
		t.Fatal("a screen key used twice must be refused")
	}
	doc = twoScreens()
	doc.Screens[1].Blocks[0].Key = "b_f_name"
	if !hasProblem(ValidateStructure(doc), "b_f_name", "key", ProblemDuplicate) {
		t.Fatal("a block key used twice must be refused")
	}
}

func TestRoutesOnlyMoveForwardAndEveryScreenIsReached(t *testing.T) {
	doc := twoScreens()
	doc.Screens[0].Route.Next = "s_missing"
	if !hasProblem(ValidateStructure(doc), "", "route.next", ProblemUnknownTarget) {
		t.Fatal("a route to a screen that does not exist must be refused")
	}
	doc = twoScreens()
	doc.Screens[1].Route.Next = "s_one"
	if !hasProblem(ValidateStructure(doc), "", "route.next", ProblemBackwardRoute) {
		t.Fatal("a route back to an earlier screen must be refused")
	}
	doc = twoScreens()
	doc.Screens[0].Route = Route{}
	problems := ValidateStructure(doc)
	if !hasProblem(problems, "", "key", ProblemUnreachable) {
		t.Fatalf("a screen no route leads to must be refused, got %+v", problems)
	}
}

func TestBranchingOnAnAnswer(t *testing.T) {
	branching := func() Document {
		return Document{
			Schema:     DocumentSchema,
			Categories: []Category{CategorySurvey},
			Screens: []Screen{
				{Key: "s_q", Title: "Pergunta", Blocks: []Block{fieldBlock(choiceField("f_c", FieldRadio, "o_yes", "o_no"))},
					Route: Route{Next: "s_b", BranchOn: "f_c", Branches: []Branch{{OptionID: "o_yes", Next: "s_a"}}}, SubmitLabel: "Continuar"},
				{Key: "s_a", Title: "Sim", Blocks: []Block{fieldBlock(textField("f_why", "Por quê"))}, SubmitLabel: "Enviar"},
				{Key: "s_b", Title: "Não", Blocks: []Block{fieldBlock(textField("f_other", "Outro"))}, SubmitLabel: "Enviar"},
			},
		}
	}
	if problems := ValidateStructure(branching()); len(problems) > 0 {
		t.Fatalf("a valid branch must pass, got %+v", problems)
	}
	cases := []struct {
		name     string
		mutate   func(*Document)
		property string
		code     string
	}{
		{"branch on a field of another screen", func(d *Document) { d.Screens[0].Route.BranchOn = "f_why" }, "route.branchOn", ProblemInvalid},
		{"branch on a text field", func(d *Document) {
			d.Screens[0].Blocks = append(d.Screens[0].Blocks, fieldBlock(textField("f_t", "Texto")))
			d.Screens[0].Route.BranchOn = "f_t"
		}, "route.branchOn", ProblemInvalid},
		{"branch on an optional field", func(d *Document) { d.Screens[0].Blocks[0].Field.Required = false }, "route.branchOn", ProblemRequired},
		{"branch on an option that does not exist", func(d *Document) { d.Screens[0].Route.Branches[0].OptionID = "o_maybe" }, "route.branches", ProblemUnknownTarget},
		{"branches without a field", func(d *Document) { d.Screens[0].Route.BranchOn = "" }, "route.branchOn", ProblemRequired},
		{"branch without a default", func(d *Document) { d.Screens[0].Route.Next = "" }, "route.next", ProblemRequired},
		{"two branches for one option", func(d *Document) {
			d.Screens[0].Route.Branches = append(d.Screens[0].Route.Branches, Branch{OptionID: "o_yes", Next: "s_b"})
		}, "route.branches", ProblemDuplicate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := branching()
			tc.mutate(&doc)
			if !hasProblem(ValidateStructure(doc), "", tc.property, tc.code) {
				t.Fatalf("want %s %s, got %+v", tc.property, tc.code, ValidateStructure(doc))
			}
		})
	}
}

func TestVisibilityConditions(t *testing.T) {
	visible := func(cond *Condition) Document {
		other := textField("f_detail", "Detalhe")
		return oneScreen(fieldBlock(choiceField("f_c", FieldRadio, "o_a", "o_b")), Block{Key: "b_detail", Field: other, VisibleIf: cond})
	}
	if problems := ValidateStructure(visible(&Condition{FieldKey: "f_c", Value: "o_a"})); len(problems) > 0 {
		t.Fatalf("a condition on an earlier choice must pass, got %+v", problems)
	}
	cases := []struct {
		name string
		cond *Condition
		code string
	}{
		{"unknown field", &Condition{FieldKey: "f_nope", Value: "o_a"}, ProblemUnknownTarget},
		{"unknown option", &Condition{FieldKey: "f_c", Value: "o_z"}, ProblemUnknownTarget},
		{"later field", &Condition{FieldKey: "f_detail", Value: "x"}, ProblemInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !hasProblem(ValidateStructure(visible(tc.cond)), "b_detail", "visibleIf", tc.code) {
				t.Fatalf("want visibleIf %s, got %+v", tc.code, ValidateStructure(visible(tc.cond)))
			}
		})
	}
	optIn := oneScreen(fieldBlock(&Field{Key: "f_ok", Kind: FieldOptIn, Label: "Quero receber"}),
		Block{Key: "b_detail", Field: textField("f_detail", "Detalhe"), VisibleIf: &Condition{FieldKey: "f_ok", Value: "maybe"}})
	if !hasProblem(ValidateStructure(optIn), "b_detail", "visibleIf", ProblemInvalid) {
		t.Fatal("an opt-in condition must compare with true or false")
	}
	text := oneScreen(fieldBlock(textField("f_t", "Texto")),
		Block{Key: "b_detail", Field: textField("f_detail", "Detalhe"), VisibleIf: &Condition{FieldKey: "f_t", Value: "x"}})
	if !hasProblem(ValidateStructure(text), "b_detail", "visibleIf", ProblemInvalid) {
		t.Fatal("conditions read choices and opt-ins only")
	}
}

func TestProblemsAreAnErrorThatUnwrapsToInvalidDocument(t *testing.T) {
	err := ValidateStructure(Document{}).Err()
	if !errors.Is(err, ErrDocumentInvalid) || ErrorCode(err) != "whatsapp_form_invalid" || !IsInputRefusal(err) {
		t.Fatalf("problems must surface as the invalid document input refusal, got %v", err)
	}
	if (Problems{}).Err() != nil {
		t.Fatal("no problems is no error")
	}
}
