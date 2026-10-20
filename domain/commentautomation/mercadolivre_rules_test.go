package commentautomation

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/shared"
)

func TestMercadoLivreRulesAnswerOrDeleteOnly(t *testing.T) {
	cases := []struct {
		name   string
		action Action
		want   error
	}{
		{"answer", ActionPublicReply, nil},
		{"delete", ActionDelete, nil},
		{"private reply", ActionPrivateReply, ErrActionUnsupported},
		{"hide", ActionHide, ErrActionUnsupported},
		{"like", ActionLike, ErrActionUnsupported},
	}
	for _, tc := range cases {
		rule := &Rule{Source: shared.EntryTypeMercadoLivre, Name: "Regra", Enabled: true, Match: MatchAny, Actions: []Action{tc.action},
			PublicReplyText: "Olá {{username}}", PrivateReplyText: "Olá"}
		rule.Normalize()
		err := rule.Validate()
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestMercadoLivreAnswersFitTwoThousandCharactersWithTheLongestNickname(t *testing.T) {
	name := strings.Repeat("w", mercadoLivreNicknameRunes)
	fits := strings.Repeat("ã", 2000-len(name)) + "{{username}}"
	over := strings.Repeat("a", 2001-len(name)) + "{{username}}"
	if err := replyRule(shared.EntryTypeMercadoLivre, ActionPublicReply, fits).Validate(); err != nil {
		t.Fatalf("fits: %v", err)
	}
	if err := replyRule(shared.EntryTypeMercadoLivre, ActionPublicReply, over).Validate(); !errors.Is(err, ErrReplyTooLong) {
		t.Fatalf("over: %v", err)
	}
	if err := CheckReply(shared.EntryTypeMercadoLivre, ActionPrivateReply, "oi"); !errors.Is(err, ErrReplyTooLong) {
		t.Fatalf("a private reply has no room on Mercado Livre: %v", err)
	}
}
