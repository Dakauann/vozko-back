package sms

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf16"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const MaxSegments = 8

var (
	ErrTextEmpty   = errors.New("sms: the message has no text")
	ErrTextTooLong = errors.New("sms: the message is longer than 8 parts")
)

type Encoding string

const (
	GSM7 Encoding = "gsm7"
	UCS2 Encoding = "ucs2"
)

const gsmBasic = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"

const gsmExtension = "^{}\\[~]|€\f"

var limits = map[Encoding]struct{ single, multi int }{
	GSM7: {single: 160, multi: 153},
	UCS2: {single: 70, multi: 67},
}

func isGSM(r rune) bool {
	return strings.ContainsRune(gsmBasic, r) || strings.ContainsRune(gsmExtension, r)
}

func EncodingOf(text string) Encoding {
	for _, r := range text {
		if !isGSM(r) {
			return UCS2
		}
	}
	return GSM7
}

type Measurement struct {
	Encoding   Encoding `json:"encoding"`
	Units      int      `json:"units"`
	Segments   int      `json:"segments"`
	PerSegment int      `json:"per_segment"`
	Remaining  int      `json:"remaining"`
}

func Measure(text string) Measurement {
	encoding := EncodingOf(text)
	units := 0
	for _, r := range text {
		switch {
		case encoding == UCS2:
			units += len(utf16.Encode([]rune{r}))
		case strings.ContainsRune(gsmExtension, r):
			units += 2
		default:
			units++
		}
	}
	limit := limits[encoding]
	m := Measurement{Encoding: encoding, Units: units, PerSegment: limit.single}
	switch {
	case units == 0:
		m.Remaining = limit.single
	case units <= limit.single:
		m.Segments = 1
		m.Remaining = limit.single - units
	default:
		m.PerSegment = limit.multi
		m.Segments = (units + limit.multi - 1) / limit.multi
		m.Remaining = m.Segments*limit.multi - units
	}
	return m
}

func Validate(text string) error {
	if strings.TrimSpace(text) == "" {
		return ErrTextEmpty
	}
	if Measure(text).Segments > MaxSegments {
		return ErrTextTooLong
	}
	return nil
}

func ToGSM(text string) string {
	var b strings.Builder
	for _, r := range text {
		if isGSM(r) {
			b.WriteRune(r)
			continue
		}
		folded, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), string(r))
		if err != nil {
			continue
		}
		for _, f := range folded {
			if isGSM(f) {
				b.WriteRune(f)
			}
		}
	}
	return b.String()
}
