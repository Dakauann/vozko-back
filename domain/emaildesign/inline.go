package emaildesign

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var ErrInlineMarkup = errors.New("emaildesign: invalid inline markup")

var paragraphBreak = regexp.MustCompile(`\n[ \t]*\n\s*`)

type inlineMarks struct {
	bold      bool
	italic    bool
	underline bool
}

type inlineParser struct {
	runs  []Run
	text  strings.Builder
	marks inlineMarks
	link  *Link
}

func ParseInline(markup string) ([]Paragraph, error) {
	clean := strings.ReplaceAll(markup, "\r\n", "\n")
	if strings.TrimSpace(clean) == "" {
		return nil, fmt.Errorf("%w: the text is empty", ErrInlineMarkup)
	}
	var paragraphs []Paragraph
	for _, chunk := range paragraphBreak.Split(strings.Trim(clean, "\n"), -1) {
		if chunk == "" {
			continue
		}
		runs, err := parseParagraph(chunk)
		if err != nil {
			return nil, err
		}
		paragraphs = append(paragraphs, Paragraph{Runs: runs})
	}
	return paragraphs, nil
}

func parseParagraph(chunk string) ([]Run, error) {
	p := &inlineParser{}
	chars := []rune(chunk)
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		switch {
		case c == '\\' && i+1 < len(chars) && strings.ContainsRune(`\*_[]`, chars[i+1]):
			p.text.WriteRune(chars[i+1])
			i++
		case c == '*':
			stars := 1
			for i+stars < len(chars) && chars[i+stars] == '*' {
				stars++
			}
			if stars > 3 {
				return nil, fmt.Errorf("%w: use one star for italic, two for bold, three for both", ErrInlineMarkup)
			}
			p.flush()
			if stars != 1 {
				p.marks.bold = !p.marks.bold
			}
			if stars != 2 {
				p.marks.italic = !p.marks.italic
			}
			i += stars - 1
		case c == '_' && i+1 < len(chars) && chars[i+1] == '_':
			p.flush()
			p.marks.underline = !p.marks.underline
			i++
		case c == '[':
			if p.link != nil {
				return nil, fmt.Errorf("%w: a link cannot hold another link", ErrInlineMarkup)
			}
			p.flush()
			p.link = &Link{}
		case c == ']':
			end, err := p.closeLink(chars, i)
			if err != nil {
				return nil, err
			}
			i = end
		default:
			p.text.WriteRune(c)
		}
	}
	p.flush()
	if p.link != nil {
		return nil, fmt.Errorf("%w: a link label is not closed", ErrInlineMarkup)
	}
	if p.marks != (inlineMarks{}) {
		return nil, fmt.Errorf("%w: a bold, italic or underline mark is not closed", ErrInlineMarkup)
	}
	return mergedRuns(p.runs), nil
}

func (p *inlineParser) closeLink(chars []rune, at int) (int, error) {
	if p.link == nil || at+1 >= len(chars) || chars[at+1] != '(' {
		return 0, fmt.Errorf("%w: write links as [label](https://address)", ErrInlineMarkup)
	}
	end := -1
	for j := at + 2; j < len(chars); j++ {
		if chars[j] == ')' {
			end = j
			break
		}
	}
	if end < 0 {
		return 0, fmt.Errorf("%w: a link address is not closed", ErrInlineMarkup)
	}
	p.flush()
	p.link.Href = strings.TrimSpace(string(chars[at+2 : end]))
	p.link = nil
	return end, nil
}

func (p *inlineParser) flush() {
	text := p.text.String()
	p.text.Reset()
	if text != "" {
		p.runs = append(p.runs, Run{Text: text, Bold: p.marks.bold, Italic: p.marks.italic, Underline: p.marks.underline, Link: p.link})
	}
}

func mergedRuns(runs []Run) []Run {
	out := make([]Run, 0, len(runs))
	for _, run := range runs {
		if n := len(out); n > 0 && sameStyle(out[n-1], run) {
			out[n-1].Text += run.Text
			continue
		}
		if run.Link != nil {
			run.Link = &Link{Href: run.Link.Href}
		}
		out = append(out, run)
	}
	return out
}

func sameStyle(a, b Run) bool {
	if a.Bold != b.Bold || a.Italic != b.Italic || a.Underline != b.Underline || (a.Link == nil) != (b.Link == nil) {
		return false
	}
	return a.Link == nil || a.Link.Href == b.Link.Href
}

func FormatInline(paragraphs []Paragraph) string {
	chunks := make([]string, 0, len(paragraphs))
	for _, p := range paragraphs {
		chunks = append(chunks, formatParagraph(p.Runs))
	}
	return strings.Join(chunks, "\n\n")
}

func formatParagraph(runs []Run) string {
	var b strings.Builder
	open := inlineMarks{}
	var link *Link
	for i, run := range runs {
		if i > 0 && sameStyle(runs[i-1], run) {
			b.WriteString(escapeInline(run.Text))
			continue
		}
		if link != nil && (run.Link == nil || run.Link.Href != link.Href) {
			b.WriteString(toggles(open, inlineMarks{}))
			open = inlineMarks{}
			b.WriteString("](" + strings.ReplaceAll(link.Href, ")", "%29") + ")")
			link = nil
		}
		want := inlineMarks{bold: run.Bold, italic: run.Italic, underline: run.Underline}
		if run.Link != nil && link == nil {
			b.WriteString(toggles(open, inlineMarks{}))
			open = inlineMarks{}
			b.WriteString("[")
			link = run.Link
		}
		b.WriteString(toggles(open, want))
		open = want
		b.WriteString(escapeInline(run.Text))
	}
	b.WriteString(toggles(open, inlineMarks{}))
	if link != nil {
		b.WriteString("](" + strings.ReplaceAll(link.Href, ")", "%29") + ")")
	}
	return b.String()
}

func toggles(from, to inlineMarks) string {
	out := ""
	if from.underline != to.underline {
		out += "__"
	}
	switch {
	case from.bold != to.bold && from.italic != to.italic:
		out += "***"
	case from.bold != to.bold:
		out += "**"
	case from.italic != to.italic:
		out += "*"
	}
	return out
}

func escapeInline(text string) string {
	var b strings.Builder
	chars := []rune(text)
	for i, c := range chars {
		switch {
		case strings.ContainsRune(`\*[]`, c):
			b.WriteRune('\\')
		case c == '_' && i+1 < len(chars) && chars[i+1] == '_':
			b.WriteRune('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}
