package studio

import (
	"math"

	"vozko/domain/mediagen"
)

type VertexPin struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type MorphKeyframe struct {
	AtMS        int64           `json:"atMs"`
	Path        string          `json:"path"`
	Easing      mediagen.Easing `json:"easing"`
	FirstVertex *VertexPin      `json:"firstVertex,omitempty"`
}

type MorphKeyframes struct {
	Path []MorphKeyframe `json:"path,omitempty"`
}

func (p *VertexPin) fits() bool {
	return pathCoordinate(p.X) && pathCoordinate(p.Y)
}

func pathCoordinate(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= MaxPathCoordinate
}

func morphKeysIssue(frames []MorphKeyframe) string {
	for i, f := range frames {
		switch {
		case !f.Easing.Known():
			return CodeUnknown
		case f.AtMS < -MaxVideoMS || f.AtMS > MaxVideoMS:
			return CodeOutOfRange
		case !ValidPath(f.Path):
			return CodeInvalid
		case f.FirstVertex != nil && !f.FirstVertex.fits():
			return CodeOutOfRange
		case i > 0 && f.AtMS <= frames[i-1].AtMS:
			return CodeOutOfRange
		}
	}
	return ""
}

func (k *MorphKeyframes) count() int {
	return len(k.Path)
}

func (k *MorphKeyframes) issue() string {
	return morphKeysIssue(k.Path)
}

func (c Clip) morphIssue() string {
	if c.Keyframes == nil || len(c.Keyframes.Path) == 0 {
		return ""
	}
	if c.Type != ClipOverlay || c.Layer == nil || c.Layer.Type != LayerShape || c.Layer.Shape != ShapePath {
		return CodeInvalid
	}
	return ""
}

func (m Mask) morphIssue() string {
	if m.Keyframes == nil || len(m.Keyframes.Path) == 0 {
		return ""
	}
	if m.Shape != "path" {
		return CodeInvalid
	}
	return morphKeysIssue(m.Keyframes.Path)
}
