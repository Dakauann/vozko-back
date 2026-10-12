package studio

import "vozko/domain/mediagen"

const (
	MinClipSpeed = 0.1
	MaxClipSpeed = 16.0
)

func (c Clip) hasSourceTime() bool {
	return c.Type == ClipVideo || c.Type == ClipAudio || c.Type == ClipGroup
}

func (c Clip) speedCurveIssue() string {
	if c.Keyframes == nil || len(c.Keyframes.Speed) == 0 {
		return ""
	}
	if (c.Type != ClipVideo && c.Type != ClipAudio) || c.Speed != nil || c.Freeze {
		return CodeInvalid
	}
	low, high := mediagen.CurveExtent(c.Keyframes.Speed)
	if low < MinClipSpeed || high > MaxClipSpeed {
		return CodeOutOfRange
	}
	return ""
}

func (c Clip) frameMixIssue() string {
	if c.FrameMix && (c.Type != ClipVideo || c.Freeze) {
		return CodeInvalid
	}
	return ""
}

func (c Clip) speedIssue() string {
	if code := c.speedCurveIssue(); code != "" {
		return code
	}
	if code := c.frameMixIssue(); code != "" {
		return code
	}
	if c.Speed == nil && !c.Reverse && !c.Freeze && c.PreservePitch == nil {
		return ""
	}
	if !c.hasSourceTime() {
		return CodeInvalid
	}
	if c.Speed != nil && !within(*c.Speed, MinClipSpeed, MaxClipSpeed) {
		return CodeOutOfRange
	}
	if c.Freeze && (c.Type != ClipVideo || c.Speed != nil || c.Reverse) {
		return CodeInvalid
	}
	if c.Type == ClipGroup && (c.Reverse || c.Freeze || c.PreservePitch != nil) {
		return CodeInvalid
	}
	return ""
}
