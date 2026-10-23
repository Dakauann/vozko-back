package emaildesign

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"
)

func sequentialIDs() IDSource {
	n := 0
	return func(kind BlockKind, taken map[string]bool) string {
		for {
			n++
			id := string(kind) + "-" + strconv.Itoa(n)
			if !taken[id] {
				return id
			}
		}
	}
}

func editBase() Document {
	return docWith(
		headingBlock("title", "Oi"),
		textBlock("intro", Run{Text: "Texto"}),
		columnsBlock("cols", Ratio5050, []Block{headingBlock("left", "Esquerda")}, []Block{}),
	)
}

func op(t *testing.T, raw string) Operation {
	t.Helper()
	var o Operation
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func ids(blocks []Block) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.ID)
	}
	return out
}

func sameIDs(t *testing.T, got []Block, want ...string) {
	t.Helper()
	have := ids(got)
	if len(have) != len(want) {
		t.Fatalf("got %v, want %v", have, want)
	}
	for i := range want {
		if have[i] != want[i] {
			t.Fatalf("got %v, want %v", have, want)
		}
	}
}

func TestApplyRunsABatchStepByStep(t *testing.T) {
	base := editBase()
	plan, err := Apply(base, []Operation{
		op(t, `{"op":"add","kind":"button","ref":"cta","fields":{"label":"Comprar","href":"https://loja.example.com"},"at":{"after":"intro"}}`),
		op(t, `{"op":"set","id":"@cta","fields":{"full_width":true}}`),
		op(t, `{"op":"add","kind":"text","fields":{"text":"Na coluna"},"at":{"into":"cols","column":1}}`),
		op(t, `{"op":"move","id":"title","at":{"before":"cols"}}`),
		op(t, `{"op":"settings","fields":{"preheader":"Só hoje"}}`),
	}, sequentialIDs())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 5 || plan.Refs["cta"] != "button-1" {
		t.Fatalf("got %d steps and refs %v", len(plan.Steps), plan.Refs)
	}
	final := plan.Final(base)
	sameIDs(t, final.Blocks, "intro", "button-1", "title", "cols", "footer")
	if !final.Blocks[1].Button.FullWidth || final.Settings.Preheader != "Só hoje" {
		t.Fatalf("got %+v", final)
	}
	sameIDs(t, final.Blocks[3].Columns.Columns[1].Blocks, "text-2")
	if plan.Steps[0].Focus != "button-1" || plan.Steps[4].Focus != "" || plan.Steps[4].Op != OpSettings {
		t.Fatalf("got focus %q and %q", plan.Steps[0].Focus, plan.Steps[4].Focus)
	}
	sameIDs(t, base.Blocks, "title", "intro", "cols", "footer")
	if len(base.Blocks[2].Columns.Columns[1].Blocks) != 0 {
		t.Fatal("the base document changed")
	}
}

func TestApplyKeepsTheFooterLastAndInPlace(t *testing.T) {
	base := editBase()
	plan, err := Apply(base, []Operation{
		op(t, `{"op":"add","kind":"spacer","at":{"after":"footer"}}`),
		op(t, `{"op":"add","kind":"divider"}`),
	}, sequentialIDs())
	if err != nil {
		t.Fatal(err)
	}
	sameIDs(t, plan.Final(base).Blocks, "title", "intro", "cols", "spacer-1", "divider-2", "footer")
	for name, raw := range map[string]string{
		"move":      `{"op":"move","id":"footer","at":{"before":"title"}}`,
		"remove":    `{"op":"remove","id":"footer"}`,
		"duplicate": `{"op":"duplicate","id":"footer"}`,
	} {
		t.Run(name, func(t *testing.T) {
			assertOperationError(t, base, []Operation{op(t, raw)}, 0, "footer_locked")
		})
	}
	assertOperationError(t, base, []Operation{op(t, `{"op":"add","kind":"footer","fields":{"company_name":"A","address":"B"}}`)}, 0, "footer_exists")
}

func TestApplyDuplicatesWithFreshIDs(t *testing.T) {
	base := editBase()
	plan, err := Apply(base, []Operation{op(t, `{"op":"duplicate","id":"cols","ref":"copy"}`)}, sequentialIDs())
	if err != nil {
		t.Fatal(err)
	}
	final := plan.Final(base)
	sameIDs(t, final.Blocks, "title", "intro", "cols", "columns-1", "footer")
	sameIDs(t, final.Blocks[3].Columns.Columns[0].Blocks, "heading-2")
	if plan.Refs["copy"] != "columns-1" || len(Validate(final)) != 0 {
		t.Fatalf("refs %v, issues %v", plan.Refs, Validate(final))
	}
}

