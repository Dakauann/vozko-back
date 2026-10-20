package mercadolivre

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

type messageTextFixture struct {
	MaxRunes int `json:"maxRunes"`
	Emoji    []struct {
		Sequence    string `json:"sequence"`
		Fitzpatrick bool   `json:"fitzpatrick"`
	} `json:"emoji"`
	Cases []struct {
		Name      string   `json:"name"`
		Text      string   `json:"text"`
		Repeat    int      `json:"repeat"`
		Valid     bool     `json:"valid"`
		Error     string   `json:"error"`
		Offending []string `json:"offending"`
	} `json:"cases"`
}

func loadMessageTextFixture(t *testing.T) messageTextFixture {
	t.Helper()
	raw, err := os.ReadFile("message_text_rules.json")
	if err != nil {
		t.Fatal(err)
	}
	var f messageTextFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTheMessageLimitMatchesTheSharedFixture(t *testing.T) {
	f := loadMessageTextFixture(t)
	if f.MaxRunes != MaxMessageRunes {
		t.Fatalf("MaxMessageRunes %d, fixture %d", MaxMessageRunes, f.MaxRunes)
	}
	if len(f.Emoji) < 1000 {
		t.Fatalf("the emoji list looks truncated: %d entries", len(f.Emoji))
	}
	if len(f.Cases) == 0 {
		t.Fatal("the fixture carries no cases")
	}
}

func TestMessageTextCasesFromTheSharedFixture(t *testing.T) {
	f := loadMessageTextFixture(t)
	for _, tc := range f.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			text := tc.Text
			if tc.Repeat > 0 {
				text = strings.Repeat(tc.Text, tc.Repeat)
			}
			err := ValidateMessageText(text)
			if tc.Valid != (err == nil) {
				t.Fatalf("ValidateMessageText valid = %v, want %v (err %v)", err == nil, tc.Valid, err)
			}
			switch tc.Error {
			case "":
			case "empty":
				if !errors.Is(err, ErrTextEmpty) {
					t.Fatalf("err = %v, want ErrTextEmpty", err)
				}
			case "too_long":
				var long *TextTooLongError
				if !errors.Is(err, ErrTextTooLong) || !errors.As(err, &long) || long.Max != MaxMessageRunes {
					t.Fatalf("err = %v, want TextTooLongError", err)
				}
			case "charset":
				var charset *TextCharsetError
				if !errors.Is(err, ErrTextCharset) || !errors.As(err, &charset) {
					t.Fatalf("err = %v, want TextCharsetError", err)
				}
				got := make([]string, 0, len(charset.Runes))
				for _, r := range charset.Runes {
					got = append(got, string(r))
				}
				want := tc.Offending
				if want == nil {
					want = []string{}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("offending = %q, want %q", got, want)
				}
			default:
				t.Fatalf("fixture uses an unknown error kind %q", tc.Error)
			}
		})
	}
}

func TestMessageTextRefusesInvalidUTF8(t *testing.T) {
	err := ValidateMessageText("ok \xff")
	var charset *TextCharsetError
	if !errors.As(err, &charset) || len(charset.Runes) != 1 || charset.Runes[0] != utf8.RuneError {
		t.Fatalf("err = %v", err)
	}
}

func TestEveryListedEmojiIsAccepted(t *testing.T) {
	f := loadMessageTextFixture(t)
	for _, e := range f.Emoji {
		if err := ValidateMessageText(e.Sequence); err != nil {
			t.Errorf("%q refused: %v", e.Sequence, err)
		}
		if e.Fitzpatrick {
			if err := ValidateMessageText(e.Sequence + "\U0001F3FB"); err != nil {
				t.Errorf("%q with a skin tone refused: %v", e.Sequence, err)
			}
		}
	}
}

func TestValidateAnswerText(t *testing.T) {
	cases := []struct {
		name string
		text string
		err  error
	}{
		{"plain", "Temos sim, pode comprar.", nil},
		{"utf8 beyond latin1 is fine for answers", "Pronto … \U0001F600 €", nil},
		{"exactly the limit", strings.Repeat("a", MaxAnswerRunes), nil},
		{"limit counts runes not bytes", strings.Repeat("ç", MaxAnswerRunes), nil},
		{"over the limit", strings.Repeat("a", MaxAnswerRunes+1), ErrAnswerTooLong},
		{"empty", "", ErrAnswerEmpty},
		{"whitespace only", " \n\t ", ErrAnswerEmpty},
		{"invalid utf8", "ok \xff", ErrTextCharset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAnswerText(tc.text)
			if tc.err == nil && err != nil {
				t.Fatalf("err = %v", err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestValidateOutgoingMessage(t *testing.T) {
	cases := []struct {
		name        string
		text        string
		attachments int
		err         error
	}{
		{"text only", "Seu pedido saiu hoje.", 0, nil},
		{"text with attachment", "Segue a nota.", 1, nil},
		{"attachment without text", "", 1, nil},
		{"attachment with blank text", "  \n ", 2, nil},
		{"nothing at all", "", 0, ErrTextEmpty},
		{"blank text and no attachment", " ", 0, ErrTextEmpty},
		{"caption keeps the charset rule", "Nota \U000020AC", 1, ErrTextCharset},
		{"caption keeps the length rule", strings.Repeat("a", MaxMessageRunes+1), 1, ErrTextTooLong},
		{"too many attachments", "ok", MaxAttachmentsPerMessage + 1, ErrMediaUnsupported},
		{"negative count", "ok", -1, ErrMediaUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOutgoingMessage(tc.text, tc.attachments)
			if tc.err == nil && err != nil {
				t.Fatalf("err = %v", err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}
