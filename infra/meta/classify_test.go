package meta

import (
	"fmt"
	"testing"

	fbdomain "vozko/domain/facebook"
)

func TestFacebookDomainClassifiesGraphErrors(t *testing.T) {
	cases := []struct {
		err  error
		want fbdomain.Failure
	}{
		{&Error{Code: CodeAccessTokenError, Subcode: 463}, fbdomain.FailureReauth},
		{&Error{Code: CodeAccessTokenError, Subcode: SubcodeRoleLost}, fbdomain.FailureRoleLost},
		{&Error{Code: CodeThreadOwned}, fbdomain.FailureThreadControl},
		{&Error{Code: 10, Subcode: SubcodeOutsideWindow}, fbdomain.FailureWindowClosed},
		{&Error{Code: CodePersonUnavailable}, fbdomain.FailureUnreachable},
		{&Error{Code: CodeAccessLevel, Subcode: SubcodeMessagingNotReviewed}, fbdomain.FailureAccessLevel},
		{&Error{Code: 10, Subcode: SubcodePageRestricted}, fbdomain.FailurePageRestricted},
		{&Error{Code: CodePrivateReplyUsed}, fbdomain.FailurePrivateReplyUsed},
		{&Error{Code: CodeDuplicatePost}, fbdomain.FailureDuplicatePost},
		{&Error{Code: CodePolicyBlock}, fbdomain.FailurePolicyBlock},
		{&Error{Code: CodePageRateLimit}, fbdomain.FailureRetryable},
		{&Error{Code: CodeInvalidParam}, fbdomain.FailureRejectedPermanent},
		{fmt.Errorf("wrapped: %w", &Error{Code: CodeThreadOwned}), fbdomain.FailureThreadControl},
		{fmt.Errorf("plain"), fbdomain.FailureUnknown},
	}
	for _, tc := range cases {
		if got := fbdomain.Classify(tc.err); got != tc.want {
			t.Errorf("%v classified %q, want %q", tc.err, got, tc.want)
		}
	}
	if fbdomain.ErrorCode(fmt.Errorf("x: %w", &Error{Code: 10900})) != 10900 {
		t.Fatal("error code not exposed")
	}
}
