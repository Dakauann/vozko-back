package rcs

import (
	"errors"
	"strings"
	"testing"
)

func TestIssuesListsEveryProblemWithItsPath(t *testing.T) {
	c := Content{Cards: []Card{
		{Title: "A", Media: image()},
		{Title: strings.Repeat("t", MaxCardTitleRunes+1), Suggestions: []Suggestion{{Kind: SuggestReply, Label: ""}, {Kind: SuggestOpenURL, Label: "Ver", URL: "http://x"}}},
	}}
	issues := c.Issues()
	want := map[string]error{
		"cards[1].title":          ErrCardTitleTooLong,
		"cards[1].suggestions[0]": ErrSuggestionInvalid,
		"cards[1].suggestions[1]": ErrSuggestionInvalid,
	}
	if len(issues) != len(want) {
		t.Fatalf("issues = %+v", issues)
	}
	for _, issue := range issues {
		if !errors.Is(issue.Err, want[issue.Path]) {
			t.Fatalf("issue %s = %v, want %v", issue.Path, issue.Err, want[issue.Path])
		}
	}
}

func TestIssuesAgreeWithValidate(t *testing.T) {
	for _, c := range []Content{
		{},
		{Text: "Oi", Cards: []Card{card("A")}},
		{Text: strings.Repeat("a", MaxTextRunes+1)},
		{Media: &Media{Kind: MediaImage, URL: "http://a.example.com/a.jpg", MIMEType: "image/jpeg"}},
		{Text: "ok"},
	} {
		if (c.Validate() == nil) != (len(c.Issues()) == 0) {
			t.Fatalf("Validate = %v but Issues = %+v", c.Validate(), c.Issues())
		}
	}
}
