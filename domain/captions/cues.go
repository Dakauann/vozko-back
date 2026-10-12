package captions

import (
	"math"
	"strings"
	"unicode/utf8"

	"vozko/domain/stt"
)

type Cue struct {
	Start float64
	End   float64
	Lines []string
}

func (c Cue) Text() string {
	return strings.Join(c.Lines, "\n")
}

type draft struct {
	words []stt.Word
	lines []string
	whole bool
}

func Cues(words []stt.Word, rules Rules) []Cue {
	var drafts []draft
	for _, sentence := range rules.sentences(spokenWords(words)) {
		drafts = append(drafts, rules.phrases(sentence)...)
	}
	return rules.timed(rules.joinShortSentences(drafts))
}

func spokenWords(words []stt.Word) []stt.Word {
	var out []stt.Word
	for i := 0; i < len(words); i++ {
		if end, ok := markerEnd(words, i); ok {
			i = end
			continue
		}
		text := strings.TrimSpace(words[i].Text)
		if text == "" {
			continue
		}
		word := stt.Word{Text: text, Start: words[i].Start, End: words[i].End}
		if n := len(out); n > 0 {
			word.Start = math.Max(word.Start, out[n-1].Start)
		}
		word.End = math.Max(word.End, word.Start)
		out = append(out, word)
	}
	return out
}

func markerEnd(words []stt.Word, from int) (int, bool) {
	if !isMarkerStart(strings.TrimSpace(words[from].Text)) {
		return 0, false
	}
	for i := from; i < len(words) && i < from+maxMarkerWords; i++ {
		if isMarkerEnd(strings.TrimSpace(words[i].Text)) {
			return i, true
		}
	}
	return 0, false
}

func (r Rules) sentences(words []stt.Word) [][]stt.Word {
	var out [][]stt.Word
	start := 0
	for i, word := range words {
		last := i == len(words)-1
		if last || r.Grammar.endsSentence(word.Text) || words[i+1].Start-word.End >= r.SentencePause {
			out = append(out, words[start:i+1])
			start = i + 1
		}
	}
	return out
}

func (r Rules) phrases(sentence []stt.Word) []draft {
	spans := cheapestSplit(len(sentence), func(from, to int) (float64, bool) {
		return r.cueCost(sentence, from, to)
	})
	drafts := make([]draft, len(spans))
	for i, s := range spans {
		words := sentence[s.from:s.to]
		lines, _, _ := r.layout(words, r.MaxLines)
		drafts[i] = draft{words: words, lines: lines, whole: len(spans) == 1}
	}
	return drafts
}

func (r Rules) cueCost(sentence []stt.Word, from, to int) (float64, bool) {
	words := sentence[from:to]
	single := len(words) == 1
	if !single && words[len(words)-1].End-words[0].Start > r.MaxDuration {
		return 0, false
	}
	lines, layoutCost, ok := r.layout(words, r.MaxLines)
	if !ok {
		return 0, false
	}
	cost := pieceCost + slack(runes(lines), r.MaxLineRunes*r.MaxLines) + layoutCost
	if to < len(sentence) {
		cost += cueBreakWeight * r.breakCost(sentence[to-1], sentence[to])
	}
	return cost, true
}

func (r Rules) layout(words []stt.Word, linesLeft int) ([]string, float64, bool) {
	text := joined(words)
	var best []string
	bestCost := math.Inf(1)
	if utf8.RuneCountInString(text) <= r.MaxLineRunes || len(words) == 1 {
		best, bestCost = []string{text}, r.lineCost(text)
	}
	if linesLeft < 2 {
		return best, bestCost, best != nil
	}
	for cut := 1; cut < len(words); cut++ {
		head := joined(words[:cut])
		if utf8.RuneCountInString(head) > r.MaxLineRunes && cut > 1 {
			break
		}
		tail, tailCost, ok := r.layout(words[cut:], linesLeft-1)
		if !ok {
			continue
		}
		if cost := r.lineCost(head) + r.breakCost(words[cut-1], words[cut]) + tailCost; cost < bestCost {
			best, bestCost = append([]string{head}, tail...), cost
		}
	}
	return best, bestCost, best != nil
}

func (r Rules) lineCost(line string) float64 {
	return pieceCost + slack(utf8.RuneCountInString(line), r.MaxLineRunes)
}

func (r Rules) breakCost(before, after stt.Word) float64 {
	if endsClause(before.Text) || r.Grammar.endsSentence(before.Text) {
		return clauseBreakCost
	}
	cost := plainBreakCost
	if r.Grammar.opens(after.Text) {
		cost = math.Min(cost, openerBreakCost)
	}
	switch gap := after.Start - before.End; {
	case gap >= r.PhrasePause:
		cost = math.Min(cost, phraseBreakCost)
	case gap >= r.BreathPause:
		cost = math.Min(cost, breathBreakCost)
	}
	return cost + r.Grammar.binding(before.Text)
}

func (r Rules) joinShortSentences(drafts []draft) []draft {
	var out []draft
	for _, d := range drafts {
		if n := len(out); n > 0 && r.shareable(out[n-1], d) {
			words := append(append([]stt.Word{}, out[n-1].words...), d.words...)
			out[n-1] = draft{words: words, lines: []string{joined(words)}, whole: true}
			continue
		}
		out = append(out, d)
	}
	return out
}

func (r Rules) shareable(first, second draft) bool {
	if !first.whole || !second.whole {
		return false
	}
	if second.words[0].Start-first.words[len(first.words)-1].End >= r.PhrasePause {
		return false
	}
	if first.duration() >= r.MinDuration && second.duration() >= r.MinDuration {
		return false
	}
	return utf8.RuneCountInString(joined(first.words))+1+utf8.RuneCountInString(joined(second.words)) <= r.MaxLineRunes
}

func (r Rules) timed(drafts []draft) []Cue {
	cues := make([]Cue, len(drafts))
	for i, d := range drafts {
		start, end := d.words[0].Start, d.words[len(d.words)-1].End
		readable := start + math.Max(r.MinDuration, float64(runes(d.lines))/r.ReadingRate)
		if i+1 < len(drafts) {
			next := drafts[i+1].words[0].Start
			end = math.Min(math.Max(end, math.Min(readable, next-r.MinGap)), next)
		} else {
			end = math.Max(end, readable)
		}
		cues[i] = Cue{Start: start, End: end, Lines: d.lines}
	}
	return cues
}

func (d draft) duration() float64 {
	return d.words[len(d.words)-1].End - d.words[0].Start
}

func joined(words []stt.Word) string {
	texts := make([]string, len(words))
	for i, word := range words {
		texts[i] = word.Text
	}
	return strings.Join(texts, " ")
}

func runes(lines []string) int {
	total := len(lines) - 1
	for _, line := range lines {
		total += utf8.RuneCountInString(line)
	}
	return total
}

func slack(used, capacity int) float64 {
	unused := 1 - math.Min(float64(used)/float64(capacity), 1)
	return slackWeight * unused * unused
}
