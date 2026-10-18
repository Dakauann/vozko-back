package buttonaction

import (
	"errors"
	"strings"
	"testing"
)

func TestAnInboundMessageIsAnExitOnlyAsTheSlashCommandOfAStoredWord(t *testing.T) {
	words := []string{"sair", "Descadastrar"}
	cases := map[string]bool{
		"/sair":          true,
		"  /SAIR ":       true,
		"/Sáir":          true,
		"/descadastrar":  true,
		"sair":           false,
		"SAIR":           false,
		"Descadastrar":   false,
		"/ sair":         false,
		"//sair":         false,
		"/sair agora":    false,
		"quero /sair":    false,
		"/sairr":         false,
		"/":              false,
		"":               false,
		"/descadastrar!": false,
	}
	for message, want := range cases {
		if got := MatchesExitWord(message, words); got != want {
			t.Errorf("%q: got %v, want %v", message, got, want)
		}
	}
}

func TestAWordStoredWithItsSlashStillMatchesOnlyTheCommand(t *testing.T) {
	if !MatchesExitWord("/sair", []string{"/sair"}) {
		t.Fatal("a word stored before the slash rule must keep matching its command")
	}
	if MatchesExitWord("sair", []string{"/sair"}) {
		t.Fatal("the word without the slash is not an exit")
	}
}

func TestMatchesExitWordWithNoConfiguredWordsNeverMatches(t *testing.T) {
	if MatchesExitWord("/sair", nil) {
		t.Fatal("no word is built in: only configured words match")
	}
}

func TestValidateExitWordChecksOnlyTheFormatOfTheWord(t *testing.T) {
	cases := map[string]error{
		"sair":                  nil,
		"/sair":                 nil,
		" /sair ":               nil,
		"x":                     nil,
		"1":                     nil,
		"SAIR":                  nil,
		"descadastrar":          nil,
		strings.Repeat("a", 30): nil,
		strings.Repeat("a", 31): ErrExitWordLength,
		"   ":                   ErrExitWordLength,
		"/":                     ErrExitWordLength,
		"":                      ErrExitWordLength,
		"nao quero":             ErrExitWordFormat,
		"sair\tagora":           ErrExitWordFormat,
		"//sair":                ErrExitWordFormat,
		"/ sair":                nil,
	}
	for word, want := range cases {
		err := ValidateExitWord(word)
		if want == nil && err != nil || want != nil && !errors.Is(err, want) {
			t.Errorf("%q: got %v, want %v", word, err, want)
		}
	}
}

func TestOnlyAShortSlashCommandCanBeAnExitWord(t *testing.T) {
	cases := map[string]bool{
		"/sair":                       true,
		" /SAIR ":                     true,
		"sair":                        false,
		"/":                           false,
		"":                            false,
		"/ sair":                      false,
		"/sair ja":                    false,
		"/" + strings.Repeat("a", 31): false,
	}
	for text, want := range cases {
		if got := MayBeExitCommand(text); got != want {
			t.Errorf("%q: got %v, want %v", text, got, want)
		}
	}
}

func TestTheFormatRefusalHasItsOwnCode(t *testing.T) {
	if got := ErrorCode(ErrExitWordFormat); got != "exit_word_format" {
		t.Fatalf("got %q", got)
	}
}
