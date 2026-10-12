package stt

import (
	"strings"
	"unicode/utf8"
)

func (t Transcription) TimedWords() []Word {
	var words []Word
	for _, segment := range t.Segments {
		if len(segment.Words) > 0 {
			words = append(words, segment.Words...)
			continue
		}
		words = append(words, segment.spreadWords()...)
	}
	return words
}

func (s Segment) spreadWords() []Word {
	texts := strings.Fields(s.Text)
	if len(texts) == 0 || s.End <= s.Start {
		return nil
	}
	weights := make([]float64, len(texts))
	total := 0.0
	for i, text := range texts {
		weights[i] = float64(utf8.RuneCountInString(text) + 1)
		total += weights[i]
	}
	words := make([]Word, len(texts))
	at := s.Start
	for i, text := range texts {
		end := s.End
		if i < len(texts)-1 {
			end = at + (s.End-s.Start)*weights[i]/total
		}
		words[i] = Word{Text: text, Start: at, End: end}
		at = end
	}
	return words
}
