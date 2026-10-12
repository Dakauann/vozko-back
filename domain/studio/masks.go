package studio

import (
	"slices"

	"vozko/domain/mediagen"
)

const (
	MinMaskPoints   = 3
	MaxMaskPoints   = 64
	MaxMaskFeather  = 0.5
	MaxMaskExpand   = 0.5
	MinMaskInner    = 0.05
	MinMaskSize     = 0.001
	MaxMaskSize     = 4.0
	MaxMaskRotation = 3600.0
)

var (
	maskShapes = []string{"rectangle", "ellipse", "linear", "mirror", "star", "heart", "triangle", "path"}
	maskModes  = []string{"add", "subtract", "intersect"}
)

type MaskKeyframes struct {
	X         []mediagen.Keyframe `json:"x,omitempty"`
	Y         []mediagen.Keyframe `json:"y,omitempty"`
	W         []mediagen.Keyframe `json:"w,omitempty"`
	H         []mediagen.Keyframe `json:"h,omitempty"`
	Rotation  []mediagen.Keyframe `json:"rotation,omitempty"`
	Roundness []mediagen.Keyframe `json:"roundness,omitempty"`
	Inner     []mediagen.Keyframe `json:"inner,omitempty"`
	Feather   []mediagen.Keyframe `json:"feather,omitempty"`
	Expansion []mediagen.Keyframe `json:"expansion,omitempty"`
	Opacity   []mediagen.Keyframe `json:"opacity,omitempty"`
	Path      []MorphKeyframe     `json:"path,omitempty"`
}

type Mask struct {
	ID        string         `json:"id"`
	Shape     string         `json:"shape"`
	Mode      string         `json:"mode"`
	X         float64        `json:"x"`
	Y         float64        `json:"y"`
	W         float64        `json:"w"`
	H         float64        `json:"h"`
	Rotation  float64        `json:"rotation"`
	Roundness float64        `json:"roundness,omitempty"`
	Points    int            `json:"points,omitempty"`
	Inner     float64        `json:"inner,omitempty"`
	Path      string         `json:"path,omitempty"`
	Feather   float64        `json:"feather"`
	Expansion float64        `json:"expansion"`
	Opacity   float64        `json:"opacity"`
	Invert    bool           `json:"invert,omitempty"`
	Keyframes *MaskKeyframes `json:"keyframes,omitempty"`
}

type maskRange struct{ low, high float64 }

var maskRanges = struct{ position, size, rotation, roundness, inner, feather, expansion, opacity maskRange }{
	position:  maskRange{-1, 2},
	size:      maskRange{MinMaskSize, MaxMaskSize},
	rotation:  maskRange{-MaxMaskRotation, MaxMaskRotation},
	roundness: maskRange{0, 1},
	inner:     maskRange{MinMaskInner, 1},
	feather:   maskRange{0, MaxMaskFeather},
	expansion: maskRange{-MaxMaskExpand, MaxMaskExpand},
	opacity:   maskRange{0, 1},
}

func (r maskRange) holds(v float64) bool { return within(v, r.low, r.high) }

func (k *MaskKeyframes) issue() string {
	if k == nil {
		return ""
	}
	lanes := []struct {
		frames []mediagen.Keyframe
		r      maskRange
	}{
		{k.X, maskRanges.position}, {k.Y, maskRanges.position}, {k.W, maskRanges.size}, {k.H, maskRanges.size},
		{k.Rotation, maskRanges.rotation}, {k.Roundness, maskRanges.roundness}, {k.Inner, maskRanges.inner},
		{k.Feather, maskRanges.feather}, {k.Expansion, maskRanges.expansion}, {k.Opacity, maskRanges.opacity},
	}
	for _, lane := range lanes {
		if code := keysIssue(lane.frames, lane.r.low, lane.r.high); code != "" {
			return code
		}
	}
	return ""
}

func (m Mask) issue() string {
	if !slices.Contains(maskShapes, m.Shape) || !slices.Contains(maskModes, m.Mode) {
		return CodeUnknown
	}
	star := m.Shape == "star"
	if (!star && (m.Points != 0 || m.Inner != 0)) || (m.Shape != "rectangle" && m.Roundness != 0) {
		return CodeInvalid
	}
	if (m.Shape == "path") != (m.Path != "") || (m.Path != "" && !ValidPath(m.Path)) {
		return CodeInvalid
	}
	if star && m.Points != 0 && (m.Points < MinMaskPoints || m.Points > MaxMaskPoints) {
		return CodeOutOfRange
	}
	switch {
	case !maskRanges.position.holds(m.X) || !maskRanges.position.holds(m.Y) || !maskRanges.size.holds(m.W) || !maskRanges.size.holds(m.H):
		return CodeOutOfRange
	case !maskRanges.rotation.holds(m.Rotation) || !maskRanges.roundness.holds(m.Roundness) || (m.Inner != 0 && !maskRanges.inner.holds(m.Inner)):
		return CodeOutOfRange
	case !maskRanges.feather.holds(m.Feather) || !maskRanges.expansion.holds(m.Expansion) || !maskRanges.opacity.holds(m.Opacity):
		return CodeOutOfRange
	}
	if code := m.morphIssue(); code != "" {
		return code
	}
	return m.Keyframes.issue()
}

func masksIssue(masks []Mask) string {
	seen := make(map[string]bool, len(masks))
	for _, m := range masks {
		if !validToken(m.ID) || seen[m.ID] {
			return CodeInvalid
		}
		seen[m.ID] = true
		if code := m.issue(); code != "" {
			return code
		}
	}
	return ""
}
