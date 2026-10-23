package rcs

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Issue struct {
	Path string
	Err  error
}

func (c Content) Issues() []Issue {
	var out []Issue
	add := func(path string, err error) {
		if err != nil {
			out = append(out, Issue{Path: path, Err: err})
		}
	}
	hasText := strings.TrimSpace(c.Text) != ""
	switch {
	case !hasText && c.Media == nil && len(c.Cards) == 0:
		add("", ErrContentEmpty)
	case len(c.Cards) > 0 && (hasText || c.Media != nil):
		add("", ErrContentMixed)
	}
	if utf8.RuneCountInString(c.Text) > MaxTextRunes {
		add("text", ErrTextTooLong)
	}
	if len(c.Cards) > MaxCarouselCards {
		add("cards", ErrTooManyCards)
	}
	if c.Media != nil {
		add("media", c.Media.validate())
	}
	for i, card := range c.Cards {
		out = append(out, card.issues(fmt.Sprintf("cards[%d]", i))...)
	}
	out = append(out, suggestionIssues("suggestions", c.Suggestions, MaxMessageSuggestions)...)
	return out
}

func (c Card) issues(path string) []Issue {
	var out []Issue
	if strings.TrimSpace(c.Title) == "" && strings.TrimSpace(c.Description) == "" && c.Media == nil {
		out = append(out, Issue{Path: path, Err: ErrCardEmpty})
	}
	if utf8.RuneCountInString(c.Title) > MaxCardTitleRunes {
		out = append(out, Issue{Path: path + ".title", Err: ErrCardTitleTooLong})
	}
	if utf8.RuneCountInString(c.Description) > MaxCardDescriptionRunes {
		out = append(out, Issue{Path: path + ".description", Err: ErrCardTextTooLong})
	}
	if c.Media != nil {
		if err := c.Media.validate(); err != nil {
			out = append(out, Issue{Path: path + ".media", Err: err})
		}
	}
	return append(out, suggestionIssues(path+".suggestions", c.Suggestions, MaxCardSuggestions)...)
}

func suggestionIssues(path string, suggestions []Suggestion, max int) []Issue {
	var out []Issue
	if len(suggestions) > max {
		out = append(out, Issue{Path: path, Err: fmt.Errorf("%w: at most %d", ErrTooManySuggestions, max)})
	}
	seen := make(map[string]bool, len(suggestions))
	for i, s := range suggestions {
		at := fmt.Sprintf("%s[%d]", path, i)
		if err := s.validate(); err != nil {
			out = append(out, Issue{Path: at, Err: err})
			continue
		}
		key := strings.ToLower(strings.TrimSpace(s.Label))
		if seen[key] {
			out = append(out, Issue{Path: at, Err: fmt.Errorf("%w: two buttons share the label %q", ErrSuggestionInvalid, s.Label)})
		}
		seen[key] = true
	}
	return out
}
