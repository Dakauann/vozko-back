package audience

import (
	"strings"
	"unicode"
)

// What a conversation was ABOUT, in a form that can be counted.
//
// The model already answers with the concrete product, service or topic the
// customer showed interest in. That text is fine to SHOW and useless to COUNT:
// the same subject comes back as "Plano Família", "plano familia" and "Plano
// família!", which a reader sees as one thing and a GROUP BY sees as three.
//
// So the row stores both. ProductInterest is what the model wrote, shown as it
// was written; ProductInterestKey is this canonical form, and it is the only
// thing aggregated. Deriving the key here rather than asking the model for it
// is the same rule the rest of this engine follows: models label consistently
// and normalise inconsistently.

const (
	// MaxSubjectWords caps a subject at a label rather than a phrase.
	//
	// Enforced here, not trusted from the schema: a description is a request,
	// and a model that answers with a sentence would otherwise put a sentence
	// on a chart axis. Three words fits "clareamento dental laser" and refuses
	// "cliente quer saber sobre o clareamento dental".
	MaxSubjectWords = 3

	// MaxSubjectKeyRunes bounds the stored key so it can carry an index.
	MaxSubjectKeyRunes = 60

	// MaxRankedSubjects bounds the ranking a slice returns.
	//
	// A long tail is the normal shape here: a busy workspace has a handful of
	// subjects that repeat and hundreds that happened once. Returning all of
	// them would send a payload that is mostly noise to draw a chart that can
	// legibly hold about a dozen arcs.
	MaxRankedSubjects = 12
)

// nonSubjects are answers that mean "there wasn't one". A model asked for a
// subject will sometimes say so in words instead of returning empty, and
// counting those would put a bar labelled "none" above every real subject.
var nonSubjects = map[string]bool{
	"n a": true, "na": true, "nenhum": true, "nenhuma": true, "none": true,
	"nao informado": true, "nao identificado": true, "indefinido": true,
	"desconhecido": true, "unknown": true, "outro": true, "other": true,
	"sem assunto": true, "nao se aplica": true, "n d": true,
}

// SubjectKey canonicalises a free-text subject into the form repeats collide
// on: lowercase, accents folded, punctuation dropped, whitespace collapsed,
// and cut to MaxSubjectWords.
//
// Returns empty for anything that should not be counted, which the callers
// treat as "this conversation had no clear subject" rather than as a subject
// named "-".
func SubjectKey(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	// One pass: fold each rune, and turn every run of anything else into a
	// single space. Splitting on whitespace first would keep "plano,familia"
	// as one word and "plano, familia" as two.
	lastWasSpace := true
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		folded := foldRune(r)
		if folded == 0 {
			if !lastWasSpace {
				b.WriteRune(' ')
				lastWasSpace = true
			}
			continue
		}
		b.WriteRune(folded)
		lastWasSpace = false
	}

	words := strings.Fields(b.String())
	if len(words) > MaxSubjectWords {
		words = words[:MaxSubjectWords]
	}
	key := strings.Join(words, " ")
	if nonSubjects[key] {
		return ""
	}
	key, _ = TruncateRunes(key, MaxSubjectKeyRunes)
	return strings.TrimSpace(key)
}

// foldRune maps one rune to its counting form, or 0 for "this is a separator".
//
// Only Latin-1 accents are folded, deliberately: this product's conversations
// are Portuguese, Spanish and English, and a general Unicode decomposition
// would pull in a dependency to solve a problem these languages do not have.
// An unfolded script (a customer writing in Arabic, say) still counts, it just
// counts on its own exact characters, which is correct if less forgiving.
func foldRune(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return r
	case strings.ContainsRune("áàâãä", r):
		return 'a'
	case strings.ContainsRune("éèêë", r):
		return 'e'
	case strings.ContainsRune("íìîï", r):
		return 'i'
	case strings.ContainsRune("óòôõö", r):
		return 'o'
	case strings.ContainsRune("úùûü", r):
		return 'u'
	case r == 'ç':
		return 'c'
	case r == 'ñ':
		return 'n'
	case unicode.IsLetter(r) || unicode.IsDigit(r):
		// A script this fold does not know. Kept as itself rather than
		// discarded, so it is still counted.
		return r
	}
	return 0
}

// SubjectCount is one row of "what are people talking about": the countable
// key, a label to print, and how many analyses carried it.
type SubjectCount struct {
	// Key is what the rows were grouped on. It is the identity, so a chart
	// keys its marks on this and not on the label.
	Key string `json:"key"`
	// Label is a real example of how the subject was written, so the chart
	// shows "Plano Família" rather than the stripped "plano familia".
	Label string `json:"label"`
	Count int    `json:"count"`
}
