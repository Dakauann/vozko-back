package audience

import (
	"strings"
	"unicode"

	"vozko/domain/shared"
)

const (
	TopicKeyOther = "other"

	MaxTopics = 30

	MaxTopicLabelRunes       = 60
	MaxTopicDescriptionRunes = 200
)

type Topic struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

func OtherTopic() Topic {
	return Topic{Key: TopicKeyOther, Label: "Outros", Description: "não se encaixa em nenhum dos temas acima"}
}

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

type TopicSet []Topic

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

func (ts TopicSet) Validate() error {
	if len(ts) > MaxTopics+1 {
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

func (ts TopicSet) Keys() []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Key
	}
	return out
}

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
