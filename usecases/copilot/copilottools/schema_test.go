package copilottools

import (
	"errors"
	"reflect"
	"testing"
)

type schemaFake struct {
	Name   string   `json:"name" req:"true"`
	Opt    string   `json:"opt,omitempty"`
	Flag   *bool    `json:"flag"`
	Count  int      `json:"count"`
	List   []string `json:"list"`
	Hidden string   `json:"-"`
	NoTag  string
}

func TestStructParams(t *testing.T) {
	desc := map[string]string{"name": "the name", "flag": "a flag"}
	params, required := StructParams(reflect.TypeOf(schemaFake{}), desc)

	for _, k := range []string{"name", "opt", "flag", "count", "list"} {
		if _, ok := params[k]; !ok {
			t.Fatalf("missing param %q", k)
		}
	}
	if _, ok := params["Hidden"]; ok {
		t.Fatal(`json:"-" field must be skipped`)
	}
	if _, ok := params["NoTag"]; ok {
		t.Fatal("untagged field must be skipped")
	}
	if params["name"].Type != "string" || params["flag"].Type != "boolean" ||
		params["count"].Type != "integer" || params["list"].Type != "array" {
		t.Fatalf("param types wrong: %+v", params)
	}
	if params["name"].Description != "the name" || params["opt"].Description != "" {
		t.Fatalf("descriptions wrong: %+v", params)
	}
	if len(required) != 1 || required[0] != "name" {
		t.Fatalf("required wrong: %v", required)
	}
}

func TestBindArgs(t *testing.T) {
	var f schemaFake
	BindArgs(map[string]interface{}{"name": "x", "flag": true, "count": 3}, &f)
	if f.Name != "x" || f.Flag == nil || !*f.Flag || f.Count != 3 {
		t.Fatalf("bind wrong: %+v", f)
	}

	var g schemaFake
	BindArgs(map[string]interface{}{"name": make(chan int)}, &g)
	if g.Name != "" {
		t.Fatal("unmarshalable args must be a no-op")
	}
}

func TestBindArgsOnlyBindsSchemaFields(t *testing.T) {
	var f schemaFake
	BindArgs(map[string]interface{}{
		"name": "x", "NoTag": "leak", "Hidden": "leak",
		"workspaceId": "leak", "createdBy": "leak", "id": "leak",
	}, &f)
	if f.Name != "x" {
		t.Fatal("schema fields must bind")
	}
	if f.NoTag != "" || f.Hidden != "" {
		t.Fatalf("only fields exposed by the schema may bind, got %+v", f)
	}
}

type taggedArgs struct {
	Query    string   `json:"query" req:"true" desc:"o que procurar"`
	Status   string   `json:"status" enum:"open,closed" desc:"situação"`
	Channels []string `json:"channels" desc:"canais"`
	Page     int      `json:"page"`
}

func TestStructParamsReadsDescriptionsEnumsAndListItemsFromTags(t *testing.T) {
	params, required := StructParams(reflect.TypeOf(taggedArgs{}), nil)
	if params["query"].Description != "o que procurar" || len(required) != 1 || required[0] != "query" {
		t.Fatalf("query = %+v required = %v", params["query"], required)
	}
	if !reflect.DeepEqual(params["status"].Enum, []string{"open", "closed"}) {
		t.Fatalf("status enum = %v", params["status"].Enum)
	}
	if params["channels"].Items == nil || params["channels"].Items.Type != "string" {
		t.Fatalf("channels items = %+v", params["channels"].Items)
	}
}

func TestDescriptionMapStillWinsOverTheTag(t *testing.T) {
	params, _ := StructParams(reflect.TypeOf(taggedArgs{}), map[string]string{"query": "override"})
	if params["query"].Description != "override" {
		t.Fatalf("description = %q", params["query"].Description)
	}
}

func TestDecodeArgsRefusesMissingRequiredFields(t *testing.T) {
	var got taggedArgs
	if err := DecodeArgs(map[string]interface{}{"status": "open"}, &got); err == nil {
		t.Fatal("a missing required field must refuse, not run with an empty value")
	}
	if err := DecodeArgs(map[string]interface{}{"query": "   "}, &got); err == nil {
		t.Fatal("a blank required string is missing too")
	}
	if err := DecodeArgs(map[string]interface{}{"query": "maria", "page": 2.0, "unknown": "x"}, &got); err != nil || got.Query != "maria" || got.Page != 2 {
		t.Fatalf("decode = %+v, %v", got, err)
	}
}

func TestDecodeArgsRefusesAValueOutsideItsEnum(t *testing.T) {
	var got taggedArgs
	if err := DecodeArgs(map[string]interface{}{"query": "x", "status": "deleted"}, &got); err == nil {
		t.Fatal("an enum is a contract, not a suggestion")
	}
}

func TestDefinitionBuildsFromTheArgsStruct(t *testing.T) {
	def := Definition("search_x", "procura", taggedArgs{})
	if def.Name != "search_x" || def.Description != "procura" || len(def.Parameters) != 4 || def.Required[0] != "query" {
		t.Fatalf("definition = %+v", def)
	}
}

type sharedArgsFake struct {
	MediaID string `json:"media_id" req:"true"`
}

