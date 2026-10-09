package studio

import "vozko/domain/mediagen"

const MaxClipBlur = 100.0

type ClipKeyframes struct {
	mediagen.Keyframes
	Blur []mediagen.Keyframe `json:"blur,omitempty"`
}

func (k *ClipKeyframes) count() int {
	if k == nil {
		return 0
	}
	return k.Keyframes.Count() + len(k.Blur)
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
	return mediagen.PropertyIssue(k.Blur, 0, MaxClipBlur)
}
