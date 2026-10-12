package captions

import (
	"strings"
	"unicode"
)

type Grammar struct {
	openers       set
	strongBinders set
	softBinders   set
	abbreviations set
}

type set map[string]struct{}

func setOf(groups ...[]string) set {
	out := set{}
	for _, group := range groups {
		for _, word := range group {
			out[word] = struct{}{}
		}
	}
	return out
}

var (
	portugueseConjunctions  = []string{"e", "mas", "ou", "porque", "pois", "que", "quando", "se", "como", "porém", "então", "enquanto", "embora", "nem", "onde", "caso"}
	portuguesePrepositions  = []string{"a", "de", "em", "por", "para", "pra", "pro", "com", "sem", "sob", "sobre", "até", "entre", "após", "desde", "contra", "perante"}
	portugueseContractions  = []string{"do", "da", "dos", "das", "no", "na", "nos", "nas", "ao", "aos", "à", "às", "pelo", "pela", "pelos", "pelas", "num", "numa", "dum", "duma", "pros", "pras", "neste", "nesta", "nesse", "nessa", "naquele", "naquela", "deste", "desta", "desse", "dessa", "daquele", "daquela"}
	portugueseArticles      = []string{"o", "os", "as", "um", "uma", "uns", "umas"}
	portugueseDeterminers   = []string{"meu", "minha", "meus", "minhas", "teu", "tua", "teus", "tuas", "seu", "sua", "seus", "suas", "nosso", "nossa", "nossos", "nossas", "este", "esta", "estes", "estas", "esse", "essa", "esses", "essas", "aquele", "aquela", "aqueles", "aquelas"}
	portugueseVerbCompanion = []string{"não", "me", "te", "lhe", "lhes"}
	portugueseSubjects      = []string{"eu", "ele", "ela", "eles", "elas", "nós"}
	portugueseCopulas       = []string{"é", "são", "era", "eram", "foi", "foram"}
	portugueseQuantifiers   = []string{"todo", "toda", "todos", "todas", "cada", "qualquer", "algum", "alguma", "alguns", "algumas", "outro", "outra", "outros", "outras", "vários", "várias"}
	portugueseAuxiliaries   = []string{"vou", "vai", "vamos", "vão", "ia", "iam", "estou", "está", "estamos", "estão", "estava", "estavam", "tenho", "tem", "temos", "têm", "tinha", "tinham", "posso", "pode", "podemos", "podem", "quero", "quer", "queremos", "querem", "devo", "deve", "devemos", "devem", "preciso", "precisa", "precisamos", "precisam"}
	portugueseAbbreviations = []string{"sr", "sra", "srta", "dr", "dra", "prof", "profa", "av"}
)

var Portuguese = Grammar{
	openers:       setOf(portugueseConjunctions, portuguesePrepositions, portugueseContractions, portugueseArticles, portugueseDeterminers),
	strongBinders: setOf(portuguesePrepositions, portugueseContractions, portugueseArticles, portugueseDeterminers, portugueseVerbCompanion),
	softBinders:   setOf(portugueseConjunctions, portugueseSubjects, portugueseCopulas, portugueseAuxiliaries, portugueseQuantifiers),
	abbreviations: setOf(portugueseAbbreviations),
}

func (s set) has(word string) bool {
	_, ok := s[bare(word)]
	return ok
}

func (g Grammar) opens(word string) bool {
	return g.openers.has(word)
}

func (g Grammar) binding(word string) float64 {
	switch {
	case g.strongBinders.has(word):
		return strongBinding
	case g.softBinders.has(word):
		return softBinding
	}
	return 0
}

func (g Grammar) endsSentence(word string) bool {
	switch lastMark(word) {
	case '.':
		return !g.abbreviations.has(word)
	case '!', '?', '…':
		return true
	}
	return false
}

func endsClause(word string) bool {
	switch lastMark(word) {
	case ',', ';', ':':
		return true
	}
	return false
}

func lastMark(word string) rune {
	runes := []rune(strings.TrimRight(word, "\"')]}»”’"))
	if len(runes) == 0 {
		return 0
	}
	return runes[len(runes)-1]
}

func bare(word string) string {
	return strings.ToLower(strings.TrimFunc(word, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
}

func isMarkerStart(word string) bool {
	return strings.HasPrefix(word, "[") || strings.HasPrefix(word, "(")
}

func isMarkerEnd(word string) bool {
	trimmed := strings.TrimRight(word, ".,;:!?")
	return strings.HasSuffix(trimmed, "]") || strings.HasSuffix(trimmed, ")")
}
