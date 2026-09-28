package facebook

import (
	"errors"
	"strings"
	"testing"
)

func validProfile() MessengerProfile {
	return MessengerProfile{
		Greeting:   []LocalizedText{{Locale: "default", Text: "Olá! Como podemos ajudar?"}},
		GetStarted: &GetStarted{Payload: "GET_STARTED"},
		IceBreakers: []IceBreakerSet{{Locale: "default", Items: []IceBreaker{
			{Question: "Qual o horário?", Payload: "HOURS"},
		}}},
		PersistentMenu: []PersistentMenu{{Locale: "default", Items: []MenuItem{
			{Type: MenuPostback, Title: "Falar com vendas", Payload: "SALES"},
			{Type: MenuWebURL, Title: "Site", URL: "https://loja.example"},
		}}},
	}
}

func TestAValidProfilePasses(t *testing.T) {
	if err := validProfile().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (MessengerProfile{}).Validate(); err != nil {
		t.Fatalf("an empty profile clears everything: %v", err)
	}
}

func TestProfileLimits(t *testing.T) {
	cases := map[string]func(*MessengerProfile){
		"menu without get started": func(p *MessengerProfile) { p.GetStarted = nil },
		"five ice breakers": func(p *MessengerProfile) {
			for len(p.IceBreakers[0].Items) < 5 {
				p.IceBreakers[0].Items = append(p.IceBreakers[0].Items, IceBreaker{Question: "q", Payload: "p"})
			}
		},
		"twenty one menu items": func(p *MessengerProfile) {
			for len(p.PersistentMenu[0].Items) < 21 {
				p.PersistentMenu[0].Items = append(p.PersistentMenu[0].Items, MenuItem{Type: MenuPostback, Title: "x", Payload: "y"})
			}
		},
		"long menu title":       func(p *MessengerProfile) { p.PersistentMenu[0].Items[0].Title = strings.Repeat("a", 31) },
		"menu link over http":   func(p *MessengerProfile) { p.PersistentMenu[0].Items[1].URL = "http://loja.example" },
		"postback without data": func(p *MessengerProfile) { p.PersistentMenu[0].Items[0].Payload = "" },
		"unknown item type":     func(p *MessengerProfile) { p.PersistentMenu[0].Items[0].Type = "phone_number" },
		"long greeting":         func(p *MessengerProfile) { p.Greeting[0].Text = strings.Repeat("a", 161) },
		"question without data": func(p *MessengerProfile) { p.IceBreakers[0].Items[0].Payload = "" },
		"missing locale":        func(p *MessengerProfile) { p.Greeting[0].Locale = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := validProfile()
			mutate(&p)
			if err := p.Validate(); !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestAnEmptyProfileHasNoContent(t *testing.T) {
	if (MessengerProfile{}).HasContent() || !validProfile().HasContent() {
		t.Fatal("content is any field being present")
	}
}

func TestClearedFieldsAreListedForDeletion(t *testing.T) {
	p := validProfile()
	p.IceBreakers, p.PersistentMenu = nil, nil
	got := p.ClearedFields()
	if len(got) != 2 || got[0] != "ice_breakers" || got[1] != "persistent_menu" {
		t.Fatalf("cleared = %v", got)
	}
}
