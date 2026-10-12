package captions

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"vozko/domain/stt"
)

const (
	secondsPerRune = 0.055
	wordFloor      = 0.08
	wordGap        = 0.04
)

func spoken(t *testing.T, script string) []stt.Word {
	t.Helper()
	var words []stt.Word
	at := 0.0
	for _, token := range strings.Fields(script) {
		if strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">") {
			pause, err := strconv.ParseFloat(strings.Trim(token, "<>"), 64)
			if err != nil {
				t.Fatalf("bad pause %q", token)
			}
			at += pause
			continue
		}
		length := wordFloor + secondsPerRune*float64(utf8.RuneCountInString(token))
		words = append(words, stt.Word{Text: token, Start: at, End: at + length})
		at += length + wordGap
	}
	return words
}

func texts(cues []Cue) []string {
	out := make([]string, len(cues))
	for i, cue := range cues {
		out[i] = cue.Text()
	}
	return out
}

func flat(cue Cue) string {
	return strings.Join(cue.Lines, " ")
}

func lastWord(text string) string {
	fields := strings.Fields(text)
	return strings.ToLower(strings.Trim(fields[len(fields)-1], ",;:"))
}

func TestEverySentenceEndClosesItsCaption(t *testing.T) {
	cues := Cues(spoken(t, "Esse é o nosso novo modelo. Ele chegou ontem na loja e já está quase esgotado. Corre lá!"), SocialVideo)
	for _, cue := range cues {
		text := flat(cue)
		for _, glued := range []string{"modelo. Ele", "esgotado. Corre"} {
			if strings.Contains(text, glued) {
				t.Fatalf("a caption carries the end of one sentence and the start of the next: %q in %q", glued, texts(cues))
			}
		}
	}
	if flat(cues[0]) != "Esse é o nosso novo modelo." || flat(cues[len(cues)-1]) != "Corre lá!" {
		t.Fatalf("captions %q", texts(cues))
	}
}

func TestALongSentenceBreaksAtItsComma(t *testing.T) {
	cues := Cues(spoken(t, "Se você gostou do vídeo, deixa o seu like aqui embaixo."), SocialVideo)
	if len(cues) != 2 || flat(cues[0]) != "Se você gostou do vídeo," || flat(cues[1]) != "deixa o seu like aqui embaixo." {
		t.Fatalf("captions %q", texts(cues))
	}
}

func TestACommaNeverSitsInTheMiddleOfALine(t *testing.T) {
	cues := Cues(spoken(t, "Quando você chega na nossa loja pela primeira vez, a gente mostra todos os modelos disponíveis."), SocialVideo)
	ended := false
	for _, cue := range cues {
		for _, line := range cue.Lines {
			if strings.Contains(line, "vez, a") {
				t.Fatalf("the comma was ignored: %q", texts(cues))
			}
			ended = ended || strings.HasSuffix(line, "primeira vez,")
		}
	}
	if !ended {
		t.Fatalf("no line ends at the comma: %q", texts(cues))
	}
}

func TestWithoutPunctuationTheSpeakersPauseIsTheBreak(t *testing.T) {
	cues := Cues(spoken(t, "então eu peguei o carro da minha mãe <0.55> e fui direto buscar a encomenda no centro"), SocialVideo)
	if len(cues) < 2 || !strings.HasSuffix(flat(cues[0]), "minha mãe") || !strings.HasPrefix(flat(cues[1]), "e fui") {
		t.Fatalf("captions %q", texts(cues))
	}
}

func TestALongSilenceAlwaysSplitsCaptions(t *testing.T) {
	cues := Cues(spoken(t, "oi gente <1.2> bora começar"), SocialVideo)
	if len(cues) != 2 || flat(cues[0]) != "oi gente" || flat(cues[1]) != "bora começar" {
		t.Fatalf("captions %q", texts(cues))
	}
}

func TestCaptionsAndLinesNeverEndOnAWordThatBindsToTheNext(t *testing.T) {
	cues := Cues(spoken(t, "hoje eu quero mostrar para vocês como funciona o novo aplicativo da empresa e por que ele vai mudar a forma de vender pela internet no Brasil inteiro"), SocialVideo)
	if len(cues) < 3 {
		t.Fatalf("captions %q", texts(cues))
	}
	for _, cue := range cues {
		for _, line := range cue.Lines {
			if word := lastWord(line); SocialVideo.Grammar.binding(word) >= strongBinding {
				t.Fatalf("a line ends on %q: %q", word, texts(cues))
			}
		}
	}
}

