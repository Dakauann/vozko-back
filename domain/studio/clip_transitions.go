package studio

import "vozko/domain/mediagen"

type ClipTransition struct {
	Kind       TransitionKind  `json:"kind"`
	DurationMS int64           `json:"durationMs"`
	Easing     mediagen.Easing `json:"easing"`
	TransitionOptions
}

func edgeOptions(spec transitionSpec) map[option]bool {
	allowed := make(map[option]bool, len(spec.options))
	for o, on := range spec.options {
		allowed[o] = on && o != optionMoving
	}
	return allowed
}

func (t ClipTransition) issue() string {
	spec, known := transitionSpecs[t.Kind]
	switch {
	case !known || spec.sound || t.Kind == TransitionDissolve:
		return CodeUnknown
	case t.DurationMS < MinTransitionMS || t.DurationMS > MaxTransitionMS:
		return CodeOutOfRange
	case !t.Easing.Known() || t.Easing == mediagen.EaseHold:
		return CodeUnknown
	case !t.TransitionOptions.valid(edgeOptions(spec)):
		return CodeInvalid
	}
	return ""
}

func edgeMS(transition *ClipTransition, fadeMS int64, motion *mediagen.Motion) int64 {
	if transition != nil {
		return transition.DurationMS
	}
	if motion != nil {
		return max(fadeMS, motion.DurationMS)
	}
	return fadeMS
}

func (c Clip) edgeTransitionsIssue() string {
	if c.TransitionIn == nil && c.TransitionOut == nil {
		return ""
	}
	sides := []struct {
		transition *ClipTransition
		fadeMS     int64
		motion     *mediagen.Motion
	}{{c.TransitionIn, c.FadeInMS, c.MotionIn}, {c.TransitionOut, c.FadeOutMS, c.MotionOut}}
	for _, side := range sides {
		if side.transition == nil {
			continue
		}
		if code := side.transition.issue(); code != "" {
			return code
		}
		if side.fadeMS > 0 || side.motion != nil {
			return CodeInvalid
		}
	}
	if edgeMS(c.TransitionIn, c.FadeInMS, c.MotionIn)+edgeMS(c.TransitionOut, c.FadeOutMS, c.MotionOut) > c.DurationMS {
		return CodeOutOfRange
	}
	return ""
}
