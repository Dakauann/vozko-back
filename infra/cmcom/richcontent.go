package cmcom

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"vozko/domain/rcs"
)

const (
	actionReply        = "Reply"
	actionOpenURL      = "OpenUrl"
	actionDial         = "Dial"
	actionViewLocation = "ViewLocation"
	actionCalendar     = "CreateCalendarEvent"
	carouselCardWidth  = "Medium"
	webviewApplication = "WEBVIEW"
	webviewModeFull    = "full"
)

var ErrContentUnsupported = errors.New("cmcom: the message content cannot be sent")

type wireRichContent struct {
	Conversation []wireRichMessage `json:"conversation"`
	Suggestions  []wireSuggestion  `json:"suggestions,omitempty"`
}

type wireRichMessage struct {
	Header      string           `json:"header,omitempty"`
	Text        string           `json:"text,omitempty"`
	Media       *wireMedia       `json:"media,omitempty"`
	Carousel    *wireCarousel    `json:"carousel,omitempty"`
	Suggestions []wireSuggestion `json:"suggestions,omitempty"`
}

type wireMedia struct {
	MediaName string `json:"mediaName"`
	MediaURI  string `json:"mediaUri"`
	MIMEType  string `json:"mimeType"`
}

type wireCarousel struct {
	CardWidth string            `json:"cardWidth"`
	Cards     []wireRichMessage `json:"cards"`
}

type wireSuggestion struct {
	Action            string             `json:"action"`
	Label             string             `json:"label"`
	PostbackData      string             `json:"postbackdata"`
	URL               string             `json:"url,omitempty"`
	OpenURLProperties *wireOpenURLConfig `json:"openUrlProperties,omitempty"`
	Dial              *wireDial          `json:"dial,omitempty"`
	ViewLocation      *wireLocation      `json:"viewLocation,omitempty"`
	Calendar          *wireCalendar      `json:"calendar,omitempty"`
}

type wireOpenURLConfig struct {
	Application string `json:"application"`
	WebviewMode string `json:"webviewMode"`
}

type wireDial struct {
	PhoneNumber string `json:"phoneNumber"`
}

type wireLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Label     string  `json:"label,omitempty"`
}

type wireCalendar struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
}

func richContentFor(c rcs.Content) (wireRichContent, error) {
	var out wireRichContent
	chips, err := wireSuggestions(c.Suggestions)
	if err != nil {
		return out, err
	}
	out.Suggestions = chips
	switch c.Kind() {
	case rcs.KindCarousel:
		cards := make([]wireRichMessage, len(c.Cards))
		for i, card := range c.Cards {
			if cards[i], err = wireCard(card); err != nil {
				return out, fmt.Errorf("card %d: %w", i+1, err)
			}
		}
		out.Conversation = []wireRichMessage{{Carousel: &wireCarousel{CardWidth: carouselCardWidth, Cards: cards}}}
	case rcs.KindRichCard:
		card, err := wireCard(c.Cards[0])
		if err != nil {
			return out, err
		}
		out.Conversation = []wireRichMessage{card}
	default:
		if strings.TrimSpace(c.Text) != "" {
			out.Conversation = append(out.Conversation, wireRichMessage{Text: c.Text})
		}
		if c.Media != nil {
			out.Conversation = append(out.Conversation, wireRichMessage{Media: wireMediaFor(*c.Media)})
		}
	}
	return out, nil
}

func wireCard(card rcs.Card) (wireRichMessage, error) {
	suggestions, err := wireSuggestions(card.Suggestions)
	if err != nil {
		return wireRichMessage{}, err
	}
	out := wireRichMessage{Header: card.Title, Text: card.Description, Suggestions: suggestions}
	if card.Media != nil {
		out.Media = wireMediaFor(*card.Media)
	}
	return out, nil
}

