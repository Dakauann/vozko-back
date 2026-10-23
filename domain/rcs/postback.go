package rcs

import (
	"strconv"
	"strings"
)

const postbackPrefix = "vz:"

type ButtonKey struct {
	Card  int `json:"card"`
	Index int `json:"index"`
}

func (k ButtonKey) Postback() string {
	if k.Card < 0 {
		return postbackPrefix + "m" + strconv.Itoa(k.Index)
	}
	return postbackPrefix + "c" + strconv.Itoa(k.Card) + "b" + strconv.Itoa(k.Index)
}

func ParsePostback(raw string) (ButtonKey, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(raw), postbackPrefix)
	if !ok {
		return ButtonKey{}, false
	}
	if index, ok := strings.CutPrefix(rest, "m"); ok {
		n, ok := naturalNumber(index)
		return ButtonKey{Card: -1, Index: n}, ok
	}
	cardPart, ok := strings.CutPrefix(rest, "c")
	if !ok {
		return ButtonKey{}, false
	}
	cardRaw, indexRaw, ok := strings.Cut(cardPart, "b")
	if !ok {
		return ButtonKey{}, false
	}
	cardIndex, okCard := naturalNumber(cardRaw)
	index, okIndex := naturalNumber(indexRaw)
	if !okCard || !okIndex {
		return ButtonKey{}, false
	}
	return ButtonKey{Card: cardIndex, Index: index}, true
}

func naturalNumber(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

type Button struct {
	Key        ButtonKey  `json:"key"`
	Suggestion Suggestion `json:"suggestion"`
}

func (c Content) Buttons() []Button {
	var out []Button
	for i, s := range c.Suggestions {
		out = append(out, Button{Key: ButtonKey{Card: -1, Index: i}, Suggestion: s})
	}
	for ci, card := range c.Cards {
		for i, s := range card.Suggestions {
			out = append(out, Button{Key: ButtonKey{Card: ci, Index: i}, Suggestion: s})
		}
	}
	return out
}

func (c Content) ButtonAt(key ButtonKey) (Suggestion, bool) {
	suggestions := c.Suggestions
	if key.Card >= 0 {
		if key.Card >= len(c.Cards) {
			return Suggestion{}, false
		}
		suggestions = c.Cards[key.Card].Suggestions
	}
	if key.Index < 0 || key.Index >= len(suggestions) {
		return Suggestion{}, false
	}
	return suggestions[key.Index], true
}

func (c Content) WithTrackedPostbacks() Content {
	out := c
	out.Suggestions = trackSuggestions(c.Suggestions, -1)
	if len(c.Cards) > 0 {
		out.Cards = make([]Card, len(c.Cards))
		for i, card := range c.Cards {
			card.Suggestions = trackSuggestions(card.Suggestions, i)
			out.Cards[i] = card
		}
	}
	return out
}

func trackSuggestions(suggestions []Suggestion, cardIndex int) []Suggestion {
	if len(suggestions) == 0 {
		return nil
	}
	out := make([]Suggestion, len(suggestions))
	for i, s := range suggestions {
		s.Postback = ButtonKey{Card: cardIndex, Index: i}.Postback()
		out[i] = s
	}
	return out
}

const OptOutPostback = "opt_out"

func (s Suggestion) OptsOut() bool {
	return s.Kind == SuggestReply && s.Postback == OptOutPostback
}
