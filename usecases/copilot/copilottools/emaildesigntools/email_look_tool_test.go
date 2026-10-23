package emaildesigntools

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/emaildesign"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, 0, color.NRGBA{R: 200, A: 128})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func seeing(r *rig) copilot.Context {
	cc := r.cc
	cc.SeesImages = true
	return cc
}

func TestEmailLookFailsClosedForAModelThatCannotSee(t *testing.T) {
	r := newRig()
	res := NewEmailLookTool(r.deps).Execute(context.Background(), r.cc, nil)
	if res.Status != copilot.StatusError || res.Message != emailBlind || len(r.editor.commands) != 0 {
		t.Fatalf("a blind model never asks the editor for a capture, got %+v", res)
	}
}

func TestEmailLookSendsTheRealEmailWithItsImagesInlined(t *testing.T) {
	r := newRig()
	r.deps.Files = fakeFiles{"images/ws-1/hero.png": pngBytes(t, 40, 20)}
	res := NewEmailLookTool(r.deps).Execute(context.Background(), seeing(r), args(t, `{"device":"mobile","scheme":"dark","marks":true}`))
	if res.Status != copilot.StatusOK || len(res.Images) != 1 {
		t.Fatalf("got %+v", res)
	}
	if len(r.designs.rendered) != 1 || !r.designs.rendered[0].Annotate {
		t.Fatal("the look renders the live document with block ids")
	}
	looks := r.editor.sent(copilot.ScreenLook)
	payload := looks[0].Args.(lookPayload)
	if payload.Device != "mobile" || payload.Scheme != "dark" || !payload.Marks || strings.Contains(payload.HTML, cdnBase) || !strings.Contains(payload.HTML, "data:image/png;base64,") || !strings.Contains(payload.HTML, `data-block-id="hero"`) {
		t.Fatalf("got %+v", payload)
	}
	data := decoded(t, res.Data)
	report := data["images"].([]interface{})[0].(map[string]interface{})
	if report["block_id"] != "hero" || report["state"] != imageShown || report["width"] != float64(40) {
		t.Fatalf("got %+v", report)
	}
	if !strings.Contains(mustJSON(t, data["image_checks"]), "image_low_resolution") {
		t.Fatalf("a tiny image is flagged, got %+v", data)
	}
}

func TestEmailLookNamesImagesItCouldNotSee(t *testing.T) {
	r := newRig()
	r.editor.live.Blocks[1].Image.Src = "https://outra.example.com/a.jpg"
	res := NewEmailLookTool(r.deps).Execute(context.Background(), seeing(r), nil)
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	report := decoded(t, res.Data)["images"].([]interface{})[0].(map[string]interface{})
	if report["state"] != imageExternal {
		t.Fatalf("got %+v", report)
	}
	payload := r.editor.sent(copilot.ScreenLook)[0].Args.(lookPayload)
	if payload.Device != "both" || payload.Scheme != "light" {
		t.Fatalf("the look defaults to both devices in light, got %+v", payload)
	}
}

func TestEmailLookRefusesAnUnknownFocusAndAnInvalidEmail(t *testing.T) {
	r := newRig()
	if res := NewEmailLookTool(r.deps).Execute(context.Background(), seeing(r), args(t, `{"focus_id":"ghost"}`)); res.Status != copilot.StatusError || len(r.editor.sent(copilot.ScreenLook)) != 0 {
		t.Fatalf("got %+v", res)
	}
	r.editor.live.Blocks[1].Image.Src = "http://cdn.example.com/a.jpg"
	res := NewEmailLookTool(r.deps).Execute(context.Background(), seeing(r), nil)
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "image_src_not_https") {
		t.Fatalf("an email that cannot render says why, got %+v", res)
	}
}

func TestEmailLookRefusesAMissingCapture(t *testing.T) {
	r := newRig()
	r.editor.replies = map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenLook: {OK: true, Data: []byte(`{}`)}}
	if res := NewEmailLookTool(r.deps).Execute(context.Background(), seeing(r), nil); res.Status != copilot.StatusError || res.Message != emailNoCapture {
		t.Fatalf("got %+v", res)
	}
}

func TestInlinedImagesStayWithinTheirBudget(t *testing.T) {
	doc := liveDocument()
	html := `<img src="` + doc.Blocks[1].Image.Src + `" />`
	deps := Deps{Files: fakeFiles{"images/ws-1/hero.png": pngBytes(t, 2400, 1200)}}
	out, report := deps.inlineImages(context.Background(), doc, html, 10)
	if report[0].State != imageSkipped || out != html {
		t.Fatalf("an image over the budget is skipped, got %+v", report)
	}
	out, report = deps.inlineImages(context.Background(), doc, html, maxInlinedBytes)
	if report[0].State != imageShown || !strings.HasPrefix(strings.TrimPrefix(out, `<img src="`), "data:image/png;base64,") {
		t.Fatalf("got %+v", report)
	}
	if got := emaildesign.ImageWidths(doc)["hero"]; got != 540 {
		t.Fatalf("got %d", got)
	}
}

func TestEmailLookShowsADraftWithGaps(t *testing.T) {
	r := newRig()
	r.editor.live.Blocks[2] = emaildesign.Block{ID: "cta", Kind: emaildesign.KindButton, Button: &emaildesign.Button{Label: "Agendar"}}
	res := NewEmailLookTool(r.deps).Execute(context.Background(), seeing(r), nil)
	if res.Status != copilot.StatusOK || !r.designs.rendered[0].Draft {
		t.Fatalf("Elo sees the draft before it can be sent, got %+v", res)
	}
}
