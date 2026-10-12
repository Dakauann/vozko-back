package studiotools

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/skills"
)

var studioKinds = []copilot.StudioKind{copilot.StudioImage, copilot.StudioVideo}

func studioViewOf(kind copilot.StudioKind) copilot.View {
	return copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: kind}
}

func opGuideText(t *testing.T, kind copilot.StudioKind, names ...interface{}) string {
	t.Helper()
	res := NewStudioOpGuideTool().Execute(context.Background(), copilot.Context{View: studioViewOf(kind)}, map[string]interface{}{"names": names})
	if res.Status != copilot.StatusOK {
		t.Fatalf("%s guide for %v refused: %s", kind, names, res.Message)
	}
	return res.Data.(map[string]interface{})["guide"].(string)
}

func everyFamilyName(kind copilot.StudioKind) []interface{} {
	var names []interface{}
	for _, family := range studioFamiliesOf(kind) {
		names = append(names, family.name)
	}
	return names
}

func TestEveryEditOperationBelongsToExactlyOneFamily(t *testing.T) {
	for _, kind := range studioKinds {
		edit := studioEditFor(kind)
		seen := map[string]string{}
		for _, family := range edit.families {
			for _, op := range family.ops {
				if previous, dup := seen[op]; dup {
					t.Errorf("%s: %s sits in %s and %s", kind, op, previous, family.name)
				}
				seen[op] = family.name
				if !slices.Contains(edit.allowed, op) {
					t.Errorf("%s: family %s lists %s, which the editor does not have", kind, family.name, op)
				}
			}
		}
		for _, op := range edit.allowed {
			if _, ok := seen[op]; !ok {
				t.Errorf("%s: %s belongs to no family, so the guide cannot explain it", kind, op)
			}
		}
	}
}

func TestEveryOperationFieldIsDocumentedBySomeFamily(t *testing.T) {
	for _, kind := range studioKinds {
		edit := studioEditFor(kind)
		covered := map[string]bool{}
		for _, family := range edit.families {
			for _, name := range family.fields(edit.operationOrder()) {
				covered[name] = true
			}
		}
		for _, name := range edit.operationOrder() {
			if !covered[name] {
				t.Errorf("%s: no family guide mentions %s, so the model cannot learn it", kind, name)
			}
		}
	}
}

func shapeOf(params map[string]tools.Parameter) map[string]string {
	out := map[string]string{}
	var walk func(prefix string, params map[string]tools.Parameter)
	walk = func(prefix string, params map[string]tools.Parameter) {
		for name, p := range params {
			out[prefix+name] = p.Type
			if p.Items != nil {
				out[prefix+name+"[]"] = p.Items.Type
				walk(prefix+name+".", p.Items.Properties)
			}
		}
	}
	walk("", params)
	return out
}

