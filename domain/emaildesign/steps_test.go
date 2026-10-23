package emaildesign

import (
	"encoding/json"
	"testing"
)

func TestEncodeStepsSendsOnlyWhatEachStepChanged(t *testing.T) {
	base := editBase()
	plan, err := Apply(base, []Operation{
		op(t, `{"op":"set","id":"title","fields":{"text":"Novo"}}`),
		op(t, `{"op":"add","kind":"spacer","at":{"into":"cols","column":1}}`),
		op(t, `{"op":"move","id":"intro","at":{"before":"title"}}`),
		op(t, `{"op":"settings","fields":{"preheader":"Oi"}}`),
	}, sequentialIDs())
	if err != nil {
		t.Fatal(err)
	}
	patches := EncodeSteps(base, plan.Steps)
	if len(patches) != 4 {
		t.Fatalf("got %d patches", len(patches))
	}
	first := patches[0]
	if first.Op != OpSet || first.Focus != "title" || first.Settings != nil || first.Order != nil || len(first.Blocks) != 1 || first.Blocks["title"].Heading.Text != "Novo" {
		t.Fatalf("got %+v", first)
	}
	second := patches[1]
	if second.Order != nil || len(second.Blocks) != 1 || len(second.Blocks["cols"].Columns.Columns[1].Blocks) != 1 {
		t.Fatalf("got %+v", second)
	}
	third := patches[2]
	if len(third.Blocks) != 0 || third.Order == nil || len(*third.Order) != 4 || (*third.Order)[0] != "intro" {
		t.Fatalf("got %+v", third)
	}
	fourth := patches[3]
	if fourth.Settings == nil || fourth.Settings.Preheader != "Oi" || fourth.Order != nil || len(fourth.Blocks) != 0 {
		t.Fatalf("got %+v", fourth)
	}
	raw, err := json.Marshal(fourth)
	if err != nil || string(raw) != `{"op":"settings","settings":{"preheader":"Oi"}}` {
		t.Fatalf("got %s, %v", raw, err)
	}
}

func TestEncodeStepsMarksClearedSettings(t *testing.T) {
	base := editBase()
	base.Settings.Preheader = "Oi"
	plan, err := Apply(base, []Operation{op(t, `{"op":"settings","fields":{"preheader":""}}`)}, sequentialIDs())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(EncodeSteps(base, plan.Steps)[0])
	if err != nil || string(raw) != `{"op":"settings","settings":{}}` {
		t.Fatalf("got %s, %v", raw, err)
	}
}
