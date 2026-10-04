package copilottools

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
)

const newCardMedia = "0b9e6c1a-6d8f-4a4e-9a51-3f1c2b7d8e90"

func carouselDetail() *advertising.ObjectDetail {
	detail := adDetail("120303", "Todos os canais numa caixa só")
	detail.Creative = &advertising.CreativeDraft{
		Format: advertising.FormatCarousel, PrimaryText: "Todos os canais numa caixa só", Greeting: "Olá!",
		Cards: []advertising.CarouselCard{
			{Media: advertising.MetaMediaRef(advertising.MediaImage, "h1"), Headline: "Canais"},
			{Media: advertising.MetaMediaRef(advertising.MediaImage, "h2"), Headline: "aCada conversa"},
		},
	}
	return detail
}

func swapTool(t *testing.T) (copilot.Tool, *manageFixture) {
	t.Helper()
	f := newManageFixture()
	f.editor.details["120303"] = carouselDetail()
	f.editor.urls = map[string]string{"meta:h1": "https://cdn.meta/h1.png", "meta:h2": "https://cdn.meta/h2.png", newCardMedia: "https://cdn/new.png"}
	return f.tool(t, "swap_ad_creative"), f
}

func TestSwapAdCreativeChangesOnlyWhatWasAskedAndKeepsTheRest(t *testing.T) {
	tool, f := swapTool(t)
	args := map[string]interface{}{
		"meta_id": "120303",
		"cards": []interface{}{
			map[string]interface{}{"media_id": "meta:h1", "headline": "Canais"},
			map[string]interface{}{"media_id": "meta:h2", "headline": "Cada conversa no funil"},
		},
		"enhancements": false,
	}
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, args); err != nil {
		t.Fatal(err)
	}
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	edit := f.editor.edited["120303"]
	if edit.Creative == nil || edit.Creative.Format != advertising.FormatCarousel || edit.Creative.PrimaryText != "Todos os canais numa caixa só" ||
		edit.Creative.Greeting != "Olá!" || edit.Creative.Cards[1].Headline != "Cada conversa no funil" || edit.Creative.Cards[1].Media.MediaID != "meta:h2" {
		t.Fatalf("edit %+v", edit.Creative)
	}
}

func TestSwapAdCreativePreviewsTheNewCreativeWithEveryImage(t *testing.T) {
	tool, _ := swapTool(t)
	args := map[string]interface{}{"meta_id": "120303", "format": "IMAGE", "media_id": newCardMedia, "headline": "Uma caixa só"}
	preview := tool.(copilot.Previewer).Preview(context.Background(), adContext, args)
	if preview == nil || preview.Kind != PreviewAdCreative {
		t.Fatalf("preview %+v", preview)
	}
	data := preview.Data.(AdCreativePreview)
	if data.Format != advertising.FormatImage || data.MediaURL != "https://cdn/new.png" || data.Headline != "Uma caixa só" || data.Greeting != "Olá!" {
		t.Fatalf("preview data %+v", data)
	}
}

func TestSwapAdCreativeRefusesMediaThatIsNotTheAdsOrTheLibrarys(t *testing.T) {
	tool, f := swapTool(t)
	for _, id := range []string{"meta:outra", "inventado"} {
		args := map[string]interface{}{"meta_id": "120303", "format": "IMAGE", "media_id": id}
		if err := tool.(copilot.Validator).Validate(context.Background(), adContext, args); !errors.Is(err, errInvalidArgs) {
			t.Fatalf("%s: err %v", id, err)
		}
	}
	if len(f.editor.edited) != 0 {
		t.Fatal("edited with an unknown media")
	}
}

func TestSwapAdCreativeOnlySwapsAds(t *testing.T) {
	tool, _ := swapTool(t)
	err := tool.(copilot.Validator).Validate(context.Background(), adContext, map[string]interface{}{"meta_id": "120201", "headline": "x"})
	if !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
}
