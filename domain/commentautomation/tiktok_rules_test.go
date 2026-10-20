package commentautomation

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
)

func TestTikTokRulesReplyHideOrLikeOnly(t *testing.T) {
	cases := []struct {
		name   string
		action Action
		want   error
	}{
		{"public reply", ActionPublicReply, nil},
		{"hide", ActionHide, nil},
		{"like", ActionLike, nil},
		{"private reply", ActionPrivateReply, ErrActionUnsupported},
		{"delete", ActionDelete, ErrActionUnsupported},
	}
	for _, tc := range cases {
		rule := &Rule{Source: shared.EntryTypeTikTok, Name: "Regra", Enabled: true, Match: MatchAny, Actions: []Action{tc.action},
			PublicReplyText: "Olá {{username}}", PrivateReplyText: "Olá"}
		rule.Normalize()
		err := rule.Validate()
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestTikTokRepliesFitTheCommentLimitWithTheLongestUsername(t *testing.T) {
	name := strings.Repeat("w", ttdomain.MaxUsernameRunes)
	fits := strings.Repeat("ã", ttdomain.MaxCommentRunes-len(name)) + "{{username}}"
	over := strings.Repeat("a", ttdomain.MaxCommentRunes+1-len(name)) + "{{username}}"
	if err := replyRule(shared.EntryTypeTikTok, ActionPublicReply, fits).Validate(); err != nil {
		t.Fatalf("fits: %v", err)
	}
	if err := replyRule(shared.EntryTypeTikTok, ActionPublicReply, over).Validate(); !errors.Is(err, ErrReplyTooLong) {
		t.Fatalf("over: %v", err)
	}
	if err := CheckReply(shared.EntryTypeTikTok, ActionPrivateReply, "oi"); !errors.Is(err, ErrReplyTooLong) {
		t.Fatalf("private replies are not available in Brazil: %v", err)
	}
}
