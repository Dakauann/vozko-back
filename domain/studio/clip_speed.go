package studio

const (
	MinClipSpeed = 0.1
	MaxClipSpeed = 16.0
)

func (c Clip) hasSourceTime() bool {
	return c.Type == ClipVideo || c.Type == ClipAudio
}

func (c Clip) speedIssue() string {
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
	return ""
}
