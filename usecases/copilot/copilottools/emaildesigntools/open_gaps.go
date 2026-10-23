package emaildesigntools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"vozko/domain/copilot"
	"vozko/domain/emaildesign"
)

const (
	watchTimeout     = 4 * time.Second
	resultOpenGaps   = "open_gaps"
	snapshotDesign   = "\n\n# Design aberto\nO usuário está com o design %s aberto no editor de e-mail, ao lado desta conversa (design_id %s).\n"
	snapshotCampaign = "\n\n# Campanha de e-mail aberta\nO usuário está com o conteúdo da campanha %s aberto no editor de e-mail, ao lado desta conversa (campaign_id %s).\n"
	snapshotTools    = "email_read lê esse e-mail como está na tela, sem pedir id; email_edit, email_look e email_generate_image trabalham nele.\n"
	snapshotSilent   = "O editor não respondeu à leitura do início desta resposta; email_read lê o e-mail assim que ele voltar a responder."
	snapshotGaps     = "O que impede salvar e enviar, lido do editor no início desta resposta (block_id, tipo, texto, código):\n"
	snapshotNoGaps   = "Lido do editor no início desta resposta: nada impede salvar e enviar."
	closingNoteStart = "Antes de encerrar: o e-mail ainda tem lacunas que impedem salvar e enviar, e a sua resposta não fala delas:\n"
	closingNoteEnd   = "Escreva de novo a resposta final, completa: diga ao usuário o que falta e peça esses dados numa só pergunta, ou corrija o que depende só de você. Nunca invente links, nomes ou endereços para fechar uma lacuna."
)

var gapWords = map[string][]string{
	"href_missing":               {"link", "url", "enlace", "destino"},
	"image_src_missing":          {"imagem", "image", "imagen", "foto", "photo", "bild"},
	"text_empty":                 {"texto", "text", "título", "title", "titel"},
	"social_links_empty":         {"rede", "social", "perfil", "profile"},
	"social_url_missing":         {"rede", "social", "perfil", "profile"},
	"footer_company_missing":     {"empresa", "company", "nome", "name", "nombre", "firma"},
	"footer_address_missing":     {"endereço", "address", "dirección", "direccion", "adresse"},
	"footer_missing":             {"rodapé", "footer", "pie", "fußzeile"},
	"unknown_merge_tag":          {"variável", "variable", "merge", "personaliza", "{{"},
	"merge_tag_malformed":        {"variável", "variable", "merge", "personaliza", "{{"},
	"subject_empty":              {"assunto", "subject", "asunto", "betreff"},
	"subject_too_long":           {"assunto", "subject", "asunto", "betreff"},
	"subject_invalid_characters": {"assunto", "subject", "asunto", "betreff"},
}

type gappedResult struct {
	body map[string]interface{}
	gaps []emaildesign.Gap
}

func withOpenGaps(body map[string]interface{}, live liveDesign) gappedResult {
	return gappedResult{body: body, gaps: live.gaps()}
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

func watching(cc copilot.Context) bool {
	return cc.View.OnEmailDocument() && cc.Screen != nil
}

func (t *emailReadTool) watched(ctx context.Context, cc copilot.Context) (liveDesign, bool) {
	if !watching(cc) {
		return liveDesign{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, watchTimeout)
	defer cancel()
	live, err := readLive(ctx, cc)
	return live, err == nil
}

func openLine(view copilot.View, name string) string {
	shown := strconv.Quote(name)
	if strings.TrimSpace(name) == "" {
		shown = "sem nome"
	}
	if view.OnEmailCampaign() {
		return fmt.Sprintf(snapshotCampaign, shown, view.ProjectID)
	}
	return fmt.Sprintf(snapshotDesign, shown, view.ProjectID)
}

func (t *emailReadTool) ViewSnapshot(ctx context.Context, cc copilot.Context) string {
	if !watching(cc) {
		return ""
	}
	live, ok := t.watched(ctx, cc)
	head := openLine(cc.View, live.Name) + snapshotTools
	if !ok {
		return head + snapshotSilent
	}
	gaps := live.gaps()
	if len(gaps) == 0 {
		return head + snapshotNoGaps
	}
	return head + snapshotGaps + strings.TrimSuffix(gapLines(gaps), "\n")
}

func (t *emailReadTool) ClosingNote(ctx context.Context, cc copilot.Context, reply string) string {
	live, ok := t.watched(ctx, cc)
	if !ok {
		return ""
	}
	gaps := live.gaps()
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
