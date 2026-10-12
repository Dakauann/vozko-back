package captions

type Rules struct {
	MaxLineRunes  int
	MaxLines      int
	MaxDuration   float64
	MinDuration   float64
	ReadingRate   float64
	MinGap        float64
	SentencePause float64
	PhrasePause   float64
	BreathPause   float64
	Grammar       Grammar
}

var SocialVideo = Rules{
	MaxLineRunes:  22,
	MaxLines:      2,
	MaxDuration:   7,
	MinDuration:   5.0 / 6,
	ReadingRate:   17,
	MinGap:        2.0 / 24,
	SentencePause: 1,
	PhrasePause:   0.5,
	BreathPause:   0.25,
	Grammar:       Portuguese,
}

const (
	pieceCost       = 1.0
	slackWeight     = 2.0
	clauseBreakCost = 0.0
	phraseBreakCost = 0.25
	breathBreakCost = 1.0
	openerBreakCost = 1.5
	plainBreakCost  = 3.0
	strongBinding   = 6.0
	softBinding     = 2.5
	cueBreakWeight  = 2.0
	maxMarkerWords  = 4
)
