package studio

const (
	RepeatRadial = "radial"
	RepeatGrid   = "grid"
	RepeatMirror = "mirror"

	MaxRepeatRadius = 4096.0
	MaxRepeatGap    = 4096.0
	MaxRepeatAngle  = 180.0
	MaxRepeatSweep  = 360.0
	MaxRepeatOffset = 1.0
	MinRepeatCount  = 2
	MinRepeatCells  = 1
)

var (
	repeatDirections = map[string]bool{"": true, "cw": true, "ccw": true}
	repeatLayouts    = map[string]bool{"": true, "grid": true, "brick-row": true, "brick-column": true}
	repeatFlips      = map[string]bool{"": true, "horizontal": true, "vertical": true, "both": true}
	mirrorSides      = map[string]bool{"": true, "end": true, "start": true}
)

type Repeat struct {
	Mode           string  `json:"mode"`
	Count          int     `json:"count,omitempty"`
	Shown          int     `json:"shown,omitempty"`
	Radius         float64 `json:"radius,omitempty"`
	StartAngle     float64 `json:"startAngle,omitempty"`
	Sweep          float64 `json:"sweep,omitempty"`
	Direction      string  `json:"direction,omitempty"`
	Upright        bool    `json:"upright,omitempty"`
	ReverseOverlap bool    `json:"reverseOverlap,omitempty"`
	Rows           int     `json:"rows,omitempty"`
	Columns        int     `json:"columns,omitempty"`
	GapX           float64 `json:"gapX,omitempty"`
	GapY           float64 `json:"gapY,omitempty"`
	Layout         string  `json:"layout,omitempty"`
	Offset         float64 `json:"offset,omitempty"`
	FlipRows       string  `json:"flipRows,omitempty"`
	FlipColumns    string  `json:"flipColumns,omitempty"`
	Angle          float64 `json:"angle,omitempty"`
	Gap            float64 `json:"gap,omitempty"`
	Side           string  `json:"side,omitempty"`
}

func (r Repeat) radialSet() bool {
	return r.Count != 0 || r.Shown != 0 || r.Radius != 0 || r.StartAngle != 0 || r.Sweep != 0 || r.Direction != "" || r.Upright || r.ReverseOverlap
}

func (r Repeat) gridSet() bool {
	return r.Rows != 0 || r.Columns != 0 || r.GapX != 0 || r.GapY != 0 || r.Layout != "" || r.Offset != 0 || r.FlipRows != "" || r.FlipColumns != ""
}

func (r Repeat) mirrorSet() bool {
	return r.Angle != 0 || r.Gap != 0 || r.Side != ""
}

func (r Repeat) radialIssue() string {
	switch {
	case r.gridSet() || r.mirrorSet():
		return CodeInvalid
	case r.Count < MinRepeatCount || r.Shown < 0 || r.Shown > r.Count:
		return CodeOutOfRange
	case !within(r.Radius, 1, MaxRepeatRadius) || !within(r.StartAngle, -MaxRepeatAngle, MaxRepeatAngle):
		return CodeOutOfRange
	case r.Sweep != 0 && !within(r.Sweep, 1, MaxRepeatSweep):
		return CodeOutOfRange
	case !repeatDirections[r.Direction]:
		return CodeUnknown
	}
	return ""
}

func (r Repeat) gridIssue() string {
	switch {
	case r.radialSet() || r.mirrorSet():
		return CodeInvalid
	case r.Rows < MinRepeatCells || r.Columns < MinRepeatCells || r.Rows*r.Columns < MinRepeatCount:
		return CodeOutOfRange
	case !within(r.GapX, -MaxRepeatGap, MaxRepeatGap) || !within(r.GapY, -MaxRepeatGap, MaxRepeatGap) || !within(r.Offset, 0, MaxRepeatOffset):
		return CodeOutOfRange
	case !repeatLayouts[r.Layout] || !repeatFlips[r.FlipRows] || !repeatFlips[r.FlipColumns]:
		return CodeUnknown
	case r.Offset != 0 && r.Layout != "brick-row" && r.Layout != "brick-column":
		return CodeInvalid
	}
	return ""
}

func (r Repeat) mirrorIssue() string {
	switch {
	case r.radialSet() || r.gridSet():
		return CodeInvalid
	case !within(r.Angle, 0, MaxRepeatAngle) || !within(r.Gap, -MaxRepeatGap, MaxRepeatGap):
		return CodeOutOfRange
	case !mirrorSides[r.Side]:
		return CodeUnknown
	}
	return ""
}

func (r Repeat) issue() string {
	switch r.Mode {
	case RepeatRadial:
		return r.radialIssue()
	case RepeatGrid:
		return r.gridIssue()
	case RepeatMirror:
		return r.mirrorIssue()
	}
	return CodeUnknown
}

func repeatsIssue(groups []Group, layers []Layer) string {
	parents := make(map[string]string, len(groups))
	repeats := map[string]bool{}
	for _, g := range groups {
		parents[g.ID] = g.ParentID
		if g.Repeat != nil {
			repeats[g.ID] = true
		}
	}
	if len(repeats) == 0 {
		return ""
	}
	members := map[string]bool{}
	for _, l := range layers {
		for at := l.GroupID; at != ""; at = parents[at] {
			members[at] = true
		}
	}
	for _, g := range groups {
		if g.Repeat == nil {
			continue
		}
		if code := g.Repeat.issue(); code != "" {
			return code
		}
		if !members[g.ID] {
			return CodeInvalid
		}
		for at := g.ParentID; at != ""; at = parents[at] {
			if repeats[at] {
				return CodeInvalid
			}
		}
	}
	return ""
}
