package studio

import "vozko/domain/mediagen"

const MaxClipBlur = 100.0

type ColorKeyframe struct {
	AtMS   int64           `json:"atMs"`
	Color  string          `json:"color"`
	Easing mediagen.Easing `json:"easing"`
}

type ClipKeyframes struct {
	mediagen.Keyframes
	ScaleX     []mediagen.Keyframe `json:"scaleX,omitempty"`
	ScaleY     []mediagen.Keyframe `json:"scaleY,omitempty"`
	Position   []PositionKeyframe  `json:"position,omitempty"`
	Blur       []mediagen.Keyframe `json:"blur,omitempty"`
	GainDB     []mediagen.Keyframe `json:"gainDb,omitempty"`
	Pan        []mediagen.Keyframe `json:"pan,omitempty"`
	Fill       []ColorKeyframe     `json:"fill,omitempty"`
	Stroke     []ColorKeyframe     `json:"stroke,omitempty"`
	Counter    []mediagen.Keyframe `json:"counter,omitempty"`
	PathOffset []mediagen.Keyframe `json:"pathOffset,omitempty"`
	Speed      []mediagen.Keyframe `json:"speed,omitempty"`
	LineKeyframes
	GradeKeyframes
	MorphKeyframes
	CameraKeyframes
	TiltKeyframes
}

func (k *ClipKeyframes) soundOnly() bool {
	return k == nil || k.count() == len(k.GainDB)+len(k.Pan)+len(k.Speed)
}

func (k *ClipKeyframes) hasSound() bool {
	return k != nil && len(k.GainDB)+len(k.Pan) > 0
}

func (k *ClipKeyframes) count() int {
	if k == nil {
		return 0
	}
	return k.Keyframes.Count() + len(k.ScaleX) + len(k.ScaleY) + len(k.Position) + len(k.Blur) + len(k.GainDB) + len(k.Pan) + len(k.Fill) + len(k.Stroke) + len(k.Counter) + len(k.PathOffset) + len(k.Speed) + k.LineKeyframes.count() + k.GradeKeyframes.count() + k.MorphKeyframes.count() + k.CameraKeyframes.count() + k.TiltKeyframes.count()
}

func colorKeysIssue(frames []ColorKeyframe) string {
	for i, f := range frames {
		switch {
		case !f.Easing.Known():
			return CodeUnknown
		case f.AtMS < -MaxVideoMS || f.AtMS > MaxVideoMS:
			return CodeOutOfRange
		case !validColor(f.Color, true):
			return CodeInvalid
		case i > 0 && f.AtMS <= frames[i-1].AtMS:
			return CodeOutOfRange
		}
	}
	return ""
}

func (k *ClipKeyframes) paintIssue(c Clip) string {
	if k == nil || (len(k.Fill) == 0 && len(k.Stroke) == 0) {
		return ""
	}
	if c.Type != ClipOverlay || c.Layer == nil {
		return CodeInvalid
	}
	if (len(k.Fill) > 0 && !c.Layer.paintsFill()) || (len(k.Stroke) > 0 && !c.Layer.paintsStroke()) {
		return CodeInvalid
	}
	return ""
}

func (k *ClipKeyframes) issue(box mediagen.Transform) string {
	if k == nil {
		return ""
	}
	if k.count() == 0 {
		return CodeRequired
	}
	if k.Keyframes.Count() > 0 {
		if code := mediagen.KeyframesIssueWithin(&k.Keyframes, box, MaxVideoMS); code != "" {
			return code
		}
	}
	if code := k.rivalIssue(); code != "" {
		return code
	}
	if code := k.axisIssue(box); code != "" {
		return code
	}
	if code := positionKeysIssue(k.Position); code != "" {
		return code
	}
	if code := keysIssue(k.Blur, 0, MaxClipBlur); code != "" {
		return code
	}
	if code := keysIssue(k.GainDB, MinGainDB, MaxGainDB); code != "" {
		return code
	}
	if code := keysIssue(k.Pan, MinPan, MaxPan); code != "" {
		return code
	}
	if code := colorKeysIssue(k.Fill); code != "" {
		return code
	}
	if code := k.LineKeyframes.issue(); code != "" {
		return code
	}
	if code := keysIssue(k.Counter, MinCounterValue, MaxCounterValue); code != "" {
		return code
	}
	if code := keysIssue(k.PathOffset, MinTextPathOffset, MaxTextPathOffset); code != "" {
		return code
	}
	if code := keysIssue(k.Speed, MinClipSpeed, MaxClipSpeed); code != "" {
		return code
	}
	if code := k.GradeKeyframes.issue(); code != "" {
		return code
	}
	if code := k.MorphKeyframes.issue(); code != "" {
		return code
	}
	if code := k.CameraKeyframes.issue(); code != "" {
		return code
	}
	if code := k.TiltKeyframes.issue(); code != "" {
		return code
	}
	return colorKeysIssue(k.Stroke)
}

func keysIssue(frames []mediagen.Keyframe, low, high float64) string {
	return mediagen.PropertyIssueWithin(frames, low, high, MaxVideoMS)
}
