package studio

const (
	MinPan = -1.0
	MaxPan = 1.0
)

func (c Clip) panIssue(kind TrackKind) string {
	switch {
	case c.Pan != 0 && kind != TrackAudio:
		return CodeInvalid
	case !within(c.Pan, MinPan, MaxPan):
		return CodeOutOfRange
	}
	return ""
}

func (t Track) panIssue() string {
	switch {
	case t.Pan == nil:
		return ""
	case t.Kind != TrackAudio:
		return CodeInvalid
	case !within(*t.Pan, MinPan, MaxPan):
		return CodeOutOfRange
	}
	return ""
}
