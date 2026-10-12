package studio

import "slices"

const (
	TextPathArc    = "arc"
	TextPathCircle = "circle"
	TextPathWave   = "wave"
	TextPathCustom = "custom"

	MinTextPathOffset = -4.0
	MaxTextPathOffset = 4.0
	MinTextPathBend   = -1.0
	MaxTextPathBend   = 1.0
)

type TextPath struct {
	Shape   string   `json:"shape"`
	Bend    *float64 `json:"bend,omitempty"`
	Path    string   `json:"path,omitempty"`
	Offset  float64  `json:"offset"`
	Align   string   `json:"align"`
	Upright bool     `json:"upright,omitempty"`
	Reverse bool     `json:"reverse,omitempty"`
}

func TextPathShapes() []string {
	return []string{TextPathArc, TextPathCircle, TextPathWave, TextPathCustom}
}

func TextPathAligns() []string { return []string{"start", "center", "end"} }

func (p *TextPath) usesBend() bool { return p.Shape == TextPathArc || p.Shape == TextPathWave }

func (p *TextPath) shapeIssue() string {
	traced := p.Path != ""
	switch {
	case p.usesBend() != (p.Bend != nil):
		return CodeInvalid
	case p.Bend != nil && !within(*p.Bend, MinTextPathBend, MaxTextPathBend):
		return CodeOutOfRange
	case (p.Shape == TextPathCustom) != traced:
		return CodeInvalid
	case traced && !ValidPath(p.Path):
		return CodeInvalid
	}
	return ""
}

func (c Clip) pathOffsetKeys() int {
	if c.Keyframes == nil {
		return 0
	}
	return len(c.Keyframes.PathOffset)
}

func (c Clip) textPathIssue() string {
	path := c.TextPath
	switch {
	case path == nil:
		if c.pathOffsetKeys() > 0 {
			return CodeInvalid
		}
		return ""
	case !c.animatesText() || c.Layer.Curve != 0:
		return CodeInvalid
	case !slices.Contains(TextPathShapes(), path.Shape) || !slices.Contains(TextPathAligns(), path.Align):
		return CodeUnknown
	case !within(path.Offset, MinTextPathOffset, MaxTextPathOffset):
		return CodeOutOfRange
	}
	return path.shapeIssue()
}
