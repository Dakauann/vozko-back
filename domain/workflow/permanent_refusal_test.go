package workflow

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
)

func TestASendRefusedForGoodIsNeverRetriedWhileATransientFailureIs(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		permanent bool
	}{
		{"a closed 24 h window stays closed until the customer writes", fmt.Errorf("%w (no customer message in the last 24 h)", conversation.ErrWindowClosed), true},
		{"a blocked contact stays blocked", fmt.Errorf("refused: %w", wo.ErrLeadBlocked), true},
		{"an opted out contact asked not to be messaged", fmt.Errorf("refused: %w", wo.ErrLeadOptedOut), true},
		{"an invalid number does not become valid", fmt.Errorf("refused: %w", wo.ErrInvalidPhone), true},
		{"a node failure carrying the window kind is the same refusal", Fail(ErrorKindWindowClosed, errors.New("closed")), true},
		{"an Instagram or Messenger window closed by Meta stays closed", errors.Join(conversation.ErrOutboundWindowClosed, errors.New("meta: code=10 subcode=2018278")), true},
		{"an Instagram or Messenger person who cannot receive messages", fmt.Errorf("refused (%w): meta 551", conversation.ErrRecipientUnreachable), true},
		{"a template Meta paused, disabled or does not know", fmt.Errorf("meta 132015: %w", template.ErrTemplateNotSendable), true},
		{"a message type the provider does not accept is refused the same way next time", fmt.Errorf("meta 131051: %w", conversation.ErrMessageTypeUnsupported), true},
		{"a database that did not answer may answer later", errors.New("db down"), false},
		{"a timeout may pass", context.DeadlineExceeded, false},
		{"no error is nothing to retry or refuse", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPermanentRefusal(tc.err); got != tc.permanent {
				t.Fatalf("permanent = %v, want %v", got, tc.permanent)
			}
		})
	}
}
