package mediagen

import "strconv"

type MotionEdge string

const (
	EdgeLeft   MotionEdge = "left"
	EdgeRight  MotionEdge = "right"
	EdgeTop    MotionEdge = "top"
	EdgeBottom MotionEdge = "bottom"

	MotionDistance = 0.2
)

type Motion struct {
	Edge       MotionEdge `json:"edge"`
	DurationMS int64      `json:"durationMs"`
}

func (e MotionEdge) Known() bool {
	switch e {
	case EdgeLeft, EdgeRight, EdgeTop, EdgeBottom:
		return true
	}
	return false
}

func (e MotionEdge) Direction() (float64, float64) {
	switch e {
	case EdgeLeft:
		return -1, 0
	case EdgeRight:
		return 1, 0
	case EdgeTop:
		return 0, -1
	case EdgeBottom:
		return 0, 1
	}
	return 0, 0
}

func MotionIssue(in, out *Motion, durationMS int64) string {
	var total int64
	for _, m := range []*Motion{in, out} {
		if m == nil {
			continue
		}
		if !m.Edge.Known() {
			return CodeUnknown
		}
		if m.DurationMS < MinClipMS {
			return CodeOutOfRange
		}
		total += m.DurationMS
	}
	if total > durationMS {
		return CodeOutOfRange
	}
	return ""
}

func motionPart(m *Motion) string {
	if m == nil {
		return "-"
	}
	return string(m.Edge) + ":" + strconv.FormatInt(m.DurationMS, 10)
}
