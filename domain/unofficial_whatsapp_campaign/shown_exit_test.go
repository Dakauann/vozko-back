package unofficial_whatsapp_campaign

import (
	"errors"
	"strings"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

func TestEveryVariantMustShowAnExitCommand(t *testing.T) {
	cases := []struct {
		name string
		spec MessageSpec
		ok   bool
	}{
		{"one text showing /sair", MessageSpec{Kind: KindText, Bodies: []string{"Oi {{1}}! Responda /sair para não receber mais."}}, true},
		{"every variant shows one", MessageSpec{Kind: KindText, Bodies: []string{"Oi /sair", "Olá, envie /parar"}}, true},
		{"an image caption showing one", MessageSpec{Kind: KindImage, MediaID: "m-1", Bodies: []string{"Promo! /sair"}}, true},
		{"a menu text showing one", MessageSpec{Kind: KindMenu, Bodies: []string{"Escolha. /sair para sair"}}, true},
		{"one variant without it", MessageSpec{Kind: KindText, Bodies: []string{"Oi /sair", "Olá"}}, false},
		{"no command at all", MessageSpec{Kind: KindText, Bodies: []string{"bom dia"}}, false},
		{"a link path", MessageSpec{Kind: KindText, Bodies: []string{"veja https://loja.com/sair"}}, false},
		{"a date", MessageSpec{Kind: KindText, Bodies: []string{"até 12/10"}}, false},
		{"an image without a caption", MessageSpec{Kind: KindImage, MediaID: "m-1"}, false},
		{"a menu showing it only in the footer", MessageSpec{Kind: KindMenu, Bodies: []string{"Escolha"}, Footer: "Responda /sair"}, false},
		{"an audio followed by an exit text", MessageSpec{Kind: KindAudio, MediaID: "m-1", ExitText: "Responda /sair para não receber mais"}, true},
		{"an audio whose caption alone shows it", MessageSpec{Kind: KindAudio, MediaID: "m-1", Bodies: []string{"Responda /sair"}}, false},
		{"an audio whose exit text shows no command", MessageSpec{Kind: KindAudio, MediaID: "m-1", ExitText: "Responda sair"}, false},
	}
	for _, tc := range cases {
		err := tc.spec.RequireShownExit()
		if tc.ok && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if !tc.ok && !errors.Is(err, ErrCampaignNeedsExit) {
			t.Errorf("%s: got %v, want ErrCampaignNeedsExit", tc.name, err)
		}
	}
}

func TestTheMissingExitHasItsCode(t *testing.T) {
	if got := ErrorCode(ErrCampaignNeedsExit); got != "unofficial_campaign_needs_exit" {
		t.Fatalf("code = %q", got)
	}
	if got := ErrorCode(ErrCampaignNameRequired); got != "" {
		t.Fatalf("an uncoded error got %q", got)
	}
}

func TestOnlyAnAudioCarriesAnExitText(t *testing.T) {
	audio := MessageSpec{Kind: KindAudio, MediaID: "m-1", ExitText: "  Responda /sair  "}
	audio.Normalize()
	if audio.ExitText != "Responda /sair" {
		t.Fatalf("audio exit text = %q", audio.ExitText)
	}
	text := MessageSpec{Kind: KindText, Bodies: []string{"Oi /sair"}, ExitText: "Responda /sair"}
	text.Normalize()
	if text.ExitText != "" {
		t.Fatalf("a text message kept an exit text: %q", text.ExitText)
	}
}

func TestAnAudioExitTextFitsInOneMessage(t *testing.T) {
	long := MessageSpec{Kind: KindAudio, MediaID: "m-1", ExitText: "/sair " + strings.Repeat("a", uw.MaxTextRunes)}
	if err := long.Validate(); !errors.Is(err, ErrMessageExitTextTooLong) {
		t.Fatalf("got %v, want ErrMessageExitTextTooLong", err)
	}
}

func TestAChangedExitTextIsAChangedMessage(t *testing.T) {
	before := MessageSpec{Kind: KindAudio, MediaID: "m-1", ExitText: "Responda /sair"}
	after := before
	after.ExitText = "Responda /parar"
	if before.SameAs(after) {
		t.Fatal("a new exit text counted as the same message")
	}
	if !before.SameAs(before) {
		t.Fatal("the same message differs from itself")
	}
}
