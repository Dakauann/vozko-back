package ai

import "strings"

const contextNoteHeading = "CONTEXTO DESTA MENSAGEM (gerado pelo sistema, não foi escrito por ninguém da conversa):"

func HistoryWindowStart(total, window int) int {
	if total <= window {
		return 0
	}
	step := max(window/4, 1)
	return (total - window + step - 1) / step * step
}

func ContextNote(sections ...string) Message {
	parts := []string{contextNoteHeading}
	for _, section := range sections {
		if text := strings.TrimSpace(section); text != "" {
			parts = append(parts, text)
		}
	}
	return Message{Role: RoleUser, Content: strings.Join(parts, "\n\n")}
}
