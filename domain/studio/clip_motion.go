package studio

import (
	"slices"

	"vozko/domain/mediagen"
)

const (
	MinAnchor     = -1.0
	MaxAnchor     = 2.0
	MaxPathHandle = 2.0
	maxAxisBox    = 4.0
)

const (
	SpatialLinear = "linear"
	SpatialAuto   = "auto"
	SpatialSmooth = "smooth"
	SpatialFree   = "free"
)

var spatialModes = []string{"", SpatialLinear, SpatialAuto, SpatialSmooth, SpatialFree}

type Anchor struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Tangent struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type PositionKeyframe struct {
	AtMS    int64           `json:"atMs"`
	X       float64         `json:"x"`
	Y       float64         `json:"y"`
	Easing  mediagen.Easing `json:"easing"`
	Spatial string          `json:"spatial,omitempty"`
	In      *Tangent        `json:"in,omitempty"`
	Out     *Tangent        `json:"out,omitempty"`
}

func (t *Tangent) holds() bool {
	return within(t.X, -MaxPathHandle, MaxPathHandle) && within(t.Y, -MaxPathHandle, MaxPathHandle)
}

func (f PositionKeyframe) handled() bool {
	return f.Spatial == SpatialSmooth || f.Spatial == SpatialFree
}

func (f PositionKeyframe) issue() string {
	switch {
	case !f.Easing.Known():
		return CodeUnknown
	case f.AtMS < -MaxVideoMS || f.AtMS > MaxVideoMS:
		return CodeOutOfRange
	case !within(f.X, mediagen.MinKeyframePosition, mediagen.MaxKeyframePosition) || !within(f.Y, mediagen.MinKeyframePosition, mediagen.MaxKeyframePosition):
		return CodeOutOfRange
	case !slices.Contains(spatialModes, f.Spatial):
		return CodeUnknown
	case f.handled() != (f.In != nil && f.Out != nil) || (!f.handled() && (f.In != nil || f.Out != nil)):
		return CodeInvalid
	case f.In != nil && !f.In.holds(), f.Out != nil && !f.Out.holds():
		return CodeOutOfRange
	}
	return ""
}

func positionKeysIssue(frames []PositionKeyframe) string {
	for i, f := range frames {
		if code := f.issue(); code != "" {
			return code
		}
		if i > 0 && f.AtMS <= frames[i-1].AtMS {
			return CodeOutOfRange
		}
	}
	return ""
}

func (k *ClipKeyframes) rivalIssue() string {
	if k == nil {
		return ""
	}
	if len(k.Scale) > 0 && (len(k.ScaleX) > 0 || len(k.ScaleY) > 0) {
		return CodeInvalid
	}
	if len(k.Position) > 0 && (len(k.X) > 0 || len(k.Y) > 0) {
		return CodeInvalid
	}
	return ""
}

func axisReach(frames []mediagen.Keyframe) float64 {
	if len(frames) == 0 {
		return 1
	}
	_, high := mediagen.CurveExtent(frames)
	return high
}

func (k *ClipKeyframes) axisIssue(box mediagen.Transform) string {
	for _, frames := range [][]mediagen.Keyframe{k.ScaleX, k.ScaleY} {
		if code := keysIssue(frames, mediagen.MinKeyframeScale, mediagen.MaxKeyframeScale); code != "" {
			return code
		}
	}
	if box.W*axisReach(k.ScaleX) > maxAxisBox || box.H*axisReach(k.ScaleY) > maxAxisBox {
		return CodeOutOfRange
	}
	return ""
}

func (c Clip) placed() bool {
	return c.Anchor != nil || c.FlipX || c.FlipY || c.AutoOrient
}

func (c Clip) placementIssue() string {
	if c.Anchor == nil {
		return ""
	}
	if !within(c.Anchor.X, MinAnchor, MaxAnchor) || !within(c.Anchor.Y, MinAnchor, MaxAnchor) {
		return CodeOutOfRange
	}
	return ""
}
