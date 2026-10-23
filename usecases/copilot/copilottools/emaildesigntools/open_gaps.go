package emaildesigntools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/copilot"
	"vozko/domain/emaildesign"
)

const (
	watchTimeout     = 4 * time.Second
	resultOpenGaps   = "open_gaps"
	snapshotTitle    = "\n\n# Design aberto\n"
	snapshotGaps     = "O que impede salvar e enviar, lido do editor no início desta resposta (block_id, tipo, texto, código):\n"
	snapshotNoGaps   = "Lido do editor no início desta resposta: nada impede salvar e enviar."
	closingNoteStart = "Antes de encerrar: o design ainda tem lacunas que impedem salvar e enviar, e a sua resposta não fala delas:\n"
	closingNoteEnd   = "Escreva de novo a resposta final, completa: diga ao usuário o que falta e peça esses dados numa só pergunta, ou corrija o que depende só de você. Nunca invente links, nomes ou endereços para fechar uma lacuna."
)

var gapWords = map[string][]string{
	"href_missing":           {"link", "url", "enlace", "destino"},
	"image_src_missing":      {"imagem", "image", "imagen", "foto", "photo", "bild"},
	"text_empty":             {"texto", "text", "título", "title", "titel"},
	"social_links_empty":     {"rede", "social", "perfil", "profile"},
	"social_url_missing":     {"rede", "social", "perfil", "profile"},
	"footer_company_missing": {"empresa", "company", "nome", "name", "nombre", "firma"},
	"footer_address_missing": {"endereço", "address", "dirección", "direccion", "adresse"},
	"footer_missing":         {"rodapé", "footer", "pie", "fußzeile"},
	"unknown_merge_tag":      {"variável", "variable", "merge", "personaliza", "{{"},
	"merge_tag_malformed":    {"variável", "variable", "merge", "personaliza", "{{"},
}

type gappedResult struct {
	body map[string]interface{}
	gaps []emaildesign.Gap
}

func withOpenGaps(body map[string]interface{}, doc emaildesign.Document) gappedResult {
	return gappedResult{body: body, gaps: emaildesign.OpenGaps(doc)}
}

func (g gappedResult) MarshalJSON() ([]byte, error) {
	gaps, err := json.Marshal(g.gaps)
	if err != nil {
		return nil, err
	}
	tail := `"` + resultOpenGaps + `":` + string(gaps) + "}"
	if len(g.body) == 0 {
		return []byte("{" + tail), nil
	}
	body, err := json.Marshal(g.body)
	if err != nil {
		return nil, err
	}
	return append(body[:len(body)-1], []byte(","+tail)...), nil
}

func gapLines(gaps []emaildesign.Gap) string {
	var b strings.Builder
	for _, gap := range gaps {
		b.WriteString("- ")
		if gap.BlockID == "" {
			b.WriteString("settings")
		} else {
			b.WriteString(gap.BlockID + " (" + string(gap.Kind))
			if gap.Text != "" {
				b.WriteString(` "` + gap.Text + `"`)
			}
			b.WriteString(")")
		}
		b.WriteString(": " + gap.Code)
		if gap.Field != "" {
			b.WriteString(" em " + gap.Field)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func mentioned(reply string, gap emaildesign.Gap) bool {
	lower := strings.ToLower(reply)
	if gap.Text != "" && strings.Contains(lower, strings.ToLower(gap.Text)) {
		return true
	}
	for _, word := range append(gapWords[gap.Code], gap.Code) {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

func (t *emailReadTool) watchedGaps(ctx context.Context, cc copilot.Context) ([]emaildesign.Gap, bool) {
	if !cc.View.OnEmailDesign() || cc.Screen == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, watchTimeout)
	defer cancel()
	live, err := readLive(ctx, cc)
	if err != nil {
		return nil, false
	}
	return emaildesign.OpenGaps(live.Document), true
}

func (t *emailReadTool) ViewSnapshot(ctx context.Context, cc copilot.Context) string {
	gaps, ok := t.watchedGaps(ctx, cc)
	switch {
	case !ok:
		return ""
	case len(gaps) == 0:
		return snapshotTitle + snapshotNoGaps
	}
	return snapshotTitle + snapshotGaps + strings.TrimSuffix(gapLines(gaps), "\n")
}

func (t *emailReadTool) ClosingNote(ctx context.Context, cc copilot.Context, reply string) string {
	gaps, _ := t.watchedGaps(ctx, cc)
	var left []emaildesign.Gap
	for _, gap := range gaps {
		if !mentioned(reply, gap) {
			left = append(left, gap)
		}
	}
	if len(left) == 0 {
		return ""
	}
	return closingNoteStart + gapLines(left) + closingNoteEnd
}
