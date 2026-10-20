package brand

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Gender string

const (
	Feminine  Gender = "feminine"
	Masculine Gender = "masculine"
)

var ErrUnknownGender = errors.New("brand gender must be feminine or masculine")

func ParseGender(raw string) (Gender, error) {
	switch g := Gender(strings.ToLower(strings.TrimSpace(raw))); g {
	case Feminine, Masculine:
		return g, nil
	default:
		return "", fmt.Errorf("%w, got %q", ErrUnknownGender, raw)
	}
}

type Phrases struct {
	TheBrand string
	OfBrand  string
	InBrand  string
	ToBrand  string
	ByBrand  string
	ABrand   string
}

type articleForms struct {
	the, of, in, to, by, a string
}

var portugueseForms = map[Gender]articleForms{
	Feminine:  {the: "a ", of: "da ", in: "na ", to: "à ", by: "pela ", a: "uma "},
	Masculine: {the: "o ", of: "do ", in: "no ", to: "ao ", by: "pelo ", a: "um "},
}

var portugueseWithoutArticle = articleForms{of: "de ", in: "em ", to: "a ", by: "por "}

var spanishForms = map[Gender]articleForms{
	Feminine:  {the: "la ", of: "de la ", in: "en la ", to: "a la ", by: "por la ", a: "una "},
	Masculine: {the: "el ", of: "del ", in: "en el ", to: "al ", by: "por el ", a: "un "},
}

var spanishWithoutArticle = articleForms{of: "de ", in: "en ", to: "a ", by: "por "}

func PortuguesePhrases(name string, g Gender) Phrases {
	return formsFor(portugueseForms, portugueseWithoutArticle, g).phrases(name)
}

func SpanishPhrases(name string, g Gender) Phrases {
	return formsFor(spanishForms, spanishWithoutArticle, g).phrases(name)
}

func (b Brand) Portuguese() Phrases {
	return PortuguesePhrases(b.Name, b.Gender)
}

func (b Brand) Spanish() Phrases {
	return SpanishPhrases(b.Name, b.Gender)
}

func (p Phrases) Capitalized() Phrases {
	return Phrases{
		TheBrand: capitalize(p.TheBrand),
		OfBrand:  capitalize(p.OfBrand),
		InBrand:  capitalize(p.InBrand),
		ToBrand:  capitalize(p.ToBrand),
		ByBrand:  capitalize(p.ByBrand),
		ABrand:   capitalize(p.ABrand),
	}
}

func formsFor(byGender map[Gender]articleForms, withoutArticle articleForms, g Gender) articleForms {
	if forms, known := byGender[g]; known {
		return forms
	}
	return withoutArticle
}

func (f articleForms) phrases(name string) Phrases {
	return Phrases{
		TheBrand: f.the + name,
		OfBrand:  f.of + name,
		InBrand:  f.in + name,
		ToBrand:  f.to + name,
		ByBrand:  f.by + name,
		ABrand:   f.a + name,
	}
}

func capitalize(s string) string {
	first, size := utf8.DecodeRuneInString(s)
	if first == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(first)) + s[size:]
}
