package buttonaction

import (
	"strings"
	"unicode"

	"vozko/domain/whatsapp/template"
)

const commandOpeners = "\"'“‘«([{"

func ShownExitWords(t *template.Template) []string {
	if t == nil {
		return []string{}
	}
	return ExitWordsShownIn(visibleTexts(t)...)
}

func ExitWordsShownIn(texts ...string) []string {
	words := []string{}
	seen := map[string]bool{}
	for _, text := range texts {
		for _, word := range commandsIn(text) {
			key := FoldExitWord(word)
			if seen[key] {
				continue
			}
			seen[key] = true
			words = append(words, word)
		}
	}
	return words
}

func visibleTexts(t *template.Template) []string {
	var texts []string
	for _, comp := range t.Components {
		switch strings.ToUpper(strings.TrimSpace(comp.Type)) {
		case "HEADER":
			if format := strings.ToUpper(strings.TrimSpace(comp.Format)); format == "" || format == "TEXT" {
				texts = append(texts, comp.Text)
			}
		case "BODY", "FOOTER":
			texts = append(texts, comp.Text)
		}
	}
	for _, reply := range t.QuickReplies() {
		texts = append(texts, reply.Text)
	}
	return texts
}

func commandsIn(text string) []string {
	runes := []rune(text)
	var commands []string
	for i, r := range runes {
		if string(r) != ExitCommandPrefix || !opensCommand(runes, i) {
			continue
		}
		end := i + 1
		for end < len(runes) && isCommandRune(runes[end]) {
			end++
		}
		word := string(runes[i+1 : end])
		if word != "" && ValidateExitWord(word) == nil {
			commands = append(commands, word)
		}
	}
	return commands
}

func opensCommand(runes []rune, slash int) bool {
	if slash == 0 {
		return true
	}
	before := runes[slash-1]
	return unicode.IsSpace(before) || strings.ContainsRune(commandOpeners, before)
}

func isCommandRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}
