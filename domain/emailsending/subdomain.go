package emailsending

import (
	"fmt"
	"strings"

	"vozko/domain/emailaddress"
)

const maxSlugLength = 30

var reservedSlugs = map[string]struct{}{
	"www":        {},
	"mail":       {},
	"bounce":     {},
	"reply":      {},
	"admin":      {},
	"api":        {},
	"smtp":       {},
	"imap":       {},
	"pop":        {},
	"mx":         {},
	"dmarc":      {},
	"postmaster": {},
	"abuse":      {},
	"support":    {},
}

var foldedRunes = map[rune]string{
	'á': "a", 'à': "a", 'â': "a", 'ã': "a", 'ä': "a",
	'é': "e", 'è': "e", 'ê': "e", 'ë': "e",
	'í': "i", 'ì': "i", 'î': "i", 'ï': "i",
	'ó': "o", 'ò': "o", 'ô': "o", 'õ': "o", 'ö': "o",
	'ú': "u", 'ù': "u", 'û': "u", 'ü': "u",
	'ç': "c", 'ñ': "n",
}

func SlugCandidate(name string) string {
	var b strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(name) {
		if folded, ok := foldedRunes[r]; ok {
			b.WriteString(folded)
			lastHyphen = false
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > maxSlugLength {
		slug = strings.TrimRight(slug[:maxSlugLength], "-")
	}
	if _, reserved := reservedSlugs[slug]; reserved {
		return ""
	}
	return slug
}

func ValidateChosenSlug(slug string) error {
	if _, reserved := reservedSlugs[slug]; reserved {
		return ErrSlugReserved
	}
	if slug == "" || SlugCandidate(slug) != slug {
		return ErrInvalidSlug
	}
	return nil
}

func SlugFor(name, workspaceID string, taken func(string) bool) string {
	base := SlugCandidate(name)
	if base == "" {
		base = fallbackSlug(workspaceID)
	}
	candidate := base
	for n := 2; taken(candidate); n++ {
		suffix := fmt.Sprintf("-%d", n)
		trimmed := base
		if len(trimmed)+len(suffix) > maxSlugLength {
			trimmed = strings.TrimRight(trimmed[:maxSlugLength-len(suffix)], "-")
		}
		candidate = trimmed + suffix
	}
	return candidate
}

func fallbackSlug(workspaceID string) string {
	id := SlugCandidate(workspaceID)
	if i := strings.IndexByte(id, '-'); i > 0 {
		id = id[:i]
	}
	return "ws-" + id
}

func SharedSubdomain(slug, sharedDomain string) (string, error) {
	shared, err := emailaddress.NormalizeDomain(sharedDomain)
	if err != nil {
		return "", ErrInvalidSharedDomain
	}
	if slug == "" || SlugCandidate(slug) != slug {
		return "", ErrInvalidSlug
	}
	full, err := emailaddress.NormalizeDomain(slug + "." + shared)
	if err != nil {
		return "", ErrInvalidSlug
	}
	return full, nil
}
