package facebook

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidProfile     = errors.New("messenger profile is invalid")
	ErrProfileRateLimited = errors.New("messenger profile changes are limited to 10 every 10 minutes per page")
)

const (
	MaxIceBreakers         = 4
	MaxMenuItems           = 20
	MaxMenuTitleRunes      = 30
	MaxGreetingRunes       = 160
	MaxProfilePayloadBytes = 1000
)

type MenuItemType string

const (
	MenuPostback MenuItemType = "postback"
	MenuWebURL   MenuItemType = "web_url"
)

type LocalizedText struct {
	Locale string
	Text   string
}

type GetStarted struct {
	Payload string
}

type IceBreaker struct {
	Question string
	Payload  string
}

type IceBreakerSet struct {
	Locale string
	Items  []IceBreaker
}

type MenuItem struct {
	Type    MenuItemType
	Title   string
	Payload string
	URL     string
}

type PersistentMenu struct {
	Locale                string
	ComposerInputDisabled bool
	Items                 []MenuItem
}

type MessengerProfile struct {
	Greeting       []LocalizedText
	GetStarted     *GetStarted
	IceBreakers    []IceBreakerSet
	PersistentMenu []PersistentMenu
}

func (p MessengerProfile) Validate() error {
	if len(p.PersistentMenu) > 0 && p.GetStarted == nil {
		return invalidProfile("a persistent menu needs a Get Started button")
	}
	if p.GetStarted != nil && strings.TrimSpace(p.GetStarted.Payload) == "" {
		return invalidProfile("Get Started needs a payload")
	}
	for _, g := range p.Greeting {
		if g.Locale == "" {
			return invalidProfile("every greeting needs a locale")
		}
		if utf8.RuneCountInString(g.Text) > MaxGreetingRunes {
			return invalidProfile("a greeting is limited to %d characters", MaxGreetingRunes)
		}
	}
	for _, set := range p.IceBreakers {
		if set.Locale == "" {
			return invalidProfile("every ice breaker set needs a locale")
		}
		if len(set.Items) > MaxIceBreakers {
			return invalidProfile("up to %d ice breakers per locale", MaxIceBreakers)
		}
		for _, item := range set.Items {
			if strings.TrimSpace(item.Question) == "" || strings.TrimSpace(item.Payload) == "" || len(item.Payload) > MaxProfilePayloadBytes {
				return invalidProfile("every ice breaker needs a question and a payload")
			}
		}
	}
	for _, menu := range p.PersistentMenu {
		if err := menu.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (m PersistentMenu) validate() error {
	if m.Locale == "" {
		return invalidProfile("every menu needs a locale")
	}
	if len(m.Items) > MaxMenuItems {
		return invalidProfile("up to %d menu items", MaxMenuItems)
	}
	for _, item := range m.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" || utf8.RuneCountInString(title) > MaxMenuTitleRunes {
			return invalidProfile("menu titles take 1 to %d characters", MaxMenuTitleRunes)
		}
		switch item.Type {
		case MenuPostback:
			if strings.TrimSpace(item.Payload) == "" || len(item.Payload) > MaxProfilePayloadBytes {
				return invalidProfile("the menu item %q needs a payload", title)
			}
		case MenuWebURL:
			if err := secureURL(item.URL); err != nil {
				return invalidProfile("the menu link %q %v", title, err)
			}
		default:
			return invalidProfile("unknown menu item type %q", item.Type)
		}
	}
	return nil
}

func (p MessengerProfile) HasContent() bool {
	return len(p.Greeting) > 0 || p.GetStarted != nil || len(p.IceBreakers) > 0 || len(p.PersistentMenu) > 0
}

func (p MessengerProfile) ClearedFields() []string {
	var out []string
	if len(p.Greeting) == 0 {
		out = append(out, "greeting")
	}
	if p.GetStarted == nil {
		out = append(out, "get_started")
	}
	if len(p.IceBreakers) == 0 {
		out = append(out, "ice_breakers")
	}
	if len(p.PersistentMenu) == 0 {
		out = append(out, "persistent_menu")
	}
	return out
}

func invalidProfile(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidProfile, fmt.Sprintf(format, args...))
}
