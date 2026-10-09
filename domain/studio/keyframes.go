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
	Blur   []mediagen.Keyframe `json:"blur,omitempty"`
	Fill   []ColorKeyframe     `json:"fill,omitempty"`
	Stroke []ColorKeyframe     `json:"stroke,omitempty"`
}

func (k *ClipKeyframes) count() int {
	if k == nil {
		return 0
	}
	return k.Keyframes.Count() + len(k.Blur) + len(k.Fill) + len(k.Stroke)
}

func colorKeysIssue(frames []ColorKeyframe) string {
	if len(frames) > mediagen.MaxKeyframesPerProperty {
		return CodeTooMany
	}
	for i, f := range frames {
		switch {
		case !f.Easing.Known():
			return CodeUnknown
		case f.AtMS < -mediagen.MaxVideoMS || f.AtMS > mediagen.MaxVideoMS:
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
		if code := mediagen.KeyframesIssue(&k.Keyframes, box); code != "" {
			return code
		}
	}
	if code := mediagen.PropertyIssue(k.Blur, 0, MaxClipBlur); code != "" {
		return code
	}
	if code := colorKeysIssue(k.Fill); code != "" {
		return code
	}
	return colorKeysIssue(k.Stroke)
}