func TestApplyRemovesAndMovesOutOfColumns(t *testing.T) {
	base := editBase()
	plan, err := Apply(base, []Operation{
		op(t, `{"op":"move","id":"left","at":{"after":"cols"}}`),
		op(t, `{"op":"remove","id":"intro"}`),
	}, sequentialIDs())
	if err != nil {
		t.Fatal(err)
	}
	final := plan.Final(base)
	sameIDs(t, final.Blocks, "title", "cols", "left", "footer")
	if len(final.Blocks[1].Columns.Columns[0].Blocks) != 0 || plan.Steps[1].Focus != "" {
		t.Fatalf("got %+v", final.Blocks[1].Columns)
	}
}

func assertOperationError(t *testing.T, base Document, ops []Operation, index int, code string) {
	t.Helper()
	_, err := Apply(base, ops, sequentialIDs())
	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.Index != index || opErr.Code != code {
		t.Fatalf("want operation %d %s, got %v", index, code, err)
	}
}

func TestApplyRefusesBadOperations(t *testing.T) {
	base := editBase()
	cases := map[string]struct {
		ops   []string
		index int
		code  string
	}{
		"unknown op":         {ops: []string{`{"op":"paint"}`}, code: "operation_unknown"},
		"missing block":      {ops: []string{`{"op":"set","id":"nope","fields":{"text":"x"}}`}, code: "block_not_found"},
		"missing id":         {ops: []string{`{"op":"remove"}`}, code: "id_required"},
		"unknown ref":        {ops: []string{`{"op":"remove","id":"@ghost"}`}, code: "ref_unknown"},
		"duplicate ref":      {ops: []string{`{"op":"add","kind":"spacer","ref":"a"}`, `{"op":"add","kind":"spacer","ref":"a"}`}, index: 1, code: "ref_duplicate"},
		"bad ref":            {ops: []string{`{"op":"add","kind":"spacer","ref":"Não"}`}, code: "ref_invalid"},
		"no kind":            {ops: []string{`{"op":"add"}`}, code: "kind_required"},
		"unknown kind":       {ops: []string{`{"op":"add","kind":"video"}`}, code: "kind_unknown"},
		"columns in columns": {ops: []string{`{"op":"add","kind":"columns","at":{"into":"cols","column":0}}`}, code: "not_allowed_in_columns"},
		"into non columns":   {ops: []string{`{"op":"add","kind":"spacer","at":{"into":"title","column":0}}`}, code: "position_invalid"},
		"column range":       {ops: []string{`{"op":"add","kind":"spacer","at":{"into":"cols","column":2}}`}, code: "column_invalid"},
		"two anchors":        {ops: []string{`{"op":"add","kind":"spacer","at":{"after":"title","before":"intro"}}`}, code: "position_invalid"},
		"empty set":          {ops: []string{`{"op":"set","id":"title"}`}, code: "fields_required"},
		"field outside kind": {ops: []string{`{"op":"set","id":"title","fields":{"href":"https://x.com"}}`}, code: "field_not_for_kind"},
		"move columns in":    {ops: []string{`{"op":"move","id":"cols","at":{"into":"cols","column":0}}`}, code: "not_allowed_in_columns"},
		"later failure":      {ops: []string{`{"op":"add","kind":"spacer"}`, `{"op":"remove","id":"ghost"}`}, index: 1, code: "block_not_found"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ops := make([]Operation, 0, len(tc.ops))
			for _, raw := range tc.ops {
				ops = append(ops, op(t, raw))
			}
			assertOperationError(t, base, ops, tc.index, tc.code)
		})
	}
}

func TestApplyStopsAtTheBlockLimit(t *testing.T) {
	blocks := make([]Block, 0, MaxBlocks-1)
	for i := 0; i < MaxBlocks-1; i++ {
		blocks = append(blocks, spacerBlock("s"+strconv.Itoa(i)))
	}
	base := docWith(blocks...)
	assertOperationError(t, base, []Operation{op(t, `{"op":"add","kind":"spacer"}`)}, 0, "too_many_blocks")
}

func TestRandomIDsFollowTheEditorShape(t *testing.T) {
	id := RandomIDs()(KindHeading, map[string]bool{})
	if len(id) != len("heading-")+6 || id[:8] != "heading-" || !blockIDPattern.MatchString(id) {
		t.Fatalf("got %q", id)
	}
}
