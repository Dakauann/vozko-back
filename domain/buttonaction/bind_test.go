package buttonaction

import (
	"errors"
	"testing"
)

func storedOptOutOn(text string) ButtonAction {
	return ButtonAction{
		Template:    TemplateRef{Name: "promo_outubro", Language: "pt_BR"},
		ButtonIndex: 1,
		ButtonText:  text,
		Kind:        KindOptOut,
		Version:     2,
		UpdatedBy:   "user-9",
	}
}

func TestBindingAnActionChecksTheButtonTextThePersonSaw(t *testing.T) {
	renamed := templateWith("Ofertas", "", quickReply("Falar com atendente"), quickReply("Parar"))
	stored := []ButtonAction{storedOptOutOn("SAIR")}

	cases := []struct {
		name      string
		in        Input
		want      error
		wantText  string
		wantStale bool
	}{
		{"the current text binds the action", Input{ButtonIndex: 1, ButtonText: "Parar", Kind: KindOptOut}, nil, "Parar", false},
		{"the current text ignores case and spaces", Input{ButtonIndex: 1, ButtonText: " parar ", Kind: KindOptOut}, nil, "Parar", false},
		{"the stored text keeps the untouched stale action", Input{ButtonIndex: 1, ButtonText: "SAIR", Kind: KindOptOut}, nil, "SAIR", true},
		{"a changed stale action is refused", Input{ButtonIndex: 1, ButtonText: "SAIR", Kind: KindConsentGrant}, ErrButtonActionStale, "", false},
		{"a text nobody sees is refused", Input{ButtonIndex: 1, ButtonText: "Quero mais", Kind: KindOptOut}, ErrButtonActionStale, "", false},
		{"an empty text is refused", Input{ButtonIndex: 1, Kind: KindOptOut}, ErrButtonActionStale, "", false},
		{"a new action on another button needs that button's text", Input{ButtonIndex: 0, ButtonText: "SAIR", Kind: KindOptOut}, ErrButtonActionStale, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BindButtonAction(renamed, tc.in, stored)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if got.ButtonText != tc.wantText || got.StaleAgainst(renamed) != tc.wantStale {
				t.Fatalf("action = %+v, stale %v; want text %q stale %v", got, got.StaleAgainst(renamed), tc.wantText, tc.wantStale)
			}
		})
	}
}

func TestAKeptStaleActionStaysExactlyAsStored(t *testing.T) {
	renamed := templateWith("Ofertas", "", quickReply("Falar com atendente"), quickReply("Parar"))
	stored := storedOptOutOn("SAIR")

	got, err := BindButtonAction(renamed, Input{ButtonIndex: 1, ButtonText: "SAIR", Kind: KindOptOut}, []ButtonAction{stored})
	if err != nil {
		t.Fatal(err)
	}
	if got != stored {
		t.Fatalf("kept = %+v, want the stored action %+v", got, stored)
	}
}

func TestAStaleActionOnAButtonThatIsGoneCanBeKept(t *testing.T) {
	shrunk := templateWith("Ofertas", "", quickReply("Falar com atendente"))
	stored := storedOptOutOn("SAIR")

	got, err := BindButtonAction(shrunk, Input{ButtonIndex: 1, ButtonText: "SAIR", Kind: KindOptOut}, []ButtonAction{stored})
	if err != nil || got != stored {
		t.Fatalf("kept = %+v, %v", got, err)
	}
}

func TestTheStaleRefusalHasItsCode(t *testing.T) {
	if code := ErrorCode(ErrButtonActionStale); code != "button_action_stale" {
		t.Fatalf("code = %q", code)
	}
}
