package choicestats_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/choicestats"
	"vozko/domain/shortlink"
	"vozko/domain/whatsapp/template"
)

type fakeCodes struct {
	links map[string]*shortlink.ShortLink
	err   error
}

func (f fakeCodes) FindByCode(_ context.Context, code string) (*shortlink.ShortLink, error) {
	if f.err != nil {
		return nil, f.err
	}
	link, ok := f.links[code]
	if !ok {
		return nil, shortlink.ErrShortLinkNotFound
	}
	return link, nil
}

func buttons(bs ...template.TemplateButton) []template.TemplateComponent {
	return []template.TemplateComponent{{Type: "BODY", Text: "Oferta"}, {Type: "BUTTONS", Buttons: bs}}
}

func newTracking(t *testing.T, creator *fakeLinkCreator, codes fakeCodes) *ButtonTracking {
	t.Helper()
	links, err := NewTrackedLinks(creator, "https://vz.co/r")
	if err != nil {
		t.Fatal(err)
	}
	tracking, err := NewButtonTracking(links, codes)
	if err != nil {
		t.Fatal(err)
	}
	return tracking
}

func TestTrackingAButtonPointsItAtItsShortLinkAndUntrackingRestoresTheDestination(t *testing.T) {
	creator := &fakeLinkCreator{}
	codes := fakeCodes{links: map[string]*shortlink.ShortLink{"Old1": {Code: "Old1", WorkspaceID: "ws", TargetURL: "https://loja.com/antigo"}}}
	tracking := newTracking(t, creator, codes)
	in := buttons(
		template.TemplateButton{Type: "QUICK_REPLY", Text: "Quero"},
		template.TemplateButton{Type: "URL", Text: "Loja", URL: "https://loja.com/promo"},
		template.TemplateButton{Type: "URL", Text: "Antigo", URL: choicestats.TrackedButtonURL("https://vz.co/r", "Old1")},
	)
	out, err := tracking.Apply(context.Background(), "ws", "user-1", in, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	got := out[1].Buttons
	if got[1].URL != "https://vz.co/r/Ab3x/{{1}}" || got[1].Example == "" || creator.in.TargetURL != "https://loja.com/promo" {
		t.Fatalf("tracked button = %+v", got[1])
	}
	if got[2].URL != "https://loja.com/antigo" || got[2].Example != "" {
		t.Fatalf("untracked button = %+v, want its destination back", got[2])
	}
	if in[1].Buttons[1].URL != "https://loja.com/promo" {
		t.Fatal("the request components were changed in place")
	}
	if tracked := template.TrackedURLButtonsOf(out, "https://vz.co/r"); len(tracked) != 1 || tracked[0] != 1 {
		t.Fatalf("tracked after apply = %v", tracked)
	}

	kept, err := tracking.Apply(context.Background(), "ws", "user-1", out, []int{1})
	if err != nil || kept[1].Buttons[1].URL != "https://vz.co/r/Ab3x/{{1}}" {
		t.Fatalf("an already tracked button must keep its link: %+v %v", kept[1].Buttons[1], err)
	}
}

func TestTrackingRefusesWhatItCannotTrackOrRestore(t *testing.T) {
	cases := []struct {
		name    string
		in      []template.TemplateComponent
		indexes []int
		creator *fakeLinkCreator
		codes   fakeCodes
		want    error
		button  int
	}{
		{"a quick reply has no link", buttons(template.TemplateButton{Type: "QUICK_REPLY", Text: "Quero"}), []int{0}, &fakeLinkCreator{}, fakeCodes{}, template.ErrTrackedButtonNotURL, 0},
		{"an index past the buttons", buttons(template.TemplateButton{Type: "URL", URL: "https://a.com"}), []int{3}, &fakeLinkCreator{}, fakeCodes{}, template.ErrTrackedButtonNotURL, 3},
		{"a dynamic link", buttons(template.TemplateButton{Type: "QUICK_REPLY", Text: "Quero"}, template.TemplateButton{Type: "URL", URL: "https://a.com/{{1}}"}), []int{1}, &fakeLinkCreator{}, fakeCodes{}, template.ErrTrackedButtonDynamic, 1},
		{"a destination the shortener refuses", buttons(template.TemplateButton{Type: "URL", URL: "http://10.0.0.1"}), []int{0}, &fakeLinkCreator{err: shortlink.ErrTargetURLBlocked}, fakeCodes{}, template.ErrTrackedButtonDestination, 0},
		{"a link of another workspace", buttons(template.TemplateButton{Type: "URL", URL: choicestats.TrackedButtonURL("https://vz.co/r", "Old1")}), nil, &fakeLinkCreator{},
			fakeCodes{links: map[string]*shortlink.ShortLink{"Old1": {Code: "Old1", WorkspaceID: "other", TargetURL: "https://x.com"}}}, template.ErrTrackedLinkUnreadable, 0},
		{"an unreadable link", buttons(template.TemplateButton{Type: "URL", URL: choicestats.TrackedButtonURL("https://vz.co/r", "Old1")}), nil, &fakeLinkCreator{}, fakeCodes{err: errors.New("db down")}, template.ErrTrackedLinkUnreadable, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTracking(t, tc.creator, tc.codes).Apply(context.Background(), "ws", "user-1", tc.in, tc.indexes)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if button, ok := template.TrackedButtonIndex(err); !ok || button != tc.button {
				t.Fatalf("refused button = %d (%v), want %d so the editor can show the refusal on it", button, ok, tc.button)
			}
		})
	}
}
