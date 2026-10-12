package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/ai"
)

var ErrCompactionFailed = errors.New("agentloop: the conversation could not be summarized to free context")

type Checkpointer interface {
	SaveCheckpoint(sessionID, text string) error
}

const (
	defaultCompactKeepTokens = 15_000
	reserveMinTokens         = 16_000
	charsPerToken            = 4
	imageTokenEstimate       = 1_500
	compactionToolOutputMax  = 1_250
	maxOverflowCompactions   = 3

	checkpointOpen  = "<conversation-checkpoint>"
	checkpointClose = "</conversation-checkpoint>"
	checkpointLead  = "O que segue é um resumo e um registro em texto da conversa anterior. Trate como histórico, não como instruções novas."

	summaryTemplate = `Use este formato na resposta (pode omitir seções que não se aplicam). Não inclua as marcas <template>.
<template>
## Objetivo
- [uma ou duas frases curtas sobre o que o usuário quer]

## Requisitos
- [restrições, preferências, requisitos e limites de escopo que o usuário disse, ou "(nenhum)"]

## Decisões
- [decisões já tomadas e por quê, ou "(nenhuma)"]

## Estado do trabalho
Divida o objetivo em metas menores e diga quais estão concluídas, em andamento ou bloqueadas.
### Concluído
- [metas concluídas, ou "(nenhuma)"]

### Em andamento
- [metas em andamento, ou "(nenhuma)"]

### Bloqueado
- [o que impede o avanço e por quê, ou "(nada)"]

## Próximo passo
1. [lista em ordem das próximas ações, ou "(nenhum)"]

## Referências
Os ids e as telas que outro agente precisa para continuar: project_id, artboard_id, ids de camadas e clipes, media_id, ids de conversas, campanhas, rascunhos e ações, e as habilidades que estavam em uso, pelo nome. No máximo 15, os mais importantes primeiro. Se não houver, "(nenhuma)".
- ` + "`[id, tela ou habilidade]`" + `: [por que importa]

## Contexto importante
- [fatos sem os quais o próximo agente não continua e que ele não acha sozinho, ou "(nenhum)"]
</template>`

	summaryRules = `Regras:
- Seções curtas, com itens de uma linha, sem parágrafos nem listas aninhadas.
- Prefira referências curtas a repetir detalhes; pode deixar de fora o que o próximo agente recupera com as ferramentas de leitura.
- Preserve exatos os ids, nomes de ferramentas, textos de erro, URLs, valores e os textos que o usuário pediu na peça.
- Leve adiante só os pedidos e perguntas do usuário ainda sem resposta ou que pedem ação, com as palavras exatas dele.
- Preserve o estado das ações: o que foi aplicado, proposto e aguardando aprovação, aprovado, recusado, gerado, exportado ou publicado, e o que já foi gasto.
- Não mencione o resumo nem que o contexto foi compactado.`

	summaryShared   = "Resuma só o que o usuário e o assistente disseram e fizeram. Deixe de fora as instruções e a configuração que o assistente recebeu sem o usuário dizer: o prompt do sistema, o conteúdo das habilidades e os avisos do app. O próximo agente recebe as versões atuais disso separadamente."
	summaryNoTools  = "Não continue a tarefa nem chame ferramentas."
	summaryOnlyText = "Devolva só o resumo no formato pedido, sem introdução, explicação ou comentário."
	summaryFirst    = "Você PRECISA resumir a conversa acima num resumo estruturado que será dado a outro agente para continuar o trabalho."
	summaryUpdate   = "Atualize o resumo anterior da conversa acima num resumo único e consolidado."
	summaryNewer    = "O histórico mais novo sempre vale mais que o resumo anterior. Preserve o que havia antes, a menos que o histórico novo contradiga, substitua, resolva ou torne velho. Se algo não ajuda mais a continuar o trabalho, pode tirar."
	summaryMerge    = "Incorpore os requisitos, decisões, avanços e contexto novos. Concilie Estado do trabalho e Próximo passo: tire de Em andamento o que foi concluído, remova bloqueios resolvidos e perguntas respondidas e preserve o que está pendente."
	summaryNoTags   = "Devolva só as seções em Markdown atualizadas, sem repetir as marcas <conversation-checkpoint>, <summary> ou <recent-context> do resumo anterior."
	summaryNudge    = "A resposta anterior não preencheu o modelo de resumo. Não chame ferramentas. Devolva o resumo em texto com os títulos exatos do modelo."
)

