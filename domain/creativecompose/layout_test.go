package creativecompose

import (
	"errors"
	"strings"
	"testing"
)

func validLayout() Layout {
	return Layout{
		Template: TemplateFeed, Eyebrow: "Ligações", Headline: "Ligações com filas", Highlight: "e transferência.",
		Subline: "Discador no navegador e música de espera.", ImageMediaID: "media-1",
		Callouts: []string{"Ligue sem sair da Vozko"}, CallToAction: "Fale com a gente no WhatsApp",
	}
}

func TestTemplatesHaveTheSizesMetaRecommends(t *testing.T) {
	for template, want := range map[Template][2]int{TemplateFeed: {1080, 1350}, TemplateCard: {1080, 1080}, TemplateStory: {1080, 1920}} {
		w, h, err := template.Size()
		if err != nil || w != want[0] || h != want[1] {
			t.Fatalf("%s: %dx%d %v", template, w, h, err)
		}
	}
	if _, _, err := Template("banner").Size(); err == nil {
		t.Fatal("unknown template accepted")
	}
}

func TestAValidLayoutPasses(t *testing.T) {
	if err := validLayout().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLayoutRefusesWhatWouldLookBroken(t *testing.T) {
	cases := map[string]func(*Layout){
		"no headline":      func(l *Layout) { l.Headline = " " },
		"no image":         func(l *Layout) { l.ImageMediaID = " " },
		"long headline":    func(l *Layout) { l.Headline = strings.Repeat("a", MaxHeadline+1) },
		"long subline":     func(l *Layout) { l.Subline = strings.Repeat("a", MaxSubline+1) },
		"too many callout": func(l *Layout) { l.Callouts = []string{"a", "b", "c", "d"} },
		"long callout":     func(l *Layout) { l.Callouts = []string{strings.Repeat("a", MaxCallout+1)} },
		"em dash":          func(l *Layout) { l.Subline = "Discador — filas" },
		"en dash":          func(l *Layout) { l.Headline = "Seg–Sex" },
		"bad template":     func(l *Layout) { l.Template = "banner" },
		"long footnote":    func(l *Layout) { l.Footnote = strings.Repeat("a", MaxFootnote+1) },
	}
	for name, mutate := range cases {
		l := validLayout()
		mutate(&l)
		var invalid *ValidationError
		if err := l.Validate(); !errors.As(err, &invalid) {
			t.Fatalf("%s: err %v", name, err)
		}
	}
}

func TestLayoutTrimsItsTexts(t *testing.T) {
	l := validLayout()
	l.Headline, l.Callouts = "  Ligações  ", []string{"  ", " Filas "}
	l.Normalize()
	if l.Headline != "Ligações" || len(l.Callouts) != 1 || l.Callouts[0] != "Filas" {
		t.Fatalf("normalized %+v", l)
	}
}
