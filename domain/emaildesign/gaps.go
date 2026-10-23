package emaildesign

import (
	"strings"

	"vozko/domain/shared"
)

const maxGapTextRunes = 40

type Gap struct {
	BlockID string    `json:"block_id,omitempty"`
	Kind    BlockKind `json:"kind,omitempty"`
	Text    string    `json:"text,omitempty"`
	Code    string    `json:"code"`
	Field   string    `json:"field,omitempty"`
}

func OpenGaps(doc Document) []Gap {
	issues := Validate(doc)
	gaps := make([]Gap, 0, len(issues))
	for _, issue := range issues {
		gap := Gap{BlockID: issue.BlockID, Code: issue.Code, Field: issue.Field}
		if block, ok := FindBlock(doc.Blocks, issue.BlockID); ok && issue.BlockID != "" {
			gap.Kind, gap.Text = block.Kind, excerpt(blockText(block))
		}
		gaps = append(gaps, gap)
	}
	return gaps
}

func FindBlock(blocks []Block, id string) (Block, bool) {
	for _, b := range blocks {
		if b.ID == id {
			return b, true
		}
		if b.Kind != KindColumns || b.Columns == nil {
			continue
		}
		for _, column := range b.Columns.Columns {
			if found, ok := FindBlock(column.Blocks, id); ok {
				return found, true
			}
		}
	}
	return Block{}, false
}

func blockText(b Block) string {
	switch {
	case b.Heading != nil:
		return b.Heading.Text
	case b.Text != nil:
		var parts []string
		for _, p := range b.Text.Paragraphs {
			for _, run := range p.Runs {
				parts = append(parts, run.Text)
			}
		}
		return strings.Join(parts, "")
	case b.Button != nil:
		return b.Button.Label
	case b.Image != nil:
		return b.Image.Alt
	case b.Footer != nil:
		return b.Footer.CompanyName
	}
	return ""
}

func excerpt(text string) string {
	clipped, cut := shared.TruncateRunes(strings.Join(strings.Fields(text), " "), maxGapTextRunes)
	if cut {
		return strings.TrimSpace(clipped) + "…"
	}
	return clipped
}

func SubjectGaps(subject string) []Gap {
	issues := ValidateSubject(subject)
	gaps := make([]Gap, 0, len(issues))
	for _, issue := range issues {
		gaps = append(gaps, Gap{Code: issue.Code, Field: issue.Field})
	}
	return gaps
}
