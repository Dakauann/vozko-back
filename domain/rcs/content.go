package rcs

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
)

const (
	MaxTextRunes            = 3072
	BasicTextRunes          = 160
	MaxMessageSuggestions   = 11
	MaxCardSuggestions      = 4
	MaxSuggestionLabelRunes = 25
	MaxPostbackBytes        = 2048
	MaxCardTitleRunes       = 200
	MaxCardDescriptionRunes = 2000
	MinCarouselCards        = 2
	MaxCarouselCards        = 10
	MaxMediaBytes           = 100 << 20
)

var (
	ErrContentEmpty       = errors.New("rcs: the message has nothing to send")
	ErrContentMixed       = errors.New("rcs: cards cannot be combined with text or a media file")
	ErrTextTooLong        = errors.New("rcs: the text is too long")
	ErrTooManySuggestions = errors.New("rcs: too many buttons")
	ErrTooManyCards       = errors.New("rcs: a carousel holds at most 10 cards")
	ErrCardEmpty          = errors.New("rcs: a card needs a title, a text or a media file")
	ErrCardTitleTooLong   = errors.New("rcs: the card title is too long")
	ErrCardTextTooLong    = errors.New("rcs: the card text is too long")
	ErrSuggestionInvalid  = errors.New("rcs: invalid button")
	ErrMediaInvalid       = errors.New("rcs: invalid media file")
)

type ContentKind string

const (
	KindText     ContentKind = "text"
	KindMedia    ContentKind = "media"
	KindRichCard ContentKind = "rich_card"
	KindCarousel ContentKind = "carousel"
)

type BillingClass string

const (
	ClassBasic  BillingClass = "basic"
	ClassSingle BillingClass = "single"
)

type MediaKind string

const (
	MediaImage MediaKind = "image"
	MediaVideo MediaKind = "video"
	MediaAudio MediaKind = "audio"
	MediaPDF   MediaKind = "pdf"
)

var mediaMIMETypes = map[MediaKind][]string{
	MediaImage: {"image/jpeg", "image/png", "image/gif"},
	MediaVideo: {"video/mp4", "video/mpeg", "video/webm", "video/h263", "video/m4v"},
	MediaAudio: {"audio/aac", "audio/mpeg", "audio/mp3", "audio/mp4", "audio/ogg", "audio/3gpp"},
	MediaPDF:   {"application/pdf"},
}

func MediaMIMETypes(kind MediaKind) []string {
	return append([]string(nil), mediaMIMETypes[kind]...)
}

func MediaKindFor(mimeType string) (MediaKind, bool) {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	for kind, types := range mediaMIMETypes {
		for _, t := range types {
			if t == mimeType {
				return kind, true
			}
		}
	}
	return "", false
}

type Media struct {
	Kind      MediaKind `json:"kind"`
	URL       string    `json:"url"`
	MIMEType  string    `json:"mime_type"`
	Name      string    `json:"name,omitempty"`
	SizeBytes int64     `json:"size_bytes,omitempty"`
}

func (m Media) validate() error {
	kind, ok := MediaKindFor(m.MIMEType)
	if !ok || kind != m.Kind {
		return fmt.Errorf("%w: %q is not a %s type", ErrMediaInvalid, m.MIMEType, m.Kind)
	}
	if !secureURL(m.URL) {
		return fmt.Errorf("%w: the file must be served over https", ErrMediaInvalid)
	}
	if m.SizeBytes < 0 || m.SizeBytes > MaxMediaBytes {
		return fmt.Errorf("%w: the file is too large", ErrMediaInvalid)
	}
	return nil
}

type SuggestionKind string

const (
	SuggestReply        SuggestionKind = "reply"
	SuggestOpenURL      SuggestionKind = "open_url"
	SuggestDial         SuggestionKind = "dial"
	SuggestViewLocation SuggestionKind = "view_location"
	SuggestCalendar     SuggestionKind = "calendar"
)

type CalendarEvent struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	StartsAt    string `json:"starts_at"`
	EndsAt      string `json:"ends_at"`
}

type Suggestion struct {
	Kind          SuggestionKind `json:"kind"`
	Label         string         `json:"label"`
	Postback      string         `json:"postback,omitempty"`
	URL           string         `json:"url,omitempty"`
	Webview       bool           `json:"webview,omitempty"`
	Phone         string         `json:"phone,omitempty"`
	Latitude      float64        `json:"latitude,omitempty"`
	Longitude     float64        `json:"longitude,omitempty"`
	LocationLabel string         `json:"location_label,omitempty"`
	Calendar      *CalendarEvent `json:"calendar,omitempty"`
}

