package copilot

import (
	"errors"
	"testing"
)

func TestAStudioProjectCardCarriesTheOrderAsIs(t *testing.T) {
	order := StudioProjectOrder{
		Kind: "image", Name: "Promoção", Sizes: []CanvasSize{{Width: 1440, Height: 1800}, {Width: 1440, Height: 2560}},
		Media: []StudioProjectMedia{{MediaID: "m1", URL: "https://cdn/m1.png", Type: "image"}},
	}
	card, err := NewStudioProjectCard(order)
	if err != nil {
		t.Fatal(err)
	}
	if card.Kind != ActionStudioProject || card.StudioProject == nil || len(card.StudioProject.Sizes) != 2 || card.StudioProject.Media[0].URL != order.Media[0].URL {
		t.Fatalf("got %+v", card)
	}
}

func TestAStudioProjectCardRefusesAnIncoherentOrder(t *testing.T) {
	bad := []StudioProjectOrder{
		{Kind: "audio"},
		{Kind: "image", Aspect: "story"},
		{Kind: "image", DurationMs: 3000},
		{Kind: "video", Sizes: []CanvasSize{{Width: 1080, Height: 1080}}},
		{Kind: "video", Aspect: "cinema"},
		{Kind: "image", Sizes: []CanvasSize{{Width: 50, Height: 1080}}},
		{Kind: "image", Media: []StudioProjectMedia{{MediaID: "m1", URL: "https://cdn/a.mp4", Type: "video"}}},
		{Kind: "image", Media: []StudioProjectMedia{{MediaID: "m1", Type: "image"}}},
		{Kind: "video", DurationMs: 20},
		{Kind: "image", ProjectID: "p1"},
	}
	for _, order := range bad {
		if _, err := NewStudioProjectCard(order); !errors.Is(err, ErrInvalidStudioProject) {
			t.Errorf("%+v must be refused, got %v", order, err)
		}
	}
}

func TestAVideoProjectAcceptsEveryKindOfMedia(t *testing.T) {
	order := StudioProjectOrder{Kind: "video", Aspect: "story", DurationMs: 4000, Media: []StudioProjectMedia{
		{MediaID: "a", URL: "https://cdn/a.png", Type: "image"}, {MediaID: "b", URL: "https://cdn/b.mp4", Type: "video"}, {MediaID: "c", URL: "https://cdn/c.mp3", Type: "audio"},
	}}
	if _, err := NewStudioProjectCard(order); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStudioProjectCard(StudioProjectOrder{Kind: "image"}); err != nil {
		t.Fatalf("an empty image project is a valid order: %v", err)
	}
}