type embeddingArgsFake struct {
	sharedArgsFake
	Name string `json:"name" req:"true"`
}

func TestEmbeddedArgsArePromotedIntoTheSchemaAndDecoding(t *testing.T) {
	def := Definition("embedded", "", embeddingArgsFake{})
	if _, ok := def.Parameters["media_id"]; !ok || len(def.Required) != 2 {
		t.Fatalf("schema = %+v required %v", def.Parameters, def.Required)
	}
	var a embeddingArgsFake
	if err := DecodeArgs(map[string]interface{}{"name": "x"}, &a); err == nil {
		t.Fatal("missing embedded required field was accepted")
	}
	if err := DecodeArgs(map[string]interface{}{"name": "x", "media_id": "m1", "extra": "y"}, &a); err != nil || a.MediaID != "m1" {
		t.Fatalf("decoded %+v err %v", a, err)
	}
}

type idArgsFake struct {
	PhoneID  string   `json:"phone_id" req:"true" id:"true"`
	StageIDs []string `json:"stage_ids" id:"true"`
	Note     string   `json:"note"`
}

func TestDecodeArgsRefusesInventedIDs(t *testing.T) {
	for _, args := range []map[string]interface{}{
		{"phone_id": "0199f6d4-8761-7000-b3a1-example01"},
		{"phone_id": "CTA Vo"},
		{"phone_id": "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d", "stage_ids": []interface{}{"1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d", "stage-2"}},
	} {
		var a idArgsFake
		err := DecodeArgs(args, &a)
		if !errors.Is(err, ErrInvalidArgs) {
			t.Fatalf("%v accepted: %v", args, err)
		}
	}
	var a idArgsFake
	if err := DecodeArgs(map[string]interface{}{"phone_id": " 1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d ", "note": "x"}, &a); err != nil || a.PhoneID != "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d" {
		t.Fatalf("a real id was refused: %v", err)
	}
}

type schemaGradientFake struct {
	Kind string `json:"kind,omitempty" enum:"linear,radial" desc:"tipo"`
	From string `json:"from" req:"true" desc:"cor inicial"`
}

type schemaNestedFake struct {
	Gradient *schemaGradientFake `json:"gradient,omitempty" desc:"degradê"`
	Clear    []string            `json:"clear,omitempty" enum:"shadow,stroke" desc:"efeitos a remover"`
}

func TestAnObjectFieldDescribesItsFieldsAndAListEnumConstrainsItsItems(t *testing.T) {
	params, _ := StructParams(reflect.TypeOf(schemaNestedFake{}), nil)
	gradient := params["gradient"]
	if gradient.Type != "object" || gradient.Items == nil || gradient.Description != "degradê" {
		t.Fatalf("gradient %+v", gradient)
	}
	if kind := gradient.Items.Properties["kind"]; kind.Type != "string" || len(kind.Enum) != 2 || kind.Enum[1] != "radial" {
		t.Fatalf("gradient.kind %+v", kind)
	}
	if len(gradient.Items.Required) != 1 || gradient.Items.Required[0] != "from" {
		t.Fatalf("gradient required %v", gradient.Items.Required)
	}
	clear := params["clear"]
	if clear.Type != "array" || len(clear.Enum) != 0 || clear.Items == nil || len(clear.Items.Enum) != 2 || clear.Items.Enum[0] != "shadow" {
		t.Fatalf("an enum on a list must constrain its items, got %+v", clear)
	}
}

type schemaCellFake struct {
	Text string `json:"text" req:"true"`
}

type schemaGridFake struct {
	Rows     [][]string          `json:"rows"`
	Cube     [][][]int           `json:"cube"`
	Marks    [][]string          `json:"marks" enum:"a,b"`
	Cells    [][]schemaCellFake  `json:"cells"`
	Pointers []*schemaCellFake   `json:"pointers"`
	Deep     *[][]schemaCellFake `json:"deep"`
}

func TestListsOfListsDeclareTheirItemsAtAnyDepth(t *testing.T) {
	params, _ := StructParams(reflect.TypeOf(schemaGridFake{}), nil)
	rows := params["rows"].Items
	if rows == nil || rows.Type != "array" || rows.Items == nil || rows.Items.Type != "string" {
		t.Fatalf("rows %+v", rows)
	}
	cube := params["cube"].Items
	if cube == nil || cube.Items == nil || cube.Items.Items == nil || cube.Items.Items.Type != "integer" {
		t.Fatalf("cube %+v", cube)
	}
	marks := params["marks"]
	if len(marks.Enum) != 0 || marks.Items.Items == nil || len(marks.Items.Items.Enum) != 2 || len(marks.Items.Enum) != 0 {
		t.Fatalf("an enum on a list of lists must constrain the innermost items, got %+v", marks.Items)
	}
	for _, name := range []string{"cells", "deep"} {
		cells := params[name].Items
		if cells == nil || cells.Type != "array" || cells.Items == nil || cells.Items.Type != "object" || cells.Items.Properties["text"].Type != "string" || cells.Items.Required[0] != "text" {
			t.Fatalf("%s %+v", name, cells)
		}
	}
	pointers := params["pointers"].Items
	if pointers == nil || pointers.Type != "object" || pointers.Properties["text"].Type != "string" {
		t.Fatalf("pointers %+v", pointers)
	}
}
