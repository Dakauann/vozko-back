package commentautomation

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/shared"
)

func replyRule(source shared.EntryType, action Action, text string) *Rule {
	r := &Rule{Source: source, Name: "Resposta", Enabled: true, Match: MatchAny, Actions: []Action{action}}
	if action == ActionPublicReply {
		r.PublicReplyText = text
	} else {
		r.PrivateReplyText = text
	}
	r.Normalize()
	return r
}

func TestEverySourceWithActionsHasReplyLimits(t *testing.T) {
	for source, actions := range sourceActions {
		limits, ok := sourceLimits[source]
		if !ok {
			t.Errorf("%s has actions but no reply limits", source)
			continue
		}
		if limits.AuthorNameRunes <= 0 {
			t.Errorf("%s has no author name bound", source)
		}
		if _, public := actions[ActionPublicReply]; public && !limits.Public.Set() {
			t.Errorf("%s allows public replies without a public limit", source)
		}
		if _, private := actions[ActionPrivateReply]; private && !limits.Private.Set() {
			t.Errorf("%s allows private replies without a private limit", source)
		}
	}
	for source := range sourceLimits {
		if _, ok := sourceActions[source]; !ok {
			t.Errorf("%s has reply limits but no actions", source)
		}
	}
}

func TestReplyTextIsBoundBySourceLimit(t *testing.T) {
	igName := strings.Repeat("w", instagramUsernameRunes)
	fbName := strings.Repeat("w", facebookAuthorNameRunes)
	cases := []struct {
		name    string
		source  shared.EntryType
		action  Action
		text    string
		tooLong bool
	}{
		{"instagram private fits with the longest username", shared.EntryTypeInstagram, ActionPrivateReply,
			strings.Repeat("a", 1000-len(igName)) + "{{username}}", false},
		{"instagram private over by one byte once the username renders", shared.EntryTypeInstagram, ActionPrivateReply,
			strings.Repeat("a", 1001-len(igName)) + "{{username}}", true},
		{"instagram private counts bytes not runes", shared.EntryTypeInstagram, ActionPrivateReply,
			strings.Repeat("ã", 501), true},
		{"instagram public fits", shared.EntryTypeInstagram, ActionPublicReply,
			strings.Repeat("ã", instagramCommentRunes), false},
		{"instagram public over", shared.EntryTypeInstagram, ActionPublicReply,
			strings.Repeat("a", instagramCommentRunes+1), true},
		{"facebook private counts runes", shared.EntryTypeFacebook, ActionPrivateReply,
			strings.Repeat("ã", 2000), false},
		{"facebook private over once the name renders", shared.EntryTypeFacebook, ActionPrivateReply,
			strings.Repeat("a", 2001-len(fbName)) + "{{username}}", true},
		{"facebook public fits", shared.EntryTypeFacebook, ActionPublicReply,
			strings.Repeat("a", facebookCommentRunes-len(fbName)) + "{{username}}", false},
		{"facebook public over", shared.EntryTypeFacebook, ActionPublicReply,
			strings.Repeat("a", facebookCommentRunes+1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := replyRule(tc.source, tc.action, tc.text).Validate()
			if tc.tooLong && !errors.Is(err, ErrReplyTooLong) {
				t.Fatalf("want ErrReplyTooLong, got %v", err)
			}
			if !tc.tooLong && err != nil {
				t.Fatalf("want valid, got %v", err)
			}
		})
	}
}

func TestASourceWithoutLimitsIsRefused(t *testing.T) {
	const unlimited shared.EntryType = "unlimited_test_source"
	sourceActions[unlimited] = map[Action]struct{}{ActionPublicReply: {}}
	t.Cleanup(func() { delete(sourceActions, unlimited) })

	if err := replyRule(unlimited, ActionPublicReply, "oi").Validate(); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("want ErrUnknownSource, got %v", err)
	}
}

func TestCheckReplyBoundsTheRenderedText(t *testing.T) {
	if err := CheckReply(shared.EntryTypeInstagram, ActionPrivateReply, strings.Repeat("a", 1000)); err != nil {
		t.Fatalf("fits: %v", err)
	}
	if err := CheckReply(shared.EntryTypeInstagram, ActionPrivateReply, strings.Repeat("a", 1001)); !errors.Is(err, ErrReplyTooLong) {
		t.Fatalf("over: %v", err)
	}
	if err := CheckReply("telegram", ActionPublicReply, "oi"); !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("unknown source: %v", err)
	}
	if err := CheckReply(shared.EntryTypeInstagram, ActionHide, "oi"); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("not a reply: %v", err)
	}
}

func TestAnUnsetLimitAllowsNothing(t *testing.T) {
	if (TextLimit{}).Allows("") || (TextLimit{}).Allows("a") {
		t.Fatal("a limit that was never set must refuse every text")
	}
}
