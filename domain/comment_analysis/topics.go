package comment_analysis

import (
	"strings"
	"unicode"

	"vozko/domain/shared"
)

// Topics are a CLOSED set per account (§5.2). The cluster view ("Asfalto e
// Pavimentação", "Saúde Pública", …) is only aggregatable if the labels are
// stable; free-text topics drift into hundreds of near-duplicates within a
// week. So the model picks from the account's list (the same way the
// manage_entry_stage tool picks a stage) and "other" is always present as
// the pressure valve. A topic climbing the other bucket is the signal to add
// one, and the dashboard says so.

const (
	// TopicKeyOther is always present in every set.
	TopicKeyOther = "other"

	// MaxTopics bounds the enum the model chooses from. Past this the
	// classification gets worse, not better, and the prompt gets longer.
	MaxTopics = 30

	MaxTopicLabelRunes       = 60
	MaxTopicDescriptionRunes = 200
)

// Topic is one entry in an account's set. Key is what is stored and
// aggregated; Label is what the operator sees; Description tells the model
// what belongs here.
type Topic struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// OtherTopic is the pressure valve every set carries.
func OtherTopic() Topic {
	return Topic{Key: TopicKeyOther, Label: "Outros", Description: "não se encaixa em nenhum dos temas acima"}
}

// NormalizeTopicKey folds a label or a key into the canonical slug: accent-
// and case-folded (shared.FoldForMatch, the same folding the Instagram rule
// engine uses), non-alphanumerics collapsed to single hyphens. "Saúde
// Pública" and "saude publica" both become "saude-publica", which is the
// whole point: an accent cannot fork a topic.
func NormalizeTopicKey(s string) string {
	folded := shared.FoldForMatch(s)
	var b strings.Builder
	b.Grow(len(folded))
	pendingHyphen := false
	for _, r := range folded {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
			continue
		}
		pendingHyphen = true
	}
	return b.String()
}

// TopicSet is an account's ordered topic list.
type TopicSet []Topic

// Normalize trims, derives missing keys from labels, folds keys, drops
// blanks and duplicates (first wins), and appends other exactly once, last.
func (ts TopicSet) Normalize() TopicSet {
	out := make(TopicSet, 0, len(ts)+1)
	seen := make(map[string]struct{}, len(ts)+1)
	for _, t := range ts {
		t.Label = strings.TrimSpace(t.Label)
		t.Description = strings.TrimSpace(t.Description)
		key := NormalizeTopicKey(t.Key)
		if key == "" {
			key = NormalizeTopicKey(t.Label)
		}
		if key == "" || key == TopicKeyOther {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		t.Key = key
		if t.Label == "" {
			t.Label = key
		}
		out = append(out, t)
	}
	return append(out, OtherTopic())
}

// Validate checks a set as stored. It expects a normalised set: keys that
// are not already canonical are rejected rather than fixed, so a caller that
// skipped Normalize cannot persist a key that will never match.
func (ts TopicSet) Validate() error {
	if len(ts) > MaxTopics+1 { // +1 for other
		return ErrTooManyTopics
	}
	seen := make(map[string]struct{}, len(ts))
	for _, t := range ts {
		if t.Key == "" || t.Key != NormalizeTopicKey(t.Key) {
			return ErrTopicKeyInvalid
		}
		if _, dup := seen[t.Key]; dup {
			return ErrTopicKeyInvalid
		}
		seen[t.Key] = struct{}{}
		if len([]rune(t.Label)) > MaxTopicLabelRunes {
			return ErrTopicLabelTooLong
		}
		if len([]rune(t.Description)) > MaxTopicDescriptionRunes {
			return ErrTopicLabelTooLong
		}
	}
	if _, ok := seen[TopicKeyOther]; !ok {
		return ErrTopicKeyInvalid
	}
	return nil
}

// Has reports whether key is in the set (exact, canonical key).
func (ts TopicSet) Has(key string) bool {
	for _, t := range ts {
		if t.Key == key {
			return true
		}
	}
	return false
}

func (ts TopicSet) count(key string) int {
	n := 0
	for _, t := range ts {
		if t.Key == key {
			n++
		}
	}
	return n
}

// Keys returns the canonical keys in order: the schema enum.
func (ts TopicSet) Keys() []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Key
	}
	return out
}

