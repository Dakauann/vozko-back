package emailhygiene

import (
	"strings"
	"sync"

	"vozko/domain/emailaddress"
)

const (
	typoAlphabet     = "abcdefghijklmnopqrstuvwxyz0123456789-."
	shortLabelLength = 3
)

var realLookAlikes = []string{"email.com", "mail.com", "gmx.com", "gmx.net", "hotmail.fr", "hotmail.es", "hotmail.it", "live.fr", "terra.com", "uol.com", "bol.com"}

type TypoIndex struct {
	variants map[string]string
}

var defaultTypos = sync.OnceValue(func() *TypoIndex {
	return NewTypoIndex(emailaddress.FreeMailboxDomains(), realLookAlikes)
})

func DefaultTypos() *TypoIndex {
	return defaultTypos()
}

func NewTypoIndex(targets, real []string) *TypoIndex {
	known := map[string]bool{}
	for _, d := range append(append([]string(nil), targets...), real...) {
		known[strings.ToLower(d)] = true
	}
	variants := map[string]string{}
	ambiguous := map[string]bool{}
	for _, target := range targets {
		target = strings.ToLower(target)
		for _, v := range editsOf(target) {
			if known[v] || ambiguous[v] {
				continue
			}
			if prior, ok := variants[v]; ok && prior != target {
				delete(variants, v)
				ambiguous[v] = true
				continue
			}
			variants[v] = target
		}
	}
	return &TypoIndex{variants: variants}
}

func (t *TypoIndex) Suggest(domain string) (string, bool) {
	if t == nil {
		return "", false
	}
	target, ok := t.variants[strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")]
	return target, ok
}

func editsOf(target string) []string {
	from := 0
	if label := strings.IndexByte(target, '.'); label >= 0 && label <= shortLabelLength {
		from = label
	}
	var out []string
	for i := from; i <= len(target); i++ {
		for _, c := range typoAlphabet {
			out = append(out, target[:i]+string(c)+target[i:])
		}
		if i == len(target) {
			break
		}
		out = append(out, target[:i]+target[i+1:])
		for _, c := range typoAlphabet {
			if byte(c) != target[i] {
				out = append(out, target[:i]+string(c)+target[i+1:])
			}
		}
		if i+1 < len(target) && target[i] != target[i+1] {
			out = append(out, target[:i]+string(target[i+1])+string(target[i])+target[i+2:])
		}
	}
	return out
}

func SuggestedAddress(address emailaddress.Address, domain string) (emailaddress.Address, bool) {
	if strings.TrimSpace(domain) == "" {
		return "", false
	}
	fixed, err := emailaddress.Normalize(address.LocalPart() + "@" + domain)
	if err != nil {
		return "", false
	}
	return fixed, true
}
