package studio

import "unicode/utf8"

type TextRun struct {
	Start         int      `json:"start"`
	End           int      `json:"end"`
	Fill          string   `json:"fill,omitempty"`
	FontID        string   `json:"fontId,omitempty"`
	FontWeight    int      `json:"fontWeight,omitempty"`
	FontSize      float64  `json:"fontSize,omitempty"`
	Italic        *bool    `json:"italic,omitempty"`
	Underline     bool     `json:"underline,omitempty"`
	Caps          bool     `json:"caps,omitempty"`
	LetterSpacing *float64 `json:"letterSpacing,omitempty"`
}

func (r TextRun) styled() bool {
	return r.Fill != "" || r.FontID != "" || r.FontWeight != 0 || r.FontSize != 0 || r.Italic != nil || r.Underline || r.Caps || r.LetterSpacing != nil
}

func (r TextRun) valueIssue() string {
	switch {
	case r.Fill != "" && !validColor(r.Fill, true):
		return CodeInvalid
	case r.FontID != "" && !fonts[r.FontID]:
		return CodeUnknown
	case r.FontWeight != 0 && (r.FontWeight < 100 || r.FontWeight > 900 || r.FontWeight%100 != 0):
		return CodeOutOfRange
	case r.FontSize != 0 && !within(r.FontSize, 0.005, 0.5):
		return CodeOutOfRange
	case r.LetterSpacing != nil && !within(*r.LetterSpacing, -0.5, 2):
		return CodeOutOfRange
	}
	return ""
}

func (l Layer) textRunsIssue(limit int) string {
	if len(l.Runs) == 0 {
		return ""
	}
	if l.Type != LayerText {
		return CodeInvalid
	}
	end := 0
	for _, run := range l.Runs {
		if run.Start < end || run.End <= run.Start || run.End > limit || !run.styled() {
			return CodeInvalid
		}
		if code := run.valueIssue(); code != "" {
			return code
		}
		end = run.End
	}
	return ""
}

func (c Clip) runLimit() int {
	if c.Counter != nil {
		return utf8.RuneCountInString(c.Counter.Prefix) + 1 + utf8.RuneCountInString(c.Counter.Suffix)
	}
	return utf8.RuneCountInString(c.Layer.Text)
}
