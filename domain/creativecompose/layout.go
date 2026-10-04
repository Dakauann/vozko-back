package creativecompose

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Template string

const (
	TemplateFeed  Template = "feed"
	TemplateCard  Template = "card"
	TemplateStory Template = "story"
)

var templateSizes = map[Template][2]int{
	TemplateFeed:  {1080, 1350},
	TemplateCard:  {1080, 1080},
	TemplateStory: {1080, 1920},
}

func (t Template) Size() (int, int, error) {
	size, ok := templateSizes[t]
	if !ok {
		return 0, 0, fmt.Errorf("creativecompose: unknown template %q", t)
	}
	return size[0], size[1], nil
}

const (
	MaxEyebrow      = 28
	MaxHeadline     = 32
	MaxHighlight    = 32
	MaxSubline      = 120
	MaxCallout      = 44
	MaxCallouts     = 3
	MaxCallToAction = 32
	MaxFootnote     = 32
)

type Layout struct {
	Template     Template
	Eyebrow      string
	Headline     string
	Highlight    string
	Subline      string
	ImageMediaID string
	LogoMediaID  string
	Callouts     []string
	CallToAction string
	WhatsAppIcon bool
	Footnote     string
}

func (l *Layout) Normalize() {
	l.Eyebrow, l.Headline, l.Highlight = strings.TrimSpace(l.Eyebrow), strings.TrimSpace(l.Headline), strings.TrimSpace(l.Highlight)
	l.Subline, l.CallToAction, l.Footnote = strings.TrimSpace(l.Subline), strings.TrimSpace(l.CallToAction), strings.TrimSpace(l.Footnote)
	l.ImageMediaID, l.LogoMediaID = strings.TrimSpace(l.ImageMediaID), strings.TrimSpace(l.LogoMediaID)
	callouts := make([]string, 0, len(l.Callouts))
	for _, c := range l.Callouts {
		if text := strings.TrimSpace(c); text != "" {
			callouts = append(callouts, text)
		}
	}
	l.Callouts = callouts
}

const (
	CodeRequired  = "required"
	CodeTooLong   = "too_long"
	CodeTooMany   = "too_many"
	CodeDash      = "dash"
	CodeInvalid   = "invalid"
	CodeNotFound  = "not_found"
	FieldTemplate = "template"
	FieldHeadline = "headline"
	FieldImage    = "image"
	FieldCallouts = "callouts"
	FieldLogo     = "logo"
)

type FieldIssue struct {
	Field string
	Code  string
}

type ValidationError struct{ Issues []FieldIssue }

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		parts = append(parts, issue.Field+": "+issue.Code)
	}
	return "creativecompose: " + strings.Join(parts, ", ")
}

func (l Layout) Validate() error {
	var issues []FieldIssue
	add := func(field, code string) { issues = append(issues, FieldIssue{Field: field, Code: code}) }
	if _, _, err := l.Template.Size(); err != nil {
		add(FieldTemplate, CodeInvalid)
	}
	if strings.TrimSpace(l.Headline) == "" {
		add(FieldHeadline, CodeRequired)
	}
	if strings.TrimSpace(l.ImageMediaID) == "" {
		add(FieldImage, CodeRequired)
	}
	texts := []struct {
		field string
		value string
		max   int
	}{
		{"eyebrow", l.Eyebrow, MaxEyebrow}, {FieldHeadline, l.Headline, MaxHeadline}, {"highlight", l.Highlight, MaxHighlight},
		{"subline", l.Subline, MaxSubline}, {"call_to_action", l.CallToAction, MaxCallToAction}, {"footnote", l.Footnote, MaxFootnote},
	}
	for _, text := range texts {
		checkText(text.field, text.value, text.max, add)
	}
	if len(l.Callouts) > MaxCallouts {
		add(FieldCallouts, CodeTooMany)
	}
	for _, callout := range l.Callouts {
		checkText(FieldCallouts, callout, MaxCallout, add)
	}
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func checkText(field, value string, max int, add func(string, string)) {
	if utf8.RuneCountInString(value) > max {
		add(field, CodeTooLong)
	}
	if strings.ContainsAny(value, "–—") {
		add(field, CodeDash)
	}
}
