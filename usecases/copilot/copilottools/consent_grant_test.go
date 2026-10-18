package copilottools

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/workspace"
)

func TestConsentGrantRefusalIsDeniedWithItsCode(t *testing.T) {
	res, ok := ConsentGrantRefusal(fmt.Errorf("activate: %w", workspace.ErrConsentGrantForbidden))
	if !ok || res.Status != copilot.StatusDenied || !strings.Contains(res.Message, workspace.CodeConsentGrantForbidden) {
		t.Fatalf("ok %v result %+v", ok, res)
	}
	if _, ok := ConsentGrantRefusal(errors.New("other")); ok {
		t.Fatal("another error is not a consent refusal")
	}
}

func TestRequesterOfIsTheUserOfTheConversation(t *testing.T) {
	cc := copilot.Context{UserID: "u-9", SystemAdmin: true}
	if got := RequesterOf(cc); got != (workspace.Requester{UserID: "u-9", SystemAdmin: true}) {
		t.Fatalf("requester = %+v", got)
	}
}
