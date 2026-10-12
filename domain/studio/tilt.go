package studio

import "vozko/domain/mediagen"

const (
	MaxTilt            = 3_600.0
	MinPerspective     = 0.0
	MaxPerspective     = 120.0
	DefaultPerspective = 40.0
)

type TiltKeyframes struct {
	RotationX []mediagen.Keyframe `json:"rotationX,omitempty"`
	RotationY []mediagen.Keyframe `json:"rotationY,omitempty"`
}

func takesTilt(t ClipType) bool {
	return t == ClipVideo || t == ClipImage || t == ClipOverlay || t == ClipGroup
}

func (k TiltKeyframes) count() int {
	return len(k.RotationX) + len(k.RotationY)
}

func (k TiltKeyframes) issue() string {
	for _, frames := range [][]mediagen.Keyframe{k.RotationX, k.RotationY} {
		if code := keysIssue(frames, -MaxTilt, MaxTilt); code != "" {
			return code
		}
	}
	return ""
}

func (c Clip) carriesTilt() bool {
	return c.RotationX != 0 || c.RotationY != 0 || (c.Keyframes != nil && c.Keyframes.TiltKeyframes.count() > 0)
}

func (c Clip) tiltIssue() string {
	if !c.carriesTilt() {
		return ""
	}
	if !takesTilt(c.Type) {
		return CodeInvalid
	}
	if !within(c.RotationX, -MaxTilt, MaxTilt) || !within(c.RotationY, -MaxTilt, MaxTilt) {
		return CodeOutOfRange
	}
	return ""
}

func (v VideoCanvas) perspectiveIssue() string {
	if v.Perspective != nil && !within(*v.Perspective, MinPerspective, MaxPerspective) {
		return CodeOutOfRange
	}
	return ""
}
