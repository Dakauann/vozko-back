package studiotools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/shortlink"
	"vozko/domain/studio"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
)

type fakeLinkCreate struct {
	calls     []shortlink.CreateShortLinkInput
	err       error
	failAfter int
}

func (f *fakeLinkCreate) Execute(_ context.Context, input shortlink.CreateShortLinkInput) (*shortlink.ShortLink, error) {
	f.calls = append(f.calls, input)
	if f.err != nil && len(f.calls) > f.failAfter {
		return nil, f.err
	}
	return &shortlink.ShortLink{ID: "3f2c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e0f", Code: "aB3dE9", WorkspaceID: input.WorkspaceID, TargetURL: input.TargetURL}, nil
}

type fakeLinkGet struct{ known map[string]string }

func (f fakeLinkGet) Execute(_ context.Context, workspaceID, id string) (*shortlink.ShortLink, error) {
	code, ok := f.known[workspaceID+"/"+id]
	if !ok {
		return nil, shortlink.ErrShortLinkNotFound
	}
	return &shortlink.ShortLink{ID: id, Code: code, WorkspaceID: workspaceID}, nil
}

type fakeLinkDelete struct{ deleted []string }

func (f *fakeLinkDelete) Execute(_ context.Context, workspaceID, id string) error {
	f.deleted = append(f.deleted, workspaceID+"/"+id)
	return nil
}

type fakeAccess struct{ denied map[workspace.Action]bool }

func (f fakeAccess) Execute(_, _ string, resource workspace.Resource, action workspace.Action) error {
	if resource != workspace.ResourceShortLinks || f.denied[action] {
		return workspace.ErrInsufficientPermissions
	}
	return nil
}

func linkDeps(kind studio.Kind, create *fakeLinkCreate, access fakeAccess) StudioDeps {
	return linkDepsDeleting(kind, create, &fakeLinkDelete{}, access)
}

func linkDepsDeleting(kind studio.Kind, create *fakeLinkCreate, remove *fakeLinkDelete, access fakeAccess) StudioDeps {
	deps := studioDeps(kind, nil, nil)
	deps.Links = StudioShortLinks{Create: create, Get: fakeLinkGet{known: map[string]string{"ws-1/0d0c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e01": "promo"}}, Delete: remove, BaseURL: "https://vzk.to/", Permissions: access}
	return deps
}

func TestStudioEditRemovesTheShortLinkItCreatedWhenTheBatchIsRefused(t *testing.T) {
	qr := map[string]interface{}{"op": "add_qr_code", "qr_url": "https://loja.com/oferta"}
	args := map[string]interface{}{"operations": []interface{}{qr}}
	refusals := map[string]copilot.ScreenReply{
		"refused":   {OK: false, Error: &copilot.ScreenError{Code: "invalid", Message: "camada não existe"}},
		"no editor": {OK: false, Error: &copilot.ScreenError{Code: copilot.ScreenNoEditor, Message: "sem editor"}},
	}
	for name, reply := range refusals {
		remove := &fakeLinkDelete{}
		editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: reply}}
		res := NewStudioEditImageTool(linkDepsDeleting(studio.KindImage, &fakeLinkCreate{}, remove, fakeAccess{})).Execute(context.Background(), studioSession(copilot.StudioImage, editor), args)
		if res.Status != copilot.StatusError {
			t.Fatalf("%s: result = %+v", name, res)
		}
		if len(remove.deleted) != 1 || remove.deleted[0] != "ws-1/3f2c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e0f" {
			t.Fatalf("%s: the created link stayed behind: %+v", name, remove.deleted)
		}
	}
	remove := &fakeLinkDelete{}
	if res := NewStudioEditImageTool(linkDepsDeleting(studio.KindImage, &fakeLinkCreate{}, remove, fakeAccess{})).Execute(context.Background(), studioSession(copilot.StudioImage, okEditor()), args); res.Status != copilot.StatusOK || len(remove.deleted) != 0 {
		t.Fatalf("an applied batch lost its link: %+v %+v", res, remove.deleted)
	}
	existing := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_qr_code", "short_link_id": "0d0c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e01"}}}
	refused := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: refusals["refused"]}}
	if NewStudioEditImageTool(linkDepsDeleting(studio.KindImage, &fakeLinkCreate{}, remove, fakeAccess{})).Execute(context.Background(), studioSession(copilot.StudioImage, refused), existing); len(remove.deleted) != 0 {
		t.Fatalf("a link the batch did not create was removed: %+v", remove.deleted)
	}
}

func TestStudioEditRemovesEarlierShortLinksWhenALaterOneFails(t *testing.T) {
	create := &fakeLinkCreate{err: errors.New("database down"), failAfter: 1}
	remove := &fakeLinkDelete{}
	editor := okEditor()
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_qr_code", "qr_url": "https://loja.com/a"},
		map[string]interface{}{"op": "add_qr_code", "qr_url": "https://loja.com/b"},
	}}
	res := NewStudioEditImageTool(linkDepsDeleting(studio.KindImage, create, remove, fakeAccess{})).Execute(context.Background(), studioSession(copilot.StudioImage, editor), args)
	if res.Status != copilot.StatusError || len(create.calls) != 2 {
		t.Fatalf("result = %+v, calls = %d", res, len(create.calls))
	}
	if len(remove.deleted) != 1 {
		t.Fatalf("the first link stayed behind: %+v", remove.deleted)
	}
	if _, sent := editor.sent(copilot.ScreenEdit); sent {
		t.Fatal("the editor got a batch after a link failed")
	}
}

