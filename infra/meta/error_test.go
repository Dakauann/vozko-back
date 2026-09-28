package meta

import "testing"

func TestErrorPredicates(t *testing.T) {
	cases := []struct {
		name  string
		err   *Error
		check func(*Error) bool
		want  bool
	}{
		{"thread owned by another app", &Error{Code: 10, Subcode: SubcodeThreadOwnedElsewhere}, (*Error).IsThreadControl, true},
		{"not the thread owner", &Error{Code: 100, Subcode: SubcodeNotThreadOwner}, (*Error).IsThreadControl, true},
		{"plain invalid param is not thread control", &Error{Code: 100}, (*Error).IsThreadControl, false},
		{"standard access only", &Error{Code: 200, Message: "Cannot message users who are not admins, developers or testers"}, (*Error).IsAccessLevel, true},
		{"pages_messaging not reviewed", &Error{Code: 200, Subcode: SubcodeMessagingNotReviewed}, (*Error).IsAccessLevel, true},
		{"page restricted", &Error{Code: 10, Subcode: SubcodePageRestricted}, (*Error).IsPageRestricted, true},
		{"private reply already sent", &Error{Code: CodePrivateReplyUsed}, (*Error).IsPrivateReplyUsed, true},
		{"private reply invalid comment", &Error{Code: 100, Subcode: SubcodePrivateReplyInvalid}, (*Error).IsPrivateReplyUsed, true},
		{"duplicate post", &Error{Code: CodeDuplicatePost}, (*Error).IsDuplicatePost, true},
		{"role lost", &Error{Code: CodeAccessTokenError, Subcode: SubcodeRoleLost}, (*Error).IsRoleLost, true},
		{"expired token is not role lost", &Error{Code: CodeAccessTokenError, Subcode: 463}, (*Error).IsRoleLost, false},
		{"messenger window closed", &Error{Code: 10, Subcode: SubcodeOutsideWindow}, (*Error).IsWindowClosed, true},
		{"messenger window closed alt", &Error{Code: 10, Subcode: SubcodeOutsideWindowAlt}, (*Error).IsWindowClosed, true},
		{"instagram window closed", &Error{Code: CodeWindowClosed}, (*Error).IsWindowClosed, true},
		{"cannot receive", &Error{Code: 10, Subcode: SubcodeCannotReceive}, (*Error).IsRecipientUnreachable, true},
		{"person unavailable", &Error{Code: CodePersonUnavailable}, (*Error).IsRecipientUnreachable, true},
		{"page rate limit", &Error{Code: CodePageRateLimit}, (*Error).IsRateLimit, true},
		{"page rate limit retryable", &Error{Code: CodePageRateLimit}, (*Error).Retryable, true},
		{"policy block is not retryable", &Error{Code: CodePolicyBlock}, (*Error).Retryable, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.check(tc.err); got != tc.want {
				t.Fatalf("got %t, want %t", got, tc.want)
			}
		})
	}
}

func TestPredicatesOnNilAreFalse(t *testing.T) {
	var e *Error
	for name, check := range map[string]func(*Error) bool{
		"thread":    (*Error).IsThreadControl,
		"access":    (*Error).IsAccessLevel,
		"restrict":  (*Error).IsPageRestricted,
		"private":   (*Error).IsPrivateReplyUsed,
		"duplicate": (*Error).IsDuplicatePost,
		"role":      (*Error).IsRoleLost,
	} {
		if check(e) {
			t.Fatalf("%s: nil error reported true", name)
		}
	}
}