func TestTheCompactEditSchemaKeepsEveryFieldNameAndTypeAndTheOperationEnum(t *testing.T) {
	for _, kind := range studioKinds {
		edit := studioEditFor(kind)
		reference := edit.Definition().Parameters["operations"].Items
		sent := copilot.DefinitionOn(edit, studioViewOf(kind)).Parameters["operations"].Items
		if got, want := shapeOf(sent.Properties), shapeOf(reference.Properties); !mapsEqual(got, want) {
			t.Fatalf("%s: the compact schema lost or changed fields", kind)
		}
		if !slices.Equal(sent.Properties["op"].Enum, edit.allowed) || !slices.Equal(sent.Required, reference.Required) {
			t.Fatalf("%s: op enum %v required %v", kind, sent.Properties["op"].Enum, sent.Required)
		}
		for name, p := range sent.Properties {
			if !slices.Contains(edit.core, name) && (p.Description != "" || len(p.Enum) > 0) {
				t.Errorf("%s: %s still carries its documentation in every call", kind, name)
			}
		}
		for _, name := range edit.core {
			if sent.Properties[name].Description != reference.Properties[name].Description {
				t.Errorf("%s: the core field %s lost its description", kind, name)
			}
		}
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestTheCompactEditToolNamesEveryFamilyAndOperationAndPointsToTheGuide(t *testing.T) {
	for _, kind := range studioKinds {
		edit := studioEditFor(kind)
		description := copilot.DefinitionOn(edit, studioViewOf(kind)).Description
		if !strings.Contains(description, "studio_op_guide") {
			t.Fatalf("%s: the edit tool never says where the field documentation lives", kind)
		}
		for _, family := range edit.families {
			if !strings.Contains(description, family.name+": "+strings.Join(family.ops, ", ")) {
				t.Errorf("%s: the index misses family %s", kind, family.name)
			}
		}
	}
}

func TestTheCompactEditDefinitionIsStableBetweenCalls(t *testing.T) {
	for _, kind := range studioKinds {
		first, _ := json.Marshal(copilot.DefinitionOn(studioEditFor(kind), studioViewOf(kind)))
		second, _ := json.Marshal(copilot.DefinitionOn(studioEditFor(kind), studioViewOf(kind)))
		if string(first) != string(second) {
			t.Fatalf("%s: the compact definition changes between builds and would break the prompt cache", kind)
		}
	}
}

func valuesListedIn(description string, values []string) bool {
	for _, value := range values {
		if !strings.Contains(description, value) {
			return false
		}
	}
	return true
}

func TestTheOpGuideServesEveryFieldWithItsDescriptionAndValues(t *testing.T) {
	for _, kind := range studioKinds {
		guide := opGuideText(t, kind, everyFamilyName(kind)...)
		core := studioEditFor(kind).core
		var walk func(owner string, params map[string]tools.Parameter)
		walk = func(owner string, params map[string]tools.Parameter) {
			for name, p := range params {
				if owner == "" && slices.Contains(core, name) {
					if strings.Contains(guide, "\n- "+name+" (") {
						t.Errorf("%s: the guide repeats %s, which every call already documents", kind, name)
					}
					continue
				}
				if !strings.Contains(guide, "- "+name+" (") {
					t.Errorf("%s: the guide never documents %s%s", kind, owner, name)
				}
				if p.Description != "" && !strings.Contains(guide, p.Description) {
					t.Errorf("%s: the guide drops the description of %s%s", kind, owner, name)
				}
				if len(p.Enum) > 0 && !valuesListedIn(guide, p.Enum) {
					t.Errorf("%s: the guide drops the values of %s%s", kind, owner, name)
				}
				if p.Items != nil {
					if len(p.Items.Enum) > 0 && !valuesListedIn(guide, p.Items.Enum) {
						t.Errorf("%s: the guide drops the item values of %s%s", kind, owner, name)
					}
					walk(owner+name+".", p.Items.Properties)
				}
			}
		}
		walk("", studioEditFor(kind).Definition().Parameters["operations"].Items.Properties)
		for _, family := range studioFamiliesOf(kind) {
			if !strings.Contains(guide, strings.TrimSpace(family.guide)) {
				t.Errorf("%s: the guide drops the explanation of %s", kind, family.name)
			}
		}
	}
}

func TestTheOpGuideAnswersAnOperationWithItsFamily(t *testing.T) {
	guide := opGuideText(t, copilot.StudioVideo, "set_line")
	if !strings.Contains(guide, "traco") || !strings.Contains(guide, "- wave_amount (") || strings.Contains(guide, "- chart_kind (") {
		t.Fatalf("set_line should bring the stroke family and only it:\n%s", guide)
	}
	both := opGuideText(t, copilot.StudioImage, "pranchetas", "add_artboard", "texto")
	if strings.Count(both, "## pranchetas") != 1 || !strings.Contains(both, "## texto") {
		t.Fatalf("a family asked twice must come once:\n%s", both)
	}
}

func TestTheOpGuideFollowsTheOpenProjectKind(t *testing.T) {
	if guide := opGuideText(t, copilot.StudioImage, "camadas"); !strings.Contains(guide, "\n- direction (") || strings.Contains(guide, "\n- clip_ids (") {
		t.Fatalf("image guide = %s", guide)
	}
	res := NewStudioOpGuideTool().Execute(context.Background(), copilot.Context{View: studioViewOf(copilot.StudioImage)}, map[string]interface{}{"names": []interface{}{"set_line"}})
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "camadas") {
		t.Fatalf("a video operation in the image editor must be refused with the image families, got %+v", res)
	}
}

func TestTheOpGuideFailsClosedOnUnknownOrMissingNames(t *testing.T) {
	tool := NewStudioOpGuideTool()
	for name, args := range map[string]map[string]interface{}{
		"unknown": {"names": []interface{}{"nada"}},
		"empty":   {"names": []interface{}{}},
		"missing": {},
		"blank":   {"names": []interface{}{"  "}},
	} {
		res := tool.Execute(context.Background(), copilot.Context{View: studioViewOf(copilot.StudioVideo)}, args)
		if res.Status != copilot.StatusError {
			t.Errorf("%s names were accepted: %+v", name, res)
		}
	}
	if res := tool.Execute(context.Background(), copilot.Context{}, map[string]interface{}{"names": []interface{}{"texto"}}); res.Status != copilot.StatusError {
		t.Fatalf("outside a studio project the guide must refuse, got %+v", res)
	}
}

func TestTheOpGuideIsAStudioReadToolOnEveryProjectKind(t *testing.T) {
	tool := NewStudioOpGuideTool()
	if tool.Meta().Mutating || tool.Meta() != studioReadMeta() {
		t.Fatalf("meta = %+v", tool.Meta())
	}
	for _, kind := range studioKinds {
		if !studioViewOf(kind).Offers(tool) {
			t.Errorf("the guide is missing on the %s editor", kind)
		}
	}
	if (copilot.View{}).Offers(tool) || (copilot.View{Surface: copilot.SurfaceAttendance}).Offers(tool) {
		t.Fatal("the guide leaks outside the studio")
	}
	if !slices.ContainsFunc(StudioTools(StudioDeps{}), func(candidate copilot.Tool) bool {
		return candidate.Definition().Name == "studio_op_guide"
	}) {
		t.Fatal("the studio toolset does not offer the guide")
	}
}

var familyExample = regexp.MustCompile(`studio_op_guide[^.(]*\((?:por exemplo|famílias) ([a-z, -]+?)\)`)

func TestTheBaseStudioSkillsSayWhereTheOperationFieldsLive(t *testing.T) {
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	for name, kind := range map[string]copilot.StudioKind{"design-de-imagem": copilot.StudioImage, "edicao-de-video": copilot.StudioVideo, "motion-design": copilot.StudioVideo} {
		skill, ok := library.Find(name)
		if !ok || !strings.Contains(skill.Body, "studio_op_guide") {
			t.Fatalf("%s does not tell Elo to fetch the operation fields from studio_op_guide", name)
		}
		known := map[string]bool{}
		for _, family := range studioFamiliesOf(kind) {
			known[family.name] = true
		}
		matches := familyExample.FindAllStringSubmatch(skill.Body, -1)
		if len(matches) == 0 {
			t.Errorf("%s never says which families to ask studio_op_guide for", name)
		}
		for _, match := range matches {
			for _, word := range strings.FieldsFunc(match[1], func(r rune) bool { return r == ',' || r == ' ' }) {
				if word != "e" && !known[word] {
					t.Errorf("%s names %q as an operation family, which the %s guide does not have", name, word, kind)
				}
			}
		}
	}
}

func TestTheCompactSchemaKeepsEditValidationStrict(t *testing.T) {
	edit := NewStudioEditImageTool(StudioDeps{}).(copilot.Validator)
	for name, ops := range map[string][]interface{}{
		"unknown op":     {map[string]interface{}{"op": "explode"}},
		"too many":       make([]interface{}, MaxStudioOperations+1),
		"bad path op":    {map[string]interface{}{"op": "set_path_ops", "layer_id": "l1", "path_ops": []interface{}{map[string]interface{}{"kind": "melt"}}}},
		"nothing at all": {},
	} {
		if err := edit.Validate(context.Background(), copilot.Context{View: studioViewOf(copilot.StudioImage)}, map[string]interface{}{"operations": ops}); !errors.Is(err, copilottools.ErrInvalidArgs) {
			t.Errorf("%s was not refused as invalid: %v", name, err)
		}
	}
}

func TestTheOpGuideResolvesNearNamesToTheirFamilies(t *testing.T) {
	cases := []struct {
		kind copilot.StudioKind
		name string
		want string
	}{
		{copilot.StudioImage, "forma", "## formas:"},
		{copilot.StudioImage, "Formas", "## formas:"},
		{copilot.StudioImage, "formas vetoriais", "## formas:"},
		{copilot.StudioImage, "shapes", "\n- shape ("},
		{copilot.StudioVideo, "formas", "## criar:"},
		{copilot.StudioImage, "group", "## camadas:"},
		{copilot.StudioVideo, "group", "## grupos:"},
		{copilot.StudioImage, "fills", "\n- fills ("},
		{copilot.StudioImage, "fill", "\n- fill ("},
		{copilot.StudioImage, "effects", "\n- effects ("},
		{copilot.StudioImage, "ícones", "## criar:"},
		{copilot.StudioImage, "layers", "## camadas:"},
		{copilot.StudioImage, "artboards", "## pranchetas:"},
		{copilot.StudioImage, "prancheta", "## pranchetas:"},
		{copilot.StudioImage, "ADD_TEXT", "## criar:"},
		{copilot.StudioImage, "add-text", "## criar:"},
		{copilot.StudioImage, "table_row", "## tabelas:"},
		{copilot.StudioVideo, "efeito", "## efeitos:"},
		{copilot.StudioVideo, "vfx", "## efeitos:"},
		{copilot.StudioVideo, "Transições", "## transicoes:"},
		{copilot.StudioVideo, "transição", "## transicoes:"},
		{copilot.StudioVideo, "transitions", "## transicoes:"},
		{copilot.StudioVideo, "animação", "## animacao:"},
		{copilot.StudioVideo, "keyframes", "## chaves:"},
		{copilot.StudioVideo, "linha do tempo", "## linha-do-tempo:"},
		{copilot.StudioVideo, "timeline", "## linha-do-tempo:"},
	}
	for _, c := range cases {
		if guide := opGuideText(t, c.kind, c.name); !strings.Contains(guide, c.want) {
			t.Errorf("%s %q should bring %q, got:\n%.300s", c.kind, c.name, c.want, guide)
		}
	}
}

func TestTheOpGuideSaysWhichFamilyANearNameMeant(t *testing.T) {
	res := NewStudioOpGuideTool().Execute(context.Background(), copilot.Context{View: studioViewOf(copilot.StudioImage)}, map[string]interface{}{"names": []interface{}{"forma", "texto"}})
	if res.Status != copilot.StatusOK {
		t.Fatalf("refused: %s", res.Message)
	}
	resolved, ok := res.Data.(map[string]interface{})["resolved"].(map[string]string)
	if !ok || resolved["forma"] != "formas" || len(resolved) != 1 {
		t.Fatalf("resolved = %+v", res.Data.(map[string]interface{})["resolved"])
	}
	exact := NewStudioOpGuideTool().Execute(context.Background(), copilot.Context{View: studioViewOf(copilot.StudioImage)}, map[string]interface{}{"names": []interface{}{"texto", "style_text"}})
	if _, has := exact.Data.(map[string]interface{})["resolved"]; has {
		t.Fatalf("exact families and operations need no resolution note: %+v", exact.Data)
	}
}

func TestEveryOperationAndFieldResolvesToAFamilyThatDocumentsIt(t *testing.T) {
	for _, kind := range studioKinds {
		edit := studioEditFor(kind)
		ref := referenceOf(edit)
		for _, family := range edit.families {
			for _, op := range family.ops {
				if got, ok := ref.resolve(op); !ok || got != family.name {
					t.Errorf("%s: %s resolved to %q", kind, op, got)
				}
			}
		}
		for _, field := range edit.operationOrder() {
			if slices.Contains(edit.core, field) {
				continue
			}
			family, ok := ref.resolve(field)
			if !ok || !strings.Contains(ref.sections[family], "\n- "+field+" (") {
				t.Errorf("%s: the field %s resolved to %q, which does not document it", kind, field, family)
			}
		}
	}
}

func TestEveryFamilyAliasResolvesToItsOwnFamily(t *testing.T) {
	for _, kind := range studioKinds {
		ref := referenceOf(studioEditFor(kind))
		for _, family := range studioFamiliesOf(kind) {
			for _, alias := range studioFamilyAliases[kind][family.name] {
				if got, ok := ref.resolve(alias); !ok || got != family.name {
					t.Errorf("%s: the alias %q of %s resolved to %q", kind, alias, family.name, got)
				}
			}
		}
		for family := range studioFamilyAliases[kind] {
			if !slices.ContainsFunc(studioFamiliesOf(kind), func(f studioOpFamily) bool { return f.name == family }) {
				t.Errorf("%s: aliases for %s, which is no family of this editor", kind, family)
			}
		}
	}
}

func TestUnknownGuideNamesRefuseAndSuggestTheClosestFamily(t *testing.T) {
	cases := []struct {
		kind    copilot.StudioKind
		name    string
		closest string
	}{
		{copilot.StudioVideo, "trnsicoes", "transicoes"},
		{copilot.StudioImage, "pranchtas", "pranchetas"},
		{copilot.StudioImage, "componetes", "componentes"},
		{copilot.StudioImage, "add_layer", ""},
		{copilot.StudioVideo, "nada", ""},
	}
	for _, c := range cases {
		res := NewStudioOpGuideTool().Execute(context.Background(), copilot.Context{View: studioViewOf(c.kind)}, map[string]interface{}{"names": []interface{}{c.name}})
		if res.Status != copilot.StatusError || !strings.Contains(res.Message, "a família mais próxima é ") {
			t.Errorf("%s %q must refuse with a suggestion, got %+v", c.kind, c.name, res)
			continue
		}
		if c.closest != "" && !strings.Contains(res.Message, "a família mais próxima é "+c.closest+";") {
			t.Errorf("%s %q should suggest %s, got %s", c.kind, c.name, c.closest, res.Message)
		}
	}
}

func TestGuideNameResolutionIsDeterministic(t *testing.T) {
	for _, kind := range studioKinds {
		first, second := referenceOf(studioEditFor(kind)), referenceOf(studioEditFor(kind))
		for _, name := range []string{"forma", "fills", "effects", "estilos", "cores", "x", "linha do tempo", "zzz"} {
			a, aok := first.resolve(name)
			b, bok := second.resolve(name)
			if a != b || aok != bok || first.closest(name) != second.closest(name) {
				t.Errorf("%s %q resolves differently between builds: %q %q", kind, name, a, b)
			}
		}
	}
}

func TestTheEditToolsSayARefLivesInItsBatchAndLaterBatchesUseTheCreatedIDs(t *testing.T) {
	for _, kind := range studioKinds {
		edit := studioEditFor(kind)
		compact := copilot.DefinitionOn(edit, studioViewOf(kind))
		for _, def := range []tools.Definition{compact, edit.Definition()} {
			if !strings.Contains(def.Description, "Uma ref vale só no lote que a cria: created traz os ids de cada ref, e os lotes seguintes usam esses ids.") {
				t.Errorf("%s: the description does not say how refs cross batches", kind)
			}
			ref := def.Parameters["operations"].Items.Properties["ref"].Description
			if !strings.Contains(ref, "as operações seguintes do mesmo lote usam @nome") {
				t.Errorf("%s: the ref field does not say it lives in its batch: %s", kind, ref)
			}
		}
	}
}

func TestTheIconFieldAcceptsLibraryIconIDs(t *testing.T) {
	for _, kind := range studioKinds {
		guide := opGuideText(t, kind, "add_icon")
		line := guide[strings.Index(guide, "\n- icon_id ("):]
		line = line[:strings.Index(line[1:], "\n")+1]
		if !strings.Contains(line, "studio_library_search") || !strings.Contains(line, "add_element") {
			t.Errorf("%s: icon_id should take library icon ids and send other library items to add_element: %s", kind, line)
		}
	}
}