func (s Suggestion) validate() error {
	label := strings.TrimSpace(s.Label)
	if label == "" || utf8.RuneCountInString(label) > MaxSuggestionLabelRunes {
		return fmt.Errorf("%w: the label must have 1 to %d characters", ErrSuggestionInvalid, MaxSuggestionLabelRunes)
	}
	if len(s.Postback) > MaxPostbackBytes {
		return fmt.Errorf("%w: the button data is too long", ErrSuggestionInvalid)
	}
	switch s.Kind {
	case SuggestReply:
		return nil
	case SuggestOpenURL:
		if !secureURL(s.URL) {
			return fmt.Errorf("%w: the link must be an https address", ErrSuggestionInvalid)
		}
		return nil
	case SuggestDial:
		if strings.TrimSpace(s.Phone) == "" {
			return fmt.Errorf("%w: a call button needs a phone number", ErrSuggestionInvalid)
		}
		return nil
	case SuggestViewLocation:
		if math.Abs(s.Latitude) > 90 || math.Abs(s.Longitude) > 180 || (s.Latitude == 0 && s.Longitude == 0) {
			return fmt.Errorf("%w: the location coordinates are out of range", ErrSuggestionInvalid)
		}
		return nil
	case SuggestCalendar:
		return s.Calendar.validate()
	}
	return fmt.Errorf("%w: unknown kind %q", ErrSuggestionInvalid, s.Kind)
}

func (e *CalendarEvent) validate() error {
	if e == nil || strings.TrimSpace(e.Title) == "" {
		return fmt.Errorf("%w: a calendar button needs an event title", ErrSuggestionInvalid)
	}
	starts, err1 := time.Parse(time.RFC3339, e.StartsAt)
	ends, err2 := time.Parse(time.RFC3339, e.EndsAt)
	if err1 != nil || err2 != nil || !ends.After(starts) {
		return fmt.Errorf("%w: the event must end after it starts", ErrSuggestionInvalid)
	}
	return nil
}

type Card struct {
	Media       *Media       `json:"media,omitempty"`
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
}

type Content struct {
	Text        string       `json:"text,omitempty"`
	Media       *Media       `json:"media,omitempty"`
	Cards       []Card       `json:"cards,omitempty"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
}

func (c Content) Kind() ContentKind {
	switch {
	case len(c.Cards) >= MinCarouselCards:
		return KindCarousel
	case len(c.Cards) == 1:
		return KindRichCard
	case c.Media != nil:
		return KindMedia
	}
	return KindText
}

func (c Content) BillingClass() BillingClass {
	if c.Kind() == KindText && len(c.Suggestions) == 0 && utf8.RuneCountInString(c.Text) <= BasicTextRunes {
		return ClassBasic
	}
	return ClassSingle
}

func (c Content) Validate() error {
	issues := c.Issues()
	if len(issues) == 0 {
		return nil
	}
	if issues[0].Path == "" {
		return issues[0].Err
	}
	return fmt.Errorf("%s: %w", issues[0].Path, issues[0].Err)
}

func secureURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Scheme == "https" && u.Host != ""
}

func (c Content) texts() []string {
	out := []string{c.Text}
	out = append(out, suggestionTexts(c.Suggestions)...)
	for _, card := range c.Cards {
		out = append(out, card.Title, card.Description)
		out = append(out, suggestionTexts(card.Suggestions)...)
	}
	return out
}

func suggestionTexts(suggestions []Suggestion) []string {
	out := make([]string, 0, len(suggestions)*2)
	for _, s := range suggestions {
		out = append(out, s.Label, s.URL)
	}
	return out
}

func (c Content) VariableSlots() int {
	return shared.HighestPositionalParameter(c.texts())
}

func (c Content) Render(vars []string) Content {
	fill := func(s string) string { return shared.RenderPositional(s, vars) }
	out := Content{Text: fill(c.Text), Media: c.Media, Suggestions: renderSuggestions(c.Suggestions, fill)}
	if len(c.Cards) > 0 {
		out.Cards = make([]Card, len(c.Cards))
		for i, card := range c.Cards {
			out.Cards[i] = Card{
				Media:       card.Media,
				Title:       fill(card.Title),
				Description: fill(card.Description),
				Suggestions: renderSuggestions(card.Suggestions, fill),
			}
		}
	}
	return out
}

func renderSuggestions(suggestions []Suggestion, fill func(string) string) []Suggestion {
	if len(suggestions) == 0 {
		return nil
	}
	out := make([]Suggestion, len(suggestions))
	for i, s := range suggestions {
		s.Label = fill(s.Label)
		s.URL = fill(s.URL)
		out[i] = s
	}
	return out
}

func (c Content) PreviewText() string {
	if c.Text != "" || len(c.Cards) == 0 {
		return c.Text
	}
	first := c.Cards[0]
	return strings.TrimSpace(first.Title + "\n" + first.Description)
}

func (c Content) MediaURLs() []string {
	var urls []string
	if c.Media != nil {
		urls = append(urls, c.Media.URL)
	}
	for _, card := range c.Cards {
		if card.Media != nil {
			urls = append(urls, card.Media.URL)
		}
	}
	return urls
}
