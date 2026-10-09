package ai

import "testing"

func TestARequestIDCarriesItsBillingReference(t *testing.T) {
	if got := RequestIDUnder("aichat:th-1", "u-1"); got != "aichat:th-1:u-1" {
		t.Fatalf("RequestIDUnder() = %q", got)
	}
	if got := RequestIDUnder("", "u-1"); got != "u-1" {
		t.Fatalf("without a reference the id stays as is, got %q", got)
	}
	if got := ReferencePrefix("aichat:th-1"); got != "aichat:th-1:" {
		t.Fatalf("ReferencePrefix() = %q", got)
	}
}
