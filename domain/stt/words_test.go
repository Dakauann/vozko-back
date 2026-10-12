package stt

import (
	"math"
	"testing"
)

func TestTimedWordsUseTheTimingTheTranscriberMeasured(t *testing.T) {
	transcription := Transcription{Segments: []Segment{{
		Start: 0, End: 2, Text: "Bom dia, pessoal.",
		Words: []Word{{Text: "Bom", Start: 0.1, End: 0.3}, {Text: "dia,", Start: 0.35, End: 0.7}, {Text: "pessoal.", Start: 1.2, End: 1.8}},
	}}}
	words := transcription.TimedWords()
	if len(words) != 3 || words[2] != (Word{Text: "pessoal.", Start: 1.2, End: 1.8}) {
		t.Fatalf("words %+v", words)
	}
}

func TestTimedWordsSpreadASegmentWithoutWordTimingByLength(t *testing.T) {
	transcription := Transcription{Segments: []Segment{
		{Start: 1, End: 3, Text: " Oi   tudo bem? "},
		{Start: 3.2, End: 3.2, Text: "vazio"},
		{Start: 4, End: 5, Text: "   "},
	}}
	words := transcription.TimedWords()
	if len(words) != 3 || words[0].Text != "Oi" || words[2].Text != "bem?" {
		t.Fatalf("words %+v", words)
	}
	if words[0].Start != 1 || words[2].End != 3 || words[1].Start != words[0].End || words[2].Start != words[1].End {
		t.Fatalf("the segment time is not covered word after word: %+v", words)
	}
	if math.Abs((words[1].End-words[1].Start)-(words[0].End-words[0].Start)*5/3) > 1e-9 {
		t.Fatalf("a longer word does not take longer: %+v", words)
	}
}