// Resolve maps whatever the model (or an operator) wrote to a canonical key
// in the set: the key itself, the label, or any spelling that folds to
// either. Returns false for anything outside the set.
func (ts TopicSet) Resolve(s string) (string, bool) {
	norm := NormalizeTopicKey(s)
	if norm == "" {
		return "", false
	}
	for _, t := range ts {
		if t.Key == norm {
			return t.Key, true
		}
	}
	for _, t := range ts {
		if NormalizeTopicKey(t.Label) == norm {
			return t.Key, true
		}
	}
	return "", false
}

// ---- Defaults per vertical ----

// Vertical is the kind of customer, which decides the seed topics.
type Vertical string

const (
	VerticalGov      Vertical = "gov"
	VerticalRetail   Vertical = "retail"
	VerticalServices Vertical = "services"
)

func (v Vertical) Valid() bool {
	switch v {
	case VerticalGov, VerticalRetail, VerticalServices:
		return true
	}
	return false
}

// DefaultTopicsFor seeds an account's set. An unknown vertical gets the
// services set, the most generic of the three.
func DefaultTopicsFor(v Vertical) TopicSet {
	var ts TopicSet
	switch v {
	case VerticalGov:
		ts = TopicSet{
			{Key: "saude", Label: "Saúde", Description: "postos, hospitais, filas, remédios, atendimento médico"},
			{Key: "educacao", Label: "Educação", Description: "escolas, creches, professores, merenda, vagas"},
			{Key: "infraestrutura", Label: "Infraestrutura", Description: "asfalto, buracos, obras, iluminação, saneamento"},
			{Key: "seguranca", Label: "Segurança", Description: "policiamento, violência, assaltos, guarda municipal"},
			{Key: "transporte", Label: "Transporte", Description: "ônibus, trânsito, tarifas, ciclovias"},
			{Key: "emprego-economia", Label: "Emprego e Economia", Description: "vagas, comércio, impostos, custo de vida"},
			{Key: "meio-ambiente", Label: "Meio Ambiente", Description: "lixo, enchentes, praças, poluição"},
			{Key: "assistencia-social", Label: "Assistência Social", Description: "benefícios, CRAS, moradia, auxílio"},
			{Key: "gestao-politica", Label: "Gestão e Política", Description: "prefeitura, câmara, eleições, corrupção, a pessoa do político"},
		}
	case VerticalRetail:
		ts = TopicSet{
			{Key: "produto", Label: "Produto", Description: "qualidade, tamanho, cor, material, funcionamento"},
			{Key: "preco", Label: "Preço", Description: "valor, promoção, desconto, parcelamento"},
			{Key: "entrega", Label: "Entrega", Description: "prazo, frete, rastreio, extravio"},
			{Key: "troca-devolucao", Label: "Troca e Devolução", Description: "defeito, arrependimento, reembolso"},
			{Key: "atendimento", Label: "Atendimento", Description: "resposta, educação, demora, suporte"},
			{Key: "disponibilidade", Label: "Disponibilidade", Description: "estoque, reposição, tamanhos esgotados"},
			{Key: "loja-fisica", Label: "Loja Física", Description: "horário, localização, estacionamento"},
		}
	default:
		ts = TopicSet{
			{Key: "qualidade", Label: "Qualidade do Serviço", Description: "resultado, capricho, competência"},
			{Key: "preco", Label: "Preço", Description: "valor, orçamento, formas de pagamento"},
			{Key: "agendamento", Label: "Agendamento", Description: "horários, disponibilidade, espera, atrasos"},
			{Key: "atendimento", Label: "Atendimento", Description: "cordialidade, comunicação, suporte"},
			{Key: "localizacao", Label: "Localização", Description: "endereço, acesso, estacionamento"},
			{Key: "equipe", Label: "Equipe", Description: "profissionais, elogios ou queixas nominais"},
		}
	}
	return ts.Normalize()
}
