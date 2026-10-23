package emaildesigntools

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	"vozko/domain/media"
	"vozko/usecases/copilot/copilottools/tooltest"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

const (
	designID     = "8d6f2b1a-3c4e-4f5a-9b7c-1d2e3f4a5b6c"
	libraryImage = "3e2d1c0b-9a8f-4e7d-8c6b-5a4f3e2d1c0b"
	cdnBase      = "https://cdn.example.com/"
)

var designView = copilot.View{Surface: copilot.SurfaceEmailDesign, ProjectID: designID}

type fakeDesigns struct {
	design   *emailcampaign.Design
	created  []emailcampaign.DesignInput
	rendered []emailcampaign_usecase.PreviewInput
	html     string
	err      error
}

func (f *fakeDesigns) GetDesign(_ context.Context, a emailcampaign_usecase.Actor, id string) (*emailcampaign.Design, error) {
	if f.design == nil || f.design.ID != id || f.design.WorkspaceID != a.WorkspaceID {
		return nil, emailcampaign.ErrDesignNotFound
	}
	return f.design, nil
}

func (f *fakeDesigns) CreateDesign(_ context.Context, a emailcampaign_usecase.Actor, in emailcampaign.DesignInput) (*emailcampaign.Design, error) {
	if f.err != nil {
		return nil, f.err
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}
	f.created = append(f.created, in)
	return &emailcampaign.Design{ID: designID, WorkspaceID: a.WorkspaceID, Name: in.Name, Document: in.Document, Kind: in.Kind}, nil
}

func (f *fakeDesigns) Render(in emailcampaign_usecase.PreviewInput) (emailcampaign_usecase.Preview, error) {
	f.rendered = append(f.rendered, in)
	if f.err != nil {
		return emailcampaign_usecase.Preview{}, f.err
	}
	html := f.html
	if html == "" {
		rendered, err := emaildesign.Render(in.Document, emaildesign.RenderInput{UnsubscribeURL: "https://app.example.com/u", UnsubscribeLabel: "Sair", Annotate: in.Annotate, Draft: in.Draft})
		if err != nil {
			return emailcampaign_usecase.Preview{}, err
		}
		html = rendered.HTML
	}
	return emailcampaign_usecase.Preview{HTML: html}, nil
}

type fakeLibrary map[string]*media.Media

func (f fakeLibrary) GetMedia(workspaceID, id string) (*media.Media, error) {
	m, ok := f[id]
	if !ok || m.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return m, nil
}

type fakeFiles map[string][]byte

func (f fakeFiles) KeyFromURL(url string) (string, bool) {
	if len(url) <= len(cdnBase) || url[:len(cdnBase)] != cdnBase {
		return "", false
	}
	return url[len(cdnBase):], true
}

func (f fakeFiles) DownloadFile(_ context.Context, key string) ([]byte, string, error) {
	data, ok := f[key]
	if !ok {
		return nil, "", media.ErrMediaNotFound
	}
	return data, "image/png", nil
}

type fakeEditor struct {
	live     emaildesign.Document
	envelope *envelope
	typing   func(*fakeEditor)
	selected string
	replies  map[copilot.ScreenCommandName]copilot.ScreenReply
	err      error
	commands []copilot.ScreenCommand
}

func (f *fakeEditor) reply(fields map[string]interface{}) copilot.ScreenReply {
	if f.envelope != nil {
		fields["subject"], fields["preheader"] = f.envelope.Subject, f.envelope.Preheader
	}
	data, _ := json.Marshal(fields)
	return copilot.ScreenReply{OK: true, Data: data}
}

func (f *fakeEditor) mergeEnvelope(edit *envelopeEdit) []string {
	if edit == nil || f.envelope == nil {
		return nil
	}
	var conflicts []string
	pick := func(path string, base, next string, live *string) {
		switch {
		case next == base || next == *live:
		case *live == base:
			*live = next
		default:
			conflicts = append(conflicts, path)
		}
	}
	pick("subject", edit.Base.Subject, edit.Next.Subject, &f.envelope.Subject)
	pick("preheader", edit.Base.Preheader, edit.Next.Preheader, &f.envelope.Preheader)
	return conflicts
}

