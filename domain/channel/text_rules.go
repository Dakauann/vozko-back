package channel

import (
	"fmt"
	"unicode/utf8"
)

const (
	TextViolationMaxRunes = "max_runes"
	TextViolationMaxBytes = "max_bytes"
)

type TextViolation struct {
	Code    string
	Message string
}

type TextRule interface {
	Instruction() string
	Check(body string) (TextViolation, bool)
}

type maxRunesRule int

func (r maxRunesRule) Instruction() string {
	return fmt.Sprintf("Cada mensagem enviada neste canal tem no máximo %d caracteres.", int(r))
}

func (r maxRunesRule) Check(body string) (TextViolation, bool) {
	count := utf8.RuneCountInString(body)
	if count <= int(r) {
		return TextViolation{}, false
	}
	return TextViolation{
		Code:    TextViolationMaxRunes,
		Message: fmt.Sprintf("a mensagem tem %d caracteres e o limite do canal é %d", count, int(r)),
	}, true
}

type maxBytesRule int

func (r maxBytesRule) Instruction() string {
	return fmt.Sprintf("Cada mensagem enviada neste canal tem no máximo %d bytes em UTF-8 (letras acentuadas ocupam 2 bytes e emojis até 4).", int(r))
}

func (r maxBytesRule) Check(body string) (TextViolation, bool) {
	if len(body) <= int(r) {
		return TextViolation{}, false
	}
	return TextViolation{
		Code:    TextViolationMaxBytes,
		Message: fmt.Sprintf("a mensagem tem %d bytes em UTF-8 e o limite do canal é %d", len(body), int(r)),
	}, true
}

func (c Capabilities) lengthRules() []TextRule {
	rules := make([]TextRule, 0, 2)
	if c.MaxTextRunes > 0 {
		rules = append(rules, maxRunesRule(c.MaxTextRunes))
	}
	if c.MaxTextBytes > 0 {
		rules = append(rules, maxBytesRule(c.MaxTextBytes))
	}
	return rules
}

func (c Capabilities) TextRules() []TextRule {
	return append(c.lengthRules(), c.ProviderTextRules...)
}

func (c Capabilities) TextViolations(body string) []TextViolation {
	var out []TextViolation
	for _, rule := range c.TextRules() {
		if violation, broken := rule.Check(body); broken {
			out = append(out, violation)
		}
	}
	return out
}

func (c Capabilities) TextInstructions() []string {
	rules := c.TextRules()
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		out = append(out, rule.Instruction())
	}
	return out
}

func (c Capabilities) Reserving(prefix string) (Capabilities, bool) {
	if prefix == "" {
		return c, true
	}
	if c.MaxTextRunes > 0 {
		room := c.MaxTextRunes - utf8.RuneCountInString(prefix)
		if room <= 0 {
			return Capabilities{}, false
		}
		c.MaxTextRunes = room
	}
	if c.MaxTextBytes > 0 {
		room := c.MaxTextBytes - len(prefix)
		if room <= 0 {
			return Capabilities{}, false
		}
		c.MaxTextBytes = room
	}
	return c, true
}

func (c Capabilities) WithOutboundBudget(budget int) Capabilities {
	c.OutboundBudget = budget
	return c
}