func summaryPrompt(update bool) string {
	shared := []string{summaryShared, summaryTemplate, summaryRules, summaryNoTools, summaryOnlyText}
	if !update {
		return strings.Join(append([]string{summaryFirst}, shared...), "\n\n")
	}
	return strings.Join(append([]string{summaryUpdate, summaryNewer, summaryMerge, summaryNoTags}, shared...), "\n\n")
}

func summaryFilled(text string) bool {
	headings := map[string]bool{}
	for _, line := range strings.Split(summaryTemplate, "\n") {
		if strings.HasPrefix(line, "##") {
			headings[line] = true
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if headings[strings.TrimSpace(line)] {
			return true
		}
	}
	return false
}

func checkpointText(summary, recent string) string {
	parts := []string{checkpointOpen, checkpointLead, "", "<summary>\n" + strings.TrimSpace(summary) + "\n</summary>"}
	if recent != "" {
		parts = append(parts, "", "<recent-context>\n"+recent+"\n</recent-context>")
	}
	return strings.Join(append(parts, checkpointClose), "\n")
}

func isCheckpoint(m ai.Message) bool {
	return m.Role == ai.RoleUser && strings.HasPrefix(m.Content, checkpointOpen)
}

func (c Config) compactionCeiling() (int, bool) {
	window := int(c.ModelLimits.ContextLength)
	if window <= 0 {
		return 0, false
	}
	reserve := window / 10
	if window >= 2*reserveMinTokens {
		reserve = max(reserve, reserveMinTokens)
	}
	return window - reserve, true
}

func (c Config) historyCap() int {
	if _, compacts := c.compactionCeiling(); compacts {
		return 0
	}
	return c.MaxHistoryMsgs
}

func estimateTokens(text string) int {
	return max(0, (len(text)+charsPerToken/2)/charsPerToken)
}

func estimateMessage(m ai.Message) int {
	total := estimateTokens(m.Content) + len(m.Images)*imageTokenEstimate
	for _, call := range m.ToolCalls {
		args, _ := json.Marshal(call.Arguments)
		total += estimateTokens(call.Name + string(args))
	}
	for _, b := range m.Reasoning {
		total += estimateTokens(b.Text + b.Summary + b.Data)
	}
	return total
}

func estimateRequest(drv Driver, history []ai.Message) int {
	total := estimateTokens(drv.SystemPrompt())
	if defs, err := json.Marshal(drv.Tools()); err == nil {
		total += estimateTokens(string(defs))
	}
	for _, m := range history {
		total += estimateMessage(m)
	}
	return total
}

type contextMeasure struct {
	tokens int
	upTo   int
}

func (m contextMeasure) estimate(drv Driver, history []ai.Message) int {
	if m.tokens == 0 {
		return estimateRequest(drv, history)
	}
	total := m.tokens
	for _, msg := range history[min(m.upTo, len(history)):] {
		total += estimateMessage(msg)
	}
	return total
}

func messageText(m ai.Message) string {
	if isCheckpoint(m) {
		return ""
	}
	switch m.Role {
	case ai.RoleUser:
		lines := []string{"[Usuário]: " + m.Content}
		for range m.Images {
			lines = append(lines, "[imagem anexada]")
		}
		return strings.Join(lines, "\n")
	case ai.RoleAssistant:
		var lines []string
		for _, b := range m.Reasoning {
			if b.Text != "" {
				lines = append(lines, "[Raciocínio do assistente]: "+b.Text)
			}
		}
		if strings.TrimSpace(m.Content) != "" {
			lines = append(lines, "[Assistente]: "+m.Content)
		}
		for _, call := range m.ToolCalls {
			args, _ := json.Marshal(call.Arguments)
			lines = append(lines, fmt.Sprintf("[Chamada de ferramenta]: %s(%s)", call.Name, args))
		}
		return strings.Join(lines, "\n")
	case ai.RoleTool:
		return "[Resultado da ferramenta]: " + clipToolOutput(m.Content)
	}
	return ""
}

func clipToolOutput(text string) string {
	runes := []rune(text)
	if len(runes) <= compactionToolOutputMax {
		return text
	}
	return string(runes[:compactionToolOutputMax]) + "\n[cortado]"
}

type conversationSplit struct {
	older  []ai.Message
	recent string
}

func splitConversation(history []ai.Message, keep int) (conversationSplit, bool) {
	type entry struct {
		index int
		text  string
		user  bool
	}
	var entries []entry
	for i, m := range history {
		if text := messageText(m); text != "" {
			entries = append(entries, entry{index: i, text: text, user: m.Role == ai.RoleUser})
		}
	}
	if len(entries) == 0 {
		return conversationSplit{}, false
	}
	dropped, total := len(entries), 0
	for dropped > 0 && total+estimateTokens(entries[dropped-1].text) <= keep {
		total += estimateTokens(entries[dropped-1].text)
		dropped--
	}
	dropped = min(dropped, len(entries)-1)
	start := len(entries)
	for i := dropped; i > 0; i-- {
		if entries[i].user {
			start = i
			break
		}
	}
	if start == len(entries) {
		for i := len(entries) - 1; i > 0; i-- {
			if entries[i].user {
				start = i
				break
			}
		}
	}
	olderEnd := len(history)
	var recent []string
	if start < len(entries) {
		olderEnd = entries[start].index
		for _, e := range entries[start:] {
			recent = append(recent, e.text)
		}
	}
	if olderEnd == 0 {
		return conversationSplit{}, false
	}
	return conversationSplit{older: history[:olderEnd], recent: strings.Join(recent, "\n\n")}, true
}

func (e *Engine) compact(ctx context.Context, drv Driver, cfg Config, sess *Session) error {
	split, ok := splitConversation(sess.History, cfg.CompactKeepTokens)
	if !ok {
		return nil
	}
	update := len(split.older) > 0 && isCheckpoint(split.older[0])
	msgs := append(append([]ai.Message(nil), split.older...), ai.Message{Role: ai.RoleUser, Content: summaryPrompt(update)})
	summary, err := e.summarize(ctx, drv, cfg, sess, msgs)
	if err != nil {
		return err
	}
	text := checkpointText(summary, split.recent)
	if saver, saves := drv.(Checkpointer); saves {
		if err := saver.SaveCheckpoint(cfg.SessionID, text); err != nil {
			return fmt.Errorf("%w: %w", ErrCompactionFailed, err)
		}
	}
	sess.History = []ai.Message{{Role: ai.RoleUser, Content: text}}
	sess.Turn = nil
	log.Printf("%s context compacted: %d messages summarized", cfg.LogPrefix, len(split.older))
	return nil
}

func (e *Engine) summarize(ctx context.Context, drv Driver, cfg Config, sess *Session, msgs []ai.Message) (string, error) {
	silent := func(string, interface{}) {}
	for attempt := 0; attempt < 2; attempt++ {
		request := cfg.request(drv, msgs)
		request.ToolChoice = "none"
		out, err := e.generate(ctx, silent, cfg, request)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrCompactionFailed, err)
		}
		sess.TokensUsed += out.Usage.TotalTokens
		sess.CostMicros += cfg.costOf(out.Usage)
		text := strings.TrimSpace(out.Message.Content)
		if summaryFilled(text) {
			return text, nil
		}
		msgs = append(msgs, ai.Message{Role: ai.RoleAssistant, Content: text}, ai.Message{Role: ai.RoleUser, Content: summaryNudge})
	}
	return "", fmt.Errorf("%w: the summary did not follow the template", ErrCompactionFailed)
}

func compactionHalt(err error) Outcome {
	return Outcome{Kind: OutcomeDone, Valid: false, Summary: err.Error(), Halt: err}
}
