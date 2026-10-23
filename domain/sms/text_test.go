package sms

import (
	"errors"
	"strings"
	"testing"
)

func TestEncodingDetectsCharactersOutsideGSM(t *testing.T) {
	cases := []struct {
		text string
		want Encoding
	}{
		{"Seu pedido saiu para entrega", GSM7},
		{"Promoção de verão", UCS2},
		{"Café às 10h, é hoje", GSM7},
		{"Preco R$ 10 {cupom}", GSM7},
		{"Preço baixo", UCS2},
		{"Olá", UCS2},
		{"Emoji 🎉", UCS2},
	}
	for _, tc := range cases {
		if got := EncodingOf(tc.text); got != tc.want {
			t.Fatalf("EncodingOf(%q) = %s, want %s", tc.text, got, tc.want)
		}
	}
}

func TestSegments(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"gsm single", strings.Repeat("a", 160), 1},
		{"gsm two parts", strings.Repeat("a", 161), 2},
		{"gsm multipart boundary", strings.Repeat("a", 306), 2},
		{"gsm three parts", strings.Repeat("a", 307), 3},
		{"extension characters count twice", strings.Repeat("{", 80), 1},
		{"extension pushes over", strings.Repeat("{", 81), 2},
		{"ucs2 single", strings.Repeat("ã", 70), 1},
		{"ucs2 two parts", strings.Repeat("ã", 71), 2},
		{"ucs2 multipart boundary", strings.Repeat("ã", 134), 2},
		{"emoji counts as two ucs2 units", strings.Repeat("🎉", 35), 1},
		{"emoji over", strings.Repeat("🎉", 36), 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Measure(tc.text).Segments; got != tc.want {
				t.Fatalf("Segments = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMeasureReportsWhatTheComposerShows(t *testing.T) {
	m := Measure("Olá " + strings.Repeat("a", 66))
	if m.Encoding != UCS2 || m.Units != 70 || m.Segments != 1 || m.PerSegment != 70 || m.Remaining != 0 {
		t.Fatalf("Measure = %+v", m)
	}
	m = Measure(strings.Repeat("a", 200))
	if m.Encoding != GSM7 || m.Segments != 2 || m.PerSegment != 153 || m.Remaining != 106 {
		t.Fatalf("Measure = %+v", m)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("Oi"); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	if err := Validate("  "); !errors.Is(err, ErrTextEmpty) {
		t.Fatalf("Validate blank = %v", err)
	}
	if err := Validate(strings.Repeat("ã", 67*MaxSegments+1)); !errors.Is(err, ErrTextTooLong) {
		t.Fatalf("Validate long = %v", err)
	}
}

func TestToGSMRemovesAccentsSoTheMessageFitsMore(t *testing.T) {
	got := ToGSM("Promoção de verão, só até sábado! 🎉")
	if got != "Promocao de verao, so até sabado! " || EncodingOf(got) != GSM7 {
		t.Fatalf("ToGSM = %q", got)
	}
	if ToGSM("Café é bom") != "Café é bom" {
		t.Fatal("characters GSM already has stay as they are")
	}
}
