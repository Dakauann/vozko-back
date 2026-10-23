package transactionalemail

import (
	"fmt"
	"strings"
	"testing"

	"vozko/domain/emailhygiene"
)

func TestAHygieneRefusalKeepsItsCodeAndOnlyACheckingDomainHintsARetry(t *testing.T) {
	for code, retry := range map[string]bool{
		emailhygiene.CodeDomainChecking:    true,
		emailhygiene.CodeDomainUnknown:     false,
		emailhygiene.CodeDisposableAddress: false,
		emailhygiene.CodeNoMailDomain:      false,
	} {
		err := SendRefusal(fmt.Errorf("gateway: %w", &emailhygiene.RecipientRefusal{Code: code}))
		if CodeOf(err) != code {
			t.Fatalf("code = %q, want %q", CodeOf(err), code)
		}
		if got := strings.Contains(RetryHint(err), "tente de novo"); got != retry {
			t.Fatalf("%s retry hint = %q", code, RetryHint(err))
		}
	}
}