func TestStudioEditChecksEveryShortLinkBeforeCreatingAny(t *testing.T) {
	create := &fakeLinkCreate{}
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_qr_code", "qr_url": "https://loja.com/a"},
		map[string]interface{}{"op": "add_qr_code", "short_link_id": "9d0c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e09"},
	}}
	res := NewStudioEditImageTool(linkDeps(studio.KindImage, create, fakeAccess{})).Execute(context.Background(), studioSession(copilot.StudioImage, okEditor()), args)
	if res.Status != copilot.StatusError || len(create.calls) != 0 {
		t.Fatalf("a link was created before an unknown id refused the batch: %+v, calls = %d", res, len(create.calls))
	}
}

func okEditor() *fakeEditor {
	return &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
}

func TestStudioEditPassesTableOperationsInBothEditors(t *testing.T) {
	table := map[string]interface{}{
		"op":            "add_table",
		"ref":           "precos",
		"table_rows":    []interface{}{[]interface{}{"Plano", "Preço"}, []interface{}{"Básico", "R$ 49"}},
		"table_columns": []interface{}{map[string]interface{}{"width": 2.0}, map[string]interface{}{"align": "right"}},
		"table_style":   map[string]interface{}{"borders": "all", "header_fill": "#111111", "font_size": 0.07, "header_weight": 800.0},
	}
	image := okEditor()
	args := map[string]interface{}{"operations": []interface{}{table, map[string]interface{}{"op": "style_text", "layer_id": "@precos", "table_row": 1.0, "table_column": 0.0, "match": "Básico", "font_weight": 700.0}, map[string]interface{}{"op": "update_table", "layer_id": "@precos", "table_row": 1.0, "table_column": 1.0, "text": "R$ 59"}}}
	if res := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioImage, image), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := image.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"add_table"`, `"table_rows":[["Plano","Preço"],["Básico","R$ 49"]]`, `"align":"right"`, `"borders":"all"`, `"header_weight":800`, `"table_row":1`, `"table_column":0`, `"op":"update_table"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	video := okEditor()
	videoTable := map[string]interface{}{"op": "add_table", "table_rows": []interface{}{[]interface{}{"A"}}, "at_ms": 0.0}
	if res := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioVideo, video), map[string]interface{}{"operations": []interface{}{videoTable}}); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
}