func TestEveryCaptionFitsTheLineLimits(t *testing.T) {
	cues := Cues(spoken(t, "A promoção vale para todos os produtos da vitrine, inclusive os lançamentos da semana passada, e termina no domingo à noite. Aproveita, porque o estoque é limitado e a procura está enorme."), SocialVideo)
	for _, cue := range cues {
		if len(cue.Lines) == 0 || len(cue.Lines) > SocialVideo.MaxLines {
			t.Fatalf("lines %q", cue.Lines)
		}
		for _, line := range cue.Lines {
			if utf8.RuneCountInString(line) > SocialVideo.MaxLineRunes {
				t.Fatalf("line %q is too long in %q", line, texts(cues))
			}
		}
	}
}

func TestAShortCaptionStaysOnOneLine(t *testing.T) {
	cues := Cues(spoken(t, "Vem com a gente."), SocialVideo)
	if len(cues) != 1 || len(cues[0].Lines) != 1 {
		t.Fatalf("captions %q", texts(cues))
	}
}

func TestTheLineBreakInsideACaptionFollowsTheComma(t *testing.T) {
	cues := Cues(spoken(t, "Olá, meu nome é Fernanda."), SocialVideo)
	if len(cues) != 1 || len(cues[0].Lines) != 2 || cues[0].Lines[0] != "Olá," || cues[0].Lines[1] != "meu nome é Fernanda." {
		t.Fatalf("captions %q", texts(cues))
	}
}

func TestTwoVeryShortSentencesShareOneCaption(t *testing.T) {
	cues := Cues(spoken(t, "Oi. Tudo bem? <0.6> Hoje tem novidade na loja."), SocialVideo)
	if len(cues) != 2 || cues[0].Text() != "Oi. Tudo bem?" {
		t.Fatalf("captions %q", texts(cues))
	}
}

func TestAQuickWordIsHeldLongEnoughToRead(t *testing.T) {
	cues := Cues([]stt.Word{{Text: "Obrigado!", Start: 1, End: 1.3}}, SocialVideo)
	if len(cues) != 1 || cues[0].End-cues[0].Start < SocialVideo.MinDuration {
		t.Fatalf("captions %+v", cues)
	}
}

func TestCaptionsNeverOverlap(t *testing.T) {
	words := []stt.Word{
		{Text: "Obrigado!", Start: 1, End: 1.3},
		{Text: "Até", Start: 1.5, End: 1.7},
		{Text: "amanhã,", Start: 1.72, End: 2.1},
		{Text: "pessoal.", Start: 2.12, End: 2.5},
		{Text: "Valeu.", Start: 2.45, End: 2.8},
	}
	cues := Cues(words, SocialVideo)
	if len(cues) != 3 {
		t.Fatalf("captions %q", texts(cues))
	}
	for i := 1; i < len(cues); i++ {
		if cues[i].Start < cues[i-1].End {
			t.Fatalf("overlap %+v", cues)
		}
	}
	if cues[0].End <= 1.3 || cues[0].End > cues[1].Start-SocialVideo.MinGap+1e-9 {
		t.Fatalf("the quick caption is held up to the gap before the next one: %+v", cues)
	}
}

func TestNonSpeechMarkersAreNotCaptioned(t *testing.T) {
	cues := Cues(spoken(t, "[BLANK_AUDIO] Bom dia a todos. [Música]"), SocialVideo)
	if len(cues) != 1 || cues[0].Text() != "Bom dia a todos." {
		t.Fatalf("captions %q", texts(cues))
	}
}

func TestAWordLongerThanALineIsStillCaptioned(t *testing.T) {
	cues := Cues(spoken(t, "inconstitucionalissimamente"), SocialVideo)
	if len(cues) != 1 || cues[0].Text() != "inconstitucionalissimamente" {
		t.Fatalf("captions %q", texts(cues))
	}
}

func TestNoSpeechGivesNoCaptions(t *testing.T) {
	if cues := Cues(nil, SocialVideo); len(cues) != 0 {
		t.Fatalf("captions %+v", cues)
	}
	if cues := Cues([]stt.Word{{Text: "  ", Start: 0, End: 1}}, SocialVideo); len(cues) != 0 {
		t.Fatalf("captions %+v", cues)
	}
}

func TestCaptionsKeepTheSpokenTiming(t *testing.T) {
	words := spoken(t, "Esse é o nosso novo modelo. <0.8> Ele chegou ontem.")
	cues := Cues(words, SocialVideo)
	if len(cues) != 2 || cues[0].Start != words[0].Start || cues[1].Start != words[6].Start || cues[1].End != words[len(words)-1].End {
		t.Fatalf("captions %+v words %+v", cues, words)
	}
}

func TestTimingRunsForwardEvenWhenWordsArriveOutOfOrder(t *testing.T) {
	cues := Cues([]stt.Word{{Text: "Bom", Start: 2, End: 2.3}, {Text: "dia.", Start: 1.9, End: 1.8}}, SocialVideo)
	if len(cues) != 1 || cues[0].Start != 2 || cues[0].End < cues[0].Start {
		t.Fatalf("captions %+v", cues)
	}
}
