package emaildesign

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	RecommendedSubjectRunes = 60
	RecommendedMaxLinks     = 25
	textRunesPerImage       = 200
	minCapsLetters          = 4
	maxSubjectMarks         = 2
)

type Hint struct {
	Code    string `json:"code"`
	BlockID string `json:"block_id,omitempty"`
}

type lintTally struct {
	hints     []Hint
	images    int
	textRunes int
	links     int
	actions   int
}

func Lint(doc Document, subject string) []Hint {
	t := &lintTally{}
	t.subject(subject)
	if strings.TrimSpace(doc.Settings.Preheader) == "" {
		t.hints = append(t.hints, Hint{Code: "preheader_missing"})
	}
	t.blocks(doc.Blocks)
	if t.images > 0 && t.textRunes < t.images*textRunesPerImage {
		t.hints = append(t.hints, Hint{Code: "image_heavy"})
	}
	if t.links > RecommendedMaxLinks {
		t.hints = append(t.hints, Hint{Code: "too_many_links"})
	}
	if t.actions == 0 {
		t.hints = append(t.hints, Hint{Code: "no_call_to_action"})
	}
	return t.hints
}

func (t *lintTally) subject(subject string) {
	if strings.TrimSpace(subject) == "" {
		return
	}
	if utf8.RuneCountInString(subject) > RecommendedSubjectRunes {
		t.hints = append(t.hints, Hint{Code: "subject_too_long"})
	}
	if shouty(stripMergeTags(subject)) {
		t.hints = append(t.hints, Hint{Code: "subject_all_caps"})
	}
	if excessPunctuation(subject) {
		t.hints = append(t.hints, Hint{Code: "subject_excess_punctuation"})
	}
}

func shouty(s string) bool {
	upper := 0
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsUpper(r) {
			upper++
		}
	}
	return upper >= minCapsLetters
}

func excessPunctuation(s string) bool {
	for _, repeated := range []string{"!!", "??", "!?", "?!"} {
		if strings.Contains(s, repeated) {
			return true
		}
	}
	return strings.Count(s, "!")+strings.Count(s, "?") > maxSubjectMarks
}

func (t *lintTally) blocks(blocks []Block) {
	for _, block := range blocks {
		t.block(block)
	}
}

func (t *lintTally) block(block Block) {
	switch {
	case block.Kind == KindHeading && block.Heading != nil:
		t.textRunes += utf8.RuneCountInString(stripMergeTags(block.Heading.Text))
	case block.Kind == KindText && block.Text != nil:
		for _, p := range block.Text.Paragraphs {
			for _, run := range p.Runs {
				t.textRunes += utf8.RuneCountInString(stripMergeTags(run.Text))
				if run.Link != nil {
					t.links++
					t.actions++
				}
			}
		}
	case block.Kind == KindButton && block.Button != nil:
		t.textRunes += utf8.RuneCountInString(stripMergeTags(block.Button.Label))
		t.links++
		t.actions++
	case block.Kind == KindImage && block.Image != nil:
		t.images++
		if strings.TrimSpace(block.Image.Alt) == "" {
			t.hints = append(t.hints, Hint{Code: "image_missing_alt", BlockID: block.ID})
		}
		if block.Image.Href != "" {
			t.links++
			t.actions++
		}
	case block.Kind == KindSocial && block.Social != nil:
		t.links += len(block.Social.Links)
	case block.Kind == KindColumns && block.Columns != nil:
		for _, column := range block.Columns.Columns {
			t.blocks(column.Blocks)
		}
	}
}
