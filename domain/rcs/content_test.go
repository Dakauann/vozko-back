package rcs

import (
	"errors"
	"strings"
	"testing"
)

func image() *Media {
	return &Media{Kind: MediaImage, URL: "https://media.example.com/a.jpg", MIMEType: "image/jpeg"}
}

func card(title string, suggestions ...Suggestion) Card {
	return Card{Media: image(), Title: title, Description: "desc", Suggestions: suggestions}
}

func reply(label string) Suggestion {
	return Suggestion{Kind: SuggestReply, Label: label, Postback: "pb-" + label}
}

func TestKindFollowsWhatTheMessageHolds(t *testing.T) {
	cases := []struct {
		name string
		msg  Content
		want ContentKind
	}{
		{"plain text", Content{Text: "Oi"}, KindText},
		{"text with buttons", Content{Text: "Oi", Suggestions: []Suggestion{reply("Sim")}}, KindText},
		{"media", Content{Media: image()}, KindMedia},
		{"one card", Content{Cards: []Card{card("A")}}, KindRichCard},
		{"two cards", Content{Cards: []Card{card("A"), card("B")}}, KindCarousel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.Kind(); got != tc.want {
				t.Fatalf("Kind() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestBillingClassIsBasicOnlyForShortPlainText(t *testing.T) {
	cases := []struct {
		name string
		msg  Content
		want BillingClass
	}{
		{"160 characters", Content{Text: strings.Repeat("a", 160)}, ClassBasic},
		{"160 accented characters count as characters", Content{Text: strings.Repeat("ã", 160)}, ClassBasic},
		{"161 characters", Content{Text: strings.Repeat("a", 161)}, ClassSingle},
		{"any button", Content{Text: "Oi", Suggestions: []Suggestion{reply("Sim")}}, ClassSingle},
		{"media", Content{Media: image()}, ClassSingle},
		{"rich card", Content{Cards: []Card{card("A")}}, ClassSingle},
		{"carousel", Content{Cards: []Card{card("A"), card("B")}}, ClassSingle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.BillingClass(); got != tc.want {
				t.Fatalf("BillingClass() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestValidateAcceptsEveryComposerShape(t *testing.T) {
	cases := []Content{
		{Text: "Seu pedido {{1}} saiu para entrega"},
		{Text: "Escolha", Suggestions: []Suggestion{
			reply("Sim"),
			{Kind: SuggestOpenURL, Label: "Ver pedido", URL: "https://loja.example.com/p/1"},
			{Kind: SuggestOpenURL, Label: "Rastrear", URL: "https://loja.example.com/t", Webview: true},
			{Kind: SuggestDial, Label: "Ligar", Phone: "+5511999990000"},
			{Kind: SuggestViewLocation, Label: "Loja", Latitude: -23.55, Longitude: -46.63, LocationLabel: "Loja"},
			{Kind: SuggestCalendar, Label: "Agendar", Calendar: &CalendarEvent{Title: "Visita", StartsAt: "2026-11-01T10:00:00-03:00", EndsAt: "2026-11-01T11:00:00-03:00"}},
		}},
		{Media: &Media{Kind: MediaPDF, URL: "https://media.example.com/b.pdf", MIMEType: "application/pdf"}},
		{Cards: []Card{card("A", reply("Comprar"))}},
		{Cards: []Card{card("A"), card("B"), card("C")}},
	}
	for i, c := range cases {
		if err := c.Validate(); err != nil {
			t.Fatalf("case %d: Validate() = %v", i, err)
		}
	}
}

func TestValidateRefusesWhatCarriersRefuse(t *testing.T) {
	tooManyReplies := make([]Suggestion, MaxMessageSuggestions+1)
	for i := range tooManyReplies {
		tooManyReplies[i] = reply(strings.Repeat("x", i+1))
	}
	tooManyCardButtons := make([]Suggestion, MaxCardSuggestions+1)
	for i := range tooManyCardButtons {
		tooManyCardButtons[i] = reply(strings.Repeat("y", i+1))
	}
	cards := make([]Card, MaxCarouselCards+1)
	for i := range cards {
		cards[i] = card("A")
	}
	cases := []struct {
		name string
		msg  Content
		want error
	}{
		{"empty", Content{}, ErrContentEmpty},
		{"text and cards together", Content{Text: "Oi", Cards: []Card{card("A")}}, ErrContentMixed},
		{"media and cards together", Content{Media: image(), Cards: []Card{card("A")}}, ErrContentMixed},
		{"text too long", Content{Text: strings.Repeat("a", MaxTextRunes+1)}, ErrTextTooLong},
		{"too many message buttons", Content{Text: "Oi", Suggestions: tooManyReplies}, ErrTooManySuggestions},
		{"too many card buttons", Content{Cards: []Card{card("A", tooManyCardButtons...)}}, ErrTooManySuggestions},
		{"too many cards", Content{Cards: cards}, ErrTooManyCards},
		{"card without title, text or media", Content{Cards: []Card{{}}}, ErrCardEmpty},
		{"card title too long", Content{Cards: []Card{card(strings.Repeat("t", MaxCardTitleRunes+1))}}, ErrCardTitleTooLong},
		{"label too long", Content{Text: "Oi", Suggestions: []Suggestion{reply(strings.Repeat("l", MaxSuggestionLabelRunes+1))}}, ErrSuggestionInvalid},
		{"empty label", Content{Text: "Oi", Suggestions: []Suggestion{{Kind: SuggestReply, Postback: "x"}}}, ErrSuggestionInvalid},
		{"duplicate labels", Content{Text: "Oi", Suggestions: []Suggestion{reply("Sim"), reply("Sim")}}, ErrSuggestionInvalid},
		{"unknown kind", Content{Text: "Oi", Suggestions: []Suggestion{{Kind: "teleport", Label: "x"}}}, ErrSuggestionInvalid},
		{"plain http link", Content{Text: "Oi", Suggestions: []Suggestion{{Kind: SuggestOpenURL, Label: "x", URL: "http://a.example.com"}}}, ErrSuggestionInvalid},
		{"dial without phone", Content{Text: "Oi", Suggestions: []Suggestion{{Kind: SuggestDial, Label: "x"}}}, ErrSuggestionInvalid},
		{"location out of range", Content{Text: "Oi", Suggestions: []Suggestion{{Kind: SuggestViewLocation, Label: "x", Latitude: 91}}}, ErrSuggestionInvalid},
		{"calendar ending before it starts", Content{Text: "Oi", Suggestions: []Suggestion{{Kind: SuggestCalendar, Label: "x", Calendar: &CalendarEvent{Title: "a", StartsAt: "2026-11-01T11:00:00Z", EndsAt: "2026-11-01T10:00:00Z"}}}}, ErrSuggestionInvalid},
		{"postback too long", Content{Text: "Oi", Suggestions: []Suggestion{{Kind: SuggestReply, Label: "x", Postback: strings.Repeat("p", MaxPostbackBytes+1)}}}, ErrSuggestionInvalid},
		{"media over plain http", Content{Media: &Media{Kind: MediaImage, URL: "http://a.example.com/a.jpg", MIMEType: "image/jpeg"}}, ErrMediaInvalid},
		{"media type not allowed for its kind", Content{Media: &Media{Kind: MediaImage, URL: "https://a.example.com/a.pdf", MIMEType: "application/pdf"}}, ErrMediaInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.msg.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVariableSlotsAreCountedAcrossEveryTextField(t *testing.T) {
	c := Content{Cards: []Card{
		{Title: "Olá {{1}}", Description: "Pedido {{2}}", Media: image()},
		{Title: "Frete", Description: "Até {{3}}", Media: image()},
	}}
	if got := c.VariableSlots(); got != 3 {
		t.Fatalf("VariableSlots() = %d, want 3", got)
	}
}

func TestRenderFillsVariablesWithoutTouchingTheTemplate(t *testing.T) {
	c := Content{
		Text:        "Oi {{1}}, pedido {{2}}",
		Suggestions: []Suggestion{{Kind: SuggestOpenURL, Label: "Ver {{2}}", URL: "https://loja.example.com/p/{{2}}"}},
	}
	got := c.Render([]string{"Ana", "123"})
	if got.Text != "Oi Ana, pedido 123" {
		t.Fatalf("Text = %q", got.Text)
	}
	if got.Suggestions[0].Label != "Ver 123" || got.Suggestions[0].URL != "https://loja.example.com/p/123" {
		t.Fatalf("Suggestion = %+v", got.Suggestions[0])
	}
	if c.Text != "Oi {{1}}, pedido {{2}}" || c.Suggestions[0].Label != "Ver {{2}}" {
		t.Fatal("Render changed the template")
	}
}

func TestRenderedLengthDecidesTheBillingClass(t *testing.T) {
	c := Content{Text: strings.Repeat("a", 150) + "{{1}}"}
	if c.Render([]string{"bc"}).BillingClass() != ClassBasic {
		t.Fatal("short name should stay Basic")
	}
	if c.Render([]string{strings.Repeat("z", 20)}).BillingClass() != ClassSingle {
		t.Fatal("long name should become Single")
	}
}

func TestPreviewTextSummarizesRichContentForTheInbox(t *testing.T) {
	cases := []struct {
		msg  Content
		want string
	}{
		{Content{Text: "Oi"}, "Oi"},
		{Content{Cards: []Card{{Title: "Tênis", Description: "R$ 99", Media: image()}}}, "Tênis\nR$ 99"},
		{Content{Media: image()}, ""},
	}
	for _, tc := range cases {
		if got := tc.msg.PreviewText(); got != tc.want {
			t.Fatalf("PreviewText() = %q, want %q", got, tc.want)
		}
	}
}

func TestMediaURLsListsTheMessageAndEveryCardFile(t *testing.T) {
	c := Content{
		Media: &Media{URL: "https://a/1.jpg"},
		Cards: []Card{{Media: &Media{URL: "https://a/2.jpg"}}, {Title: "sem mídia"}, {Media: &Media{URL: "https://a/3.mp4"}}},
	}
	got := c.MediaURLs()
	if len(got) != 3 || got[0] != "https://a/1.jpg" || got[1] != "https://a/2.jpg" || got[2] != "https://a/3.mp4" {
		t.Fatalf("MediaURLs = %v", got)
	}
	if len((Content{Text: "oi"}).MediaURLs()) != 0 {
		t.Fatal("a text message carries no file")
	}
}