func wireMediaFor(m rcs.Media) *wireMedia {
	name := strings.TrimSpace(m.Name)
	if name == "" {
		name = mediaFileName(m.URL)
	}
	return &wireMedia{MediaName: name, MediaURI: m.URL, MIMEType: m.MIMEType}
}

func mediaFileName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if base := path.Base(u.Path); base != "." && base != "/" {
		return base
	}
	return u.Host
}

func wireSuggestions(list []rcs.Suggestion) ([]wireSuggestion, error) {
	if len(list) == 0 {
		return nil, nil
	}
	out := make([]wireSuggestion, len(list))
	for i, s := range list {
		w, err := wireSuggestionFor(s)
		if err != nil {
			return nil, fmt.Errorf("button %q: %w", s.Label, err)
		}
		out[i] = w
	}
	return out, nil
}

func wireSuggestionFor(s rcs.Suggestion) (wireSuggestion, error) {
	if strings.TrimSpace(s.Postback) == "" {
		return wireSuggestion{}, fmt.Errorf("%w: the button has no tracking postback", ErrContentUnsupported)
	}
	out := wireSuggestion{Label: s.Label, PostbackData: s.Postback}
	switch s.Kind {
	case rcs.SuggestReply:
		out.Action = actionReply
	case rcs.SuggestOpenURL:
		out.Action = actionOpenURL
		out.URL = s.URL
		if s.Webview {
			out.OpenURLProperties = &wireOpenURLConfig{Application: webviewApplication, WebviewMode: webviewModeFull}
		}
	case rcs.SuggestDial:
		if !dialable(s.Phone) {
			return wireSuggestion{}, fmt.Errorf("%w: a call button number may hold only digits, +, * and #", ErrContentUnsupported)
		}
		out.Action = actionDial
		out.Dial = &wireDial{PhoneNumber: strings.TrimSpace(s.Phone)}
	case rcs.SuggestViewLocation:
		out.Action = actionViewLocation
		out.ViewLocation = &wireLocation{Latitude: s.Latitude, Longitude: s.Longitude, Label: s.LocationLabel}
	case rcs.SuggestCalendar:
		calendar, err := wireCalendarFor(s.Calendar)
		if err != nil {
			return wireSuggestion{}, err
		}
		out.Action = actionCalendar
		out.Calendar = calendar
	default:
		return wireSuggestion{}, fmt.Errorf("%w: unknown button kind %q", ErrContentUnsupported, s.Kind)
	}
	return out, nil
}

func dialable(phone string) bool {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return false
	}
	for _, r := range phone {
		if !(r >= '0' && r <= '9' || r == '+' || r == '*' || r == '#') {
			return false
		}
	}
	return true
}

func wireCalendarFor(e *rcs.CalendarEvent) (*wireCalendar, error) {
	if e == nil {
		return nil, fmt.Errorf("%w: a calendar button needs an event", ErrContentUnsupported)
	}
	starts, err := time.Parse(time.RFC3339, e.StartsAt)
	if err != nil {
		return nil, fmt.Errorf("%w: event start %q", ErrContentUnsupported, e.StartsAt)
	}
	ends, err := time.Parse(time.RFC3339, e.EndsAt)
	if err != nil {
		return nil, fmt.Errorf("%w: event end %q", ErrContentUnsupported, e.EndsAt)
	}
	return &wireCalendar{
		Title:       e.Title,
		Description: e.Description,
		StartTime:   starts.UTC().Format(time.RFC3339),
		EndTime:     ends.UTC().Format(time.RFC3339),
	}, nil
}

func fallbackText(c rcs.Content) (string, error) {
	if preview := c.PreviewText(); strings.TrimSpace(preview) != "" {
		return preview, nil
	}
	if c.Media != nil {
		return c.Media.URL, nil
	}
	if len(c.Cards) > 0 && c.Cards[0].Media != nil {
		return c.Cards[0].Media.URL, nil
	}
	return "", fmt.Errorf("%w: nothing to use as the fallback text", ErrContentUnsupported)
}
