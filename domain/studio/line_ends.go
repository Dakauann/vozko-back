package studio

const (
	LineEndArrow     = "arrow"
	LineEndOpenArrow = "openArrow"
	LineEndRound     = "round"
	LineEndSquare    = "square"
	LineEndBar       = "bar"
	LineEndDot       = "dot"
	LineEndDiamond   = "diamond"
)

var lineEnds = map[string]bool{"": true, LineEndArrow: true, LineEndOpenArrow: true, LineEndRound: true, LineEndSquare: true, LineEndBar: true, LineEndDot: true, LineEndDiamond: true}

func (l Layer) lineEndIssue() string {
	if !lineEnds[l.ArrowStartKind] || !lineEnds[l.ArrowEndKind] {
		return CodeUnknown
	}
	if (l.ArrowStartKind != "" && !l.ArrowStart) || (l.ArrowEndKind != "" && !l.ArrowEnd) {
		return CodeInvalid
	}
	return ""
}
