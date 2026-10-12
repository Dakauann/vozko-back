package studio

import (
	"regexp"

	"vozko/domain/mediagen"
)

const (
	MinChromaValue = 0.001
	MaxChromaValue = 1.0
)

var chromaColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)

type ChromaKeyframes struct {
	Similarity []mediagen.Keyframe `json:"similarity,omitempty"`
	Smoothness []mediagen.Keyframe `json:"smoothness,omitempty"`
	Spill      []mediagen.Keyframe `json:"spill,omitempty"`
}

type ChromaKey struct {
	Color      string           `json:"color"`
	Similarity float64          `json:"similarity"`
	Smoothness float64          `json:"smoothness"`
	Spill      float64          `json:"spill"`
	Keyframes  *ChromaKeyframes `json:"keyframes,omitempty"`
}

func chromaHolds(v float64) bool { return within(v, MinChromaValue, MaxChromaValue) }

func (k *ChromaKeyframes) issue() string {
	if k == nil {
		return ""
	}
	for _, lane := range [][]mediagen.Keyframe{k.Similarity, k.Smoothness, k.Spill} {
		if code := keysIssue(lane, MinChromaValue, MaxChromaValue); code != "" {
			return code
		}
	}
	return ""
}

func (k ChromaKey) issue() string {
	if !chromaColor.MatchString(k.Color) {
		return CodeInvalid
	}
	if !chromaHolds(k.Similarity) || !chromaHolds(k.Smoothness) || !chromaHolds(k.Spill) {
		return CodeOutOfRange
	}
	return k.Keyframes.issue()
}

func (c Clip) chromaIssue() string {
	if c.ChromaKey == nil {
		return ""
	}
	if c.Type != ClipVideo && c.Type != ClipImage {
		return CodeInvalid
	}
	return c.ChromaKey.issue()
}

func (l Layer) stillChromaIssue() string {
	if l.ChromaKey == nil {
		return ""
	}
	if l.ChromaKey.Keyframes != nil {
		return CodeInvalid
	}
	return l.ChromaKey.issue()
}
