package emaildesigntools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/mediagen"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func generated(url string) *mediagen.Job {
	return &mediagen.Job{ID: "job-1", Kind: mediagen.KindImage, Status: mediagen.StatusDone, MediaID: "9c8b7a6f-5e4d-4c3b-8a2f-1e0d9c8b7a6f", MediaURL: url, Model: tooltest.PickedImageModel, Prompt: "tênis"}
}

func generateArgs(t *testing.T, extra string) map[string]interface{} {
	t.Helper()
	return args(t, `{"prompt":"tênis de corrida sobre fundo laranja","aspect":"landscape","alt":"Tênis de corrida","image_model":"`+tooltest.PickedImageModel+`"`+extra+`}`)
}

func TestGeneratedImageLandsInTheChosenBlockAsAPublicURL(t *testing.T) {
	r := newRig()
	r.images.Final = generated("https://cdn.example.com/images/ws-1/new.jpg")
	r.cc.ChargeReference = "aichat:t1"
	res := NewEmailGenerateImageTool(r.deps).Execute(context.Background(), r.cc, generateArgs(t, `,"block_id":"hero","href":"https://loja.example.com"`))
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	hero := r.editor.live.Blocks[1].Image
	if hero.Src != "https://cdn.example.com/images/ws-1/new.jpg" || hero.Alt != "Tênis de corrida" || hero.Href != "https://loja.example.com" {
		t.Fatalf("got %+v", hero)
	}
	if r.images.Requested.Kind != mediagen.KindImage || r.images.Requested.BillingReference != "aichat:t1" || r.images.Requested.Aspect != mediagen.AspectLandscape {
		t.Fatalf("the generation goes through the media module, billed to the thread, got %+v", r.images.Requested)
	}
	data := decoded(t, res.Data)
	if data["media_id"] != generated("").MediaID || data["block_id"] != "hero" || res.Media == nil {
		t.Fatalf("got %+v", data)
	}
}

func TestGeneratedImageCanStartANewBlock(t *testing.T) {
	r := newRig()
	r.images.Final = generated("https://cdn.example.com/images/ws-1/new.jpg")
	res := NewEmailGenerateImageTool(r.deps).Execute(context.Background(), r.cc, generateArgs(t, `,"at":{"after":"title"},"width":80`))
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	added := r.editor.live.Blocks[1]
	if added.ID != "image-1" || added.Image.Width != 80 || added.Image.Src != "https://cdn.example.com/images/ws-1/new.jpg" {
		t.Fatalf("got %+v", added)
	}
}

func TestAPlacementThatCannotWorkIsRefusedBeforeAnythingIsBilled(t *testing.T) {
	r := newRig()
	r.images.Final = generated("https://cdn.example.com/images/ws-1/new.jpg")
	for name, extra := range map[string]string{
		"not an image":  `,"block_id":"title"`,
		"unknown block": `,"block_id":"ghost"`,
		"both":          `,"block_id":"hero","at":{"after":"title"}`,
		"bad place":     `,"at":{"into":"title"}`,
	} {
		res := NewEmailGenerateImageTool(r.deps).Execute(context.Background(), r.cc, generateArgs(t, extra))
		if res.Status != copilot.StatusError || r.images.Requested.Kind != "" {
			t.Errorf("%s: nothing may be generated, got %+v", name, res)
		}
	}
}

func TestAnImageWithoutAPublicURLIsNeverPlaced(t *testing.T) {
	r := newRig()
	r.images.Final = generated("http://interno/images/new.jpg")
	res := NewEmailGenerateImageTool(r.deps).Execute(context.Background(), r.cc, generateArgs(t, `,"block_id":"hero"`))
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, generated("").MediaID) || len(r.editor.sent(copilot.ScreenEdit)) != 0 {
		t.Fatalf("got %+v", res)
	}
}

func TestAFailedPlacementHandsBackTheBilledImage(t *testing.T) {
	r := newRig()
	r.images.Final = generated("https://cdn.example.com/images/ws-1/new.jpg")
	silent := &silentEditEditor{fakeEditor: r.editor}
	cc := tooltest.OwnerWithScreen(designView, silent)
	res := NewEmailGenerateImageTool(r.deps).Execute(context.Background(), cc, generateArgs(t, `,"block_id":"hero"`))
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, generated("").MediaID) || !strings.Contains(res.Message, "email_edit") {
		t.Fatalf("the media id comes back so it is placed without generating again, got %+v", res)
	}
}

func TestEmailImageGenerationKeepsThePaidApproval(t *testing.T) {
	tool := NewEmailGenerateImageTool(Deps{})
	graded := tool.(copilot.Graded)
	if !graded.NeedsApproval(copilot.ModeAsk) || !graded.NeedsApproval(copilot.ModeEdit) || graded.NeedsApproval(copilot.ModeFull) {
		t.Fatal("a paid generation asks unless the person gave full access")
	}
	if meta := tool.Meta(); !meta.Mutating || meta.Resource != workspace.ResourceMedia || meta.Action != workspace.ActionCreate {
		t.Fatalf("got %+v", meta)
	}
	also := tool.(interface {
		AlsoRequires() []workspace.PermissionEntry
	}).AlsoRequires()
	if len(also) != 1 || also[0].Resource != workspace.ResourceEmailMarketing || also[0].Action != workspace.ActionUpdate {
		t.Fatalf("placing the image edits the email, got %+v", also)
	}
	def := tool.Definition()
	for _, name := range []string{"prompt", "aspect", "reference_media_ids", "block_id", "at", "alt"} {
		if _, ok := def.Parameters[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
}

func TestTheApprovalCardRecommendsAModelAndValidatesThePlacement(t *testing.T) {
	r := newRig()
	tool := NewEmailGenerateImageTool(r.deps)
	choices, err := tool.(copilot.ChoiceAsker).Choices(context.Background(), r.cc, generateArgs(t, `,"block_id":"hero"`))
	if err != nil || len(choices) != 1 || choices[0].Default == "" {
		t.Fatalf("got %+v, %v", choices, err)
	}
	if err := tool.(copilot.Validator).Validate(context.Background(), r.cc, args(t, `{"prompt":"x","aspect":"square","block_id":"hero"}`)); err == nil {
		t.Fatal("alt text is required")
	}
}