func (f *fakeEditor) Run(_ context.Context, cmd copilot.ScreenCommand) (copilot.ScreenReply, error) {
	f.commands = append(f.commands, cmd)
	if f.err != nil {
		return copilot.ScreenReply{}, f.err
	}
	if reply, ok := f.replies[cmd.Name]; ok {
		return reply, nil
	}
	switch cmd.Name {
	case copilot.ScreenRead:
		return f.reply(map[string]interface{}{"document": f.live, "selected": f.selected, "name": "Black Friday"}), nil
	case copilot.ScreenEdit:
		if f.typing != nil {
			f.typing(f)
		}
		payload := cmd.Args.(editPayload)
		final := replayed(payload)
		f.live = final
		conflicts := append([]string{}, f.mergeEnvelope(payload.Envelope)...)
		return f.reply(map[string]interface{}{"document": final, "conflicts": conflicts, "skipped": []int{}}), nil
	case copilot.ScreenLook:
		return copilot.ScreenReply{OK: true, Data: []byte(`{"marks":[]}`), Images: []string{"data:image/jpeg;base64,AAAA"}}, nil
	}
	return copilot.ScreenReply{OK: true, Data: []byte(`{}`)}, nil
}

func (f *fakeEditor) sent(name copilot.ScreenCommandName) []copilot.ScreenCommand {
	var out []copilot.ScreenCommand
	for _, cmd := range f.commands {
		if cmd.Name == name {
			out = append(out, cmd)
		}
	}
	return out
}

func replayed(payload editPayload) emaildesign.Document {
	doc := payload.Base
	for _, step := range payload.Steps {
		if step.Settings != nil {
			doc.Settings = *step.Settings
		}
		byID := map[string]emaildesign.Block{}
		order := make([]string, 0, len(doc.Blocks))
		for _, b := range doc.Blocks {
			byID[b.ID] = b
			order = append(order, b.ID)
		}
		if step.Order != nil {
			order = *step.Order
		}
		blocks := make([]emaildesign.Block, 0, len(order))
		for _, id := range order {
			if b, ok := step.Blocks[id]; ok {
				blocks = append(blocks, b)
				continue
			}
			blocks = append(blocks, byID[id])
		}
		doc = emaildesign.Document{Version: doc.Version, Settings: doc.Settings, Blocks: blocks}
	}
	return doc
}

func liveDocument() emaildesign.Document {
	return emaildesign.Document{Version: emaildesign.CurrentVersion, Settings: emaildesign.Settings{Preheader: "Ofertas"}, Blocks: []emaildesign.Block{
		{ID: "title", Kind: emaildesign.KindHeading, Heading: &emaildesign.Heading{Text: "Black Friday", Level: 1}},
		{ID: "hero", Kind: emaildesign.KindImage, Image: &emaildesign.Image{Src: cdnBase + "images/ws-1/hero.png", Alt: "Vitrine", Width: 100}},
		{ID: "intro", Kind: emaildesign.KindText, Text: &emaildesign.Text{Paragraphs: []emaildesign.Paragraph{{Runs: []emaildesign.Run{{Text: "Olá"}}}}}},
		{ID: "footer", Kind: emaildesign.KindFooter, Footer: &emaildesign.Footer{CompanyName: "Loja", Address: "Rua A, 1"}},
	}}
}

func counter() emaildesign.IDSource {
	n := 0
	return func(kind emaildesign.BlockKind, taken map[string]bool) string {
		n++
		return string(kind) + "-" + strconv.Itoa(n)
	}
}

type rig struct {
	deps    Deps
	designs *fakeDesigns
	editor  *fakeEditor
	images  *tooltest.StubImages
	cc      copilot.Context
}

func newRig() *rig {
	designs := &fakeDesigns{design: &emailcampaign.Design{ID: designID, WorkspaceID: "ws-1", Name: "Black Friday"}}
	editor := &fakeEditor{live: liveDocument()}
	images := &tooltest.StubImages{}
	library := fakeLibrary{libraryImage: {ID: libraryImage, WorkspaceID: "ws-1", URL: cdnBase + "images/ws-1/library.jpg", Type: media.MediaTypeProductImage}}
	deps := Deps{Designs: designs, Library: library, Files: fakeFiles{}, Media: images, IDs: counter()}
	return &rig{deps: deps, designs: designs, editor: editor, images: images, cc: tooltest.OwnerWithScreen(designView, editor)}
}

func decoded(t *testing.T, data interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return out
}
