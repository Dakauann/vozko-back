package emaildesign

import (
	"regexp"
	"strings"
)

const plainDivider = "----------"

var blankLineRuns = regexp.MustCompile(`\n{3,}`)

func labelledLink(label, href string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return href
	}
	return label + " (" + href + ")"
}

func (r *renderer) plainDocument(doc Document) string {
	var chunks []string
	if r.in.ViewInBrowserURL != "" {
		chunks = append(chunks, labelledLink(r.in.ViewInBrowserLabel, r.in.ViewInBrowserURL))
	}
	chunks = append(chunks, r.plainBlocks(doc.Blocks)...)
	kept := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(strings.ReplaceAll(chunk, "\r", ""))
		if chunk != "" {
			kept = append(kept, chunk)
		}
	}
	return blankLineRuns.ReplaceAllString(strings.Join(kept, "\n\n"), "\n\n") + "\n"
}

func (r *renderer) plainBlocks(blocks []Block) []string {
	var chunks []string
	for _, block := range blocks {
		chunks = append(chunks, r.plainBlock(block)...)
	}
	return chunks
}

func (r *renderer) plainBlock(block Block) []string {
	switch block.Kind {
	case KindHeading:
		return []string{r.plain(block.Heading.Text)}
	case KindText:
		return r.plainText(*block.Text)
	case KindButton:
		return []string{labelledLink(r.plain(block.Button.Label), r.link(block.Button.Href))}
	case KindImage:
		alt := r.plain(block.Image.Alt)
		if block.Image.Href == "" {
			return []string{alt}
		}
		return []string{labelledLink(alt, r.link(block.Image.Href))}
	case KindDivider:
		return []string{plainDivider}
	case KindColumns:
		var chunks []string
		for _, column := range block.Columns.Columns {
			chunks = append(chunks, r.plainBlocks(column.Blocks)...)
		}
		return chunks
	case KindSocial:
		lines := make([]string, 0, len(block.Social.Links))
		for _, l := range block.Social.Links {
			lines = append(lines, labelledLink(r.plain(l.displayLabel()), r.link(l.URL)))
		}
		return []string{strings.Join(lines, "\n")}
	case KindFooter:
		f := block.Footer
		chunks := []string{
			r.plain(f.Text),
			strings.TrimSpace(r.plain(f.CompanyName)) + "\n" + strings.TrimSpace(r.plain(f.Address)),
		}
		if r.in.Transactional {
			return chunks
		}
		if r.in.InertUnsubscribe {
			return append(chunks, strings.TrimSpace(r.in.UnsubscribeLabel))
		}
		return append(chunks, labelledLink(r.in.UnsubscribeLabel, r.in.UnsubscribeURL))
	}
	return nil
}

func (r *renderer) plainText(t Text) []string {
	paragraphs := make([]string, 0, len(t.Paragraphs))
	for _, p := range t.Paragraphs {
		var b strings.Builder
		for _, run := range p.Runs {
			text := r.plain(run.Text)
			if run.Link != nil {
				href := r.link(run.Link.Href)
				leading := text[:len(text)-len(strings.TrimLeft(text, " "))]
				trailing := text[len(strings.TrimRight(text, " ")):]
				text = leading + labelledLink(text, href) + trailing
			}
			b.WriteString(text)
		}
		paragraphs = append(paragraphs, b.String())
	}
	return paragraphs
}