func TestStudioEditRefusesBadTableArguments(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"weight off the hundreds": {"table_style": map[string]interface{}{"weight": 450.0}},
		"unknown borders":         {"table_style": map[string]interface{}{"borders": "dotted"}},
		"font too big":            {"table_style": map[string]interface{}{"font_size": 0.5}},
		"bad color":               {"table_style": map[string]interface{}{"fill": "white"}},
		"column too wide":         {"table_columns": []interface{}{map[string]interface{}{"width": 500.0}}},
		"tab inside a cell":       {"table_rows": []interface{}{[]interface{}{"a\tb"}}},
		"negative row":            {"table_row": -1.0},
	}
	for name, extra := range cases {
		op := map[string]interface{}{"op": "add_table", "table_rows": []interface{}{[]interface{}{"x"}}}
		for key, value := range extra {
			op[key] = value
		}
		err := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil)).(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioImage, okEditor()), map[string]interface{}{"operations": []interface{}{op}})
		if !errors.Is(err, copilottools.ErrInvalidArgs) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestStudioEditCreatesTheShortLinkThroughTheUseCaseAndSendsItsAddress(t *testing.T) {
	create := &fakeLinkCreate{}
	editor := okEditor()
	args := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_qr_code", "qr_url": "https://loja.com/oferta", "logo_media_id": "media-logo", "logo_scale": 0.25}}}
	tool := NewStudioEditImageTool(linkDeps(studio.KindImage, create, fakeAccess{}))
	if err := tool.(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioImage, editor), args); err != nil {
		t.Fatalf("validate = %v", err)
	}
	if len(create.calls) != 0 {
		t.Fatalf("validation created a link: %+v", create.calls)
	}
	if res := tool.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	if len(create.calls) != 1 || create.calls[0].WorkspaceID != "ws-1" || create.calls[0].CreatedBy != "u-1" || create.calls[0].TargetURL != "https://loja.com/oferta" {
		t.Fatalf("the use case got %+v", create.calls)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"short_links":{"3f2c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e0f":"https://vzk.to/aB3dE9"}`, `"qr_link":"short_link"`, `"short_link_id":"3f2c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e0f"`, `"logo_scale":0.25`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	if strings.Contains(sent, `"qr_url"`) {
		t.Fatalf("the target address went to the editor instead of the short link: %s", sent)
	}
}

func TestStudioEditReusesAnExistingShortLinkOfTheWorkspace(t *testing.T) {
	editor := okEditor()
	args := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_qr_code", "short_link_id": "0d0c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e01", "at_ms": 0.0}}}
	if res := NewStudioEditVideoTool(linkDeps(studio.KindVideo, &fakeLinkCreate{}, fakeAccess{})).Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	if sent := renderJSON(cmd.Args); !strings.Contains(sent, `"short_links":{"0d0c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e01":"https://vzk.to/promo"}`) {
		t.Fatalf("the existing link was not resolved: %s", sent)
	}
	unknown := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_qr_code", "short_link_id": "9d0c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e09", "at_ms": 0.0}}}
	if res := NewStudioEditVideoTool(linkDeps(studio.KindVideo, &fakeLinkCreate{}, fakeAccess{})).Execute(context.Background(), studioSession(copilot.StudioVideo, okEditor()), unknown); res.Status != copilot.StatusError || !strings.Contains(res.Message, "não existe neste workspace") {
		t.Fatalf("an invented link id was accepted: %+v", res)
	}
}

func TestStudioEditRefusesShortLinksWithoutPermissionOrWiring(t *testing.T) {
	args := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_qr_code", "qr_url": "https://loja.com"}}}
	create := &fakeLinkCreate{}
	editor := okEditor()
	res := NewStudioEditImageTool(linkDeps(studio.KindImage, create, fakeAccess{denied: map[workspace.Action]bool{workspace.ActionCreate: true}})).Execute(context.Background(), studioSession(copilot.StudioImage, editor), args)
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "PERMISSÃO NEGADA") || len(create.calls) != 0 {
		t.Fatalf("result = %+v, calls = %d", res, len(create.calls))
	}
	if _, sent := editor.sent(copilot.ScreenEdit); sent {
		t.Fatal("the editor got a batch without the short link")
	}
	admin := studioSession(copilot.StudioImage, okEditor())
	admin.SystemAdmin = true
	if res := NewStudioEditImageTool(linkDeps(studio.KindImage, &fakeLinkCreate{}, fakeAccess{denied: map[workspace.Action]bool{workspace.ActionCreate: true}})).Execute(context.Background(), admin, args); res.Status != copilot.StatusOK {
		t.Fatalf("a system admin was refused: %+v", res)
	}
	if res := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioImage, okEditor()), args); res.Status != copilot.StatusError {
		t.Fatalf("an unwired short link service let the batch through: %+v", res)
	}
	plain := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_qr_code", "qr_link": "url", "qr_url": "https://loja.com"}}}
	if res := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioImage, okEditor()), plain); res.Status != copilot.StatusOK {
		t.Fatalf("a plain address needs no short link: %+v", res)
	}
}

func TestStudioEditExplainsWhyTheUseCaseRefusedTheTarget(t *testing.T) {
	create := &fakeLinkCreate{err: shortlink.ErrThreatDetected}
	args := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_qr_code", "qr_url": "https://loja.com"}}}
	res := NewStudioEditImageTool(linkDeps(studio.KindImage, create, fakeAccess{})).Execute(context.Background(), studioSession(copilot.StudioImage, okEditor()), args)
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "malicious") {
		t.Fatalf("result = %+v", res)
	}
	bad := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_qr_code", "qr_url": "ftp://loja.com"}}}
	if err := NewStudioEditImageTool(linkDeps(studio.KindImage, &fakeLinkCreate{}, fakeAccess{})).(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioImage, okEditor()), bad); !errors.Is(err, copilottools.ErrInvalidArgs) {
		t.Fatalf("err = %v", err)
	}
}

func TestStudioEditRefusesBadQRArguments(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"alpha color":            {"qr_link": "url", "qr_url": "https://a.com", "qr_dark": "#000000ff"},
		"unknown level":          {"qr_link": "url", "qr_url": "https://a.com", "qr_ecc": "X"},
		"quiet zone of one":      {"qr_link": "url", "qr_url": "https://a.com", "qr_quiet_zone": 1.0},
		"logo past H":            {"qr_link": "url", "qr_url": "https://a.com", "logo_scale": 0.4},
		"logo past the level":    {"qr_link": "url", "qr_url": "https://a.com", "qr_ecc": "M", "logo_scale": 0.25},
		"whatsapp without phone": {"qr_link": "whatsapp"},
		"text without text":      {"qr_link": "text", "qr_text": "  "},
		"short link without url": {"qr_link": "short_link"},
		"both url and id":        {"qr_url": "https://a.com", "short_link_id": "0d0c8a1e-9b4d-4c2a-8e1f-5a6b7c8d9e01"},
		"unknown link":           {"qr_link": "sms"},
	}
	for name, extra := range cases {
		op := map[string]interface{}{"op": "add_qr_code"}
		for key, value := range extra {
			op[key] = value
		}
		err := NewStudioEditImageTool(linkDeps(studio.KindImage, &fakeLinkCreate{}, fakeAccess{})).(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioImage, okEditor()), map[string]interface{}{"operations": []interface{}{op}})
		if !errors.Is(err, copilottools.ErrInvalidArgs) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}
