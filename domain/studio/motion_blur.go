package studio

const (
	MinShutterAngle     = 0.0
	MaxShutterAngle     = 720.0
	MinShutterPhase     = -360.0
	MaxShutterPhase     = 360.0
	DefaultShutterAngle = 180.0
	DefaultShutterPhase = -90.0
)

func takesMotionBlur(t ClipType) bool {
	return t == ClipVideo || t == ClipImage || t == ClipOverlay
}

func (c Clip) motionBlurIssue() string {
	if c.MotionBlur && !takesMotionBlur(c.Type) {
		return CodeInvalid
	}
	return ""
}

func (v VideoCanvas) shutterIssue() string {
	if v.ShutterAngle != nil && !within(*v.ShutterAngle, MinShutterAngle, MaxShutterAngle) {
		return CodeOutOfRange
	}
	if v.ShutterPhase != nil && !within(*v.ShutterPhase, MinShutterPhase, MaxShutterPhase) {
		return CodeOutOfRange
	}
	return ""
}
