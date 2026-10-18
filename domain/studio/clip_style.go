package studio

import "vozko/domain/mediagen"

type EffectKeyframes struct {
	Opacity []mediagen.Keyframe `json:"opacity,omitempty"`
	X       []mediagen.Keyframe `json:"x,omitempty"`
	Y       []mediagen.Keyframe `json:"y,omitempty"`
	Blur    []mediagen.Keyframe `json:"blur,omitempty"`
	Spread  []mediagen.Keyframe `json:"spread,omitempty"`
}

var mediaEffectTypes = map[string]bool{EffectDropShadow: true, EffectOuterGlow: true}

func (k *EffectKeyframes) issue() string {
	if k == nil {
		return ""
	}
	lanes := []struct {
		frames    []mediagen.Keyframe
		low, high float64
	}{
		{k.Opacity, 0, 1},
		{k.X, -MaxEffectOffset, MaxEffectOffset},
		{k.Y, -MaxEffectOffset, MaxEffectOffset},
		{k.Blur, 0, MaxEffectBlur},
		{k.Spread, -MaxEffectSpread, MaxEffectSpread},
	}
	for _, lane := range lanes {
		if code := keysIssue(lane.frames, lane.low, lane.high); code != "" {
			return code
		}
	}
	return ""
}

func effectKeysIssue(effects []Effect) string {
	for _, e := range effects {
		if code := e.Keyframes.issue(); code != "" {
			return code
		}
	}
	return ""
}

func withoutEffectKeys(effects []Effect) []Effect {
	if effects == nil {
		return nil
	}
	plain := make([]Effect, len(effects))
	for i, e := range effects {
		e.Keyframes = nil
		plain[i] = e
	}
	return plain
}

func (l Layer) withoutEffectKeys() Layer {
	l.Effects = withoutEffectKeys(l.Effects)
	return l
}

func (c Clip) styled() bool {
	return len(c.Effects)+len(c.Strokes) > 0
}

func (c Clip) styleIssue() string {
	if !c.styled() {
		return ""
	}
	if c.Type != ClipVideo && c.Type != ClipImage {
		return CodeInvalid
	}
	for _, e := range c.Effects {
		if !mediaEffectTypes[e.Type] {
			return CodeInvalid
		}
	}
	seen := make(map[string]bool, len(c.Effects))
	for _, e := range withoutEffectKeys(c.Effects) {
		if code := e.issue(seen); code != "" {
			return code
		}
	}
	if code := effectKeysIssue(c.Effects); code != "" {
		return code
	}
	for _, s := range c.Strokes {
		if code := s.issue(true); code != "" {
			return code
		}
	}
	return ""
}
