package mercadolivre

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const MaxMessageRunes = 350

const MaxAnswerRunes = 2000

const (
	variationSelector   = rune(0xFE0F)
	firstSkinToneRune   = '\U0001F3FB'
	lastSkinToneRune    = '\U0001F3FF'
	latin1Last          = 0xFF
	asciiDelete         = 0x7F
	firstC1ControlRune  = 0x80
	lastC1ControlRune   = 0x9F
	firstPrintableASCII = 0x20
)

//go:embed message_text_rules.json
var messageTextRulesJSON []byte

type TextTooLongError struct {
	Max int
	Got int
}

func (e *TextTooLongError) Error() string {
	return fmt.Sprintf("%s: %d of %d", ErrTextTooLong.Error(), e.Got, e.Max)
}

func (e *TextTooLongError) Unwrap() error { return ErrTextTooLong }

type TextCharsetError struct {
	Runes []rune
}

func (e *TextCharsetError) Error() string {
	return fmt.Sprintf("%s: %q", ErrTextCharset.Error(), string(e.Runes))
}

func (e *TextCharsetError) Unwrap() error { return ErrTextCharset }

type emojiTable struct {
	fitzpatrick map[string]bool
	starts      map[rune]struct{}
	longest     int
}

var emojiRules = func() emojiTable {
	var file struct {
		MaxRunes int `json:"maxRunes"`
		Emoji    []struct {
			Sequence    string `json:"sequence"`
			Fitzpatrick bool   `json:"fitzpatrick"`
		} `json:"emoji"`
	}
	if err := json.Unmarshal(messageTextRulesJSON, &file); err != nil {
		panic(fmt.Sprintf("mercadolivre: message text rules are unreadable: %v", err))
	}
	if file.MaxRunes != MaxMessageRunes || len(file.Emoji) == 0 {
		panic("mercadolivre: message text rules disagree with the domain limit or carry no emoji")
	}
	table := emojiTable{
		fitzpatrick: make(map[string]bool, len(file.Emoji)),
		starts:      make(map[rune]struct{}),
	}
	for _, e := range file.Emoji {
		key := strings.ReplaceAll(e.Sequence, string(variationSelector), "")
		runes := []rune(key)
		if len(runes) == 0 {
			continue
		}
		table.fitzpatrick[key] = e.Fitzpatrick
		table.starts[runes[0]] = struct{}{}
		if len(runes) > table.longest {
			table.longest = len(runes)
		}
	}
	return table
}()

func (t emojiTable) match(runes []rune, at int) int {
	if _, ok := t.starts[runes[at]]; !ok {
		return 0
	}
	collected := make([]rune, 0, t.longest)
	ends := make([]int, 0, t.longest)
	for j := at; j < len(runes) && len(collected) < t.longest; j++ {
		if runes[j] == variationSelector {
			if j == at {
				return 0
			}
			continue
		}
		collected = append(collected, runes[j])
		ends = append(ends, j+1)
	}
	for n := len(collected); n > 0; n-- {
		fitz, ok := t.fitzpatrick[string(collected[:n])]
		if !ok {
			continue
		}
		end := skipVariationSelectors(runes, ends[n-1])
		if fitz && end < len(runes) && runes[end] >= firstSkinToneRune && runes[end] <= lastSkinToneRune {
			end = skipVariationSelectors(runes, end+1)
		}
		return end - at
	}
	return 0
}

func skipVariationSelectors(runes []rune, at int) int {
	for at < len(runes) && runes[at] == variationSelector {
		at++
	}
	return at
}

func allowedLatin1(r rune) bool {
	switch {
	case r == '\t' || r == '\n' || r == '\r':
		return true
	case r < firstPrintableASCII, r == asciiDelete:
		return false
	case r >= firstC1ControlRune && r <= lastC1ControlRune:
		return false
	}
	return r <= latin1Last
}

func offendingMessageRunes(text string) []rune {
	table := emojiRules
	runes := []rune(text)
	var offending []rune
	seen := map[rune]struct{}{}
	for i := 0; i < len(runes); {
		if n := table.match(runes, i); n > 0 {
			i += n
			continue
		}
		r := runes[i]
		i++
		if r != utf8.RuneError && allowedLatin1(r) {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		offending = append(offending, r)
	}
	return offending
}

func ValidateMessageText(text string) error {
	if strings.TrimSpace(text) == "" {
		return ErrTextEmpty
	}
	if offending := offendingMessageRunes(text); len(offending) > 0 {
		return &TextCharsetError{Runes: offending}
	}
	if n := utf8.RuneCountInString(text); n > MaxMessageRunes {
		return &TextTooLongError{Max: MaxMessageRunes, Got: n}
	}
	return nil
}

func ValidateAnswerText(text string) error {
	if !utf8.ValidString(text) {
		return &TextCharsetError{Runes: []rune{utf8.RuneError}}
	}
	if strings.TrimSpace(text) == "" {
		return ErrAnswerEmpty
	}
	if n := utf8.RuneCountInString(text); n > MaxAnswerRunes {
		return fmt.Errorf("%w: %d of %d", ErrAnswerTooLong, n, MaxAnswerRunes)
	}
	return nil
}

func ValidateOutgoingMessage(text string, attachments int) error {
	if attachments < 0 || attachments > MaxAttachmentsPerMessage {
		return fmt.Errorf("%w: %d attachments", ErrMediaUnsupported, attachments)
	}
	if attachments > 0 && strings.TrimSpace(text) == "" {
		return nil
	}
	return ValidateMessageText(text)
}
