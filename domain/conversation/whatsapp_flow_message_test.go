package conversation

import (
	"errors"
	"strings"
	"testing"
)

func validFlowMessage() SendFlowMessageInput {
	return SendFlowMessageInput{To: "5511987654321", FlowID: "123", FlowToken: "vf1_abc", CTA: "Abrir formulário", Body: "Preencha seus dados"}
}

func TestAFlowMessageWithinWhatsAppLimitsIsValid(t *testing.T) {
	in := validFlowMessage()
	in.Header, in.Footer = "Cadastro", "Leva um minuto"
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFlowMessageRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SendFlowMessageInput)
		want   error
	}{
		{"no recipient", func(in *SendFlowMessageInput) { in.To = " " }, ErrWhatsAppRecipientRequired},
		{"no flow", func(in *SendFlowMessageInput) { in.FlowID = "" }, ErrWhatsAppFlowRequired},
		{"no token", func(in *SendFlowMessageInput) { in.FlowToken = "" }, ErrWhatsAppFlowTokenRequired},
		{"no body", func(in *SendFlowMessageInput) { in.Body = " " }, ErrWhatsAppBodyRequired},
		{"body too long", func(in *SendFlowMessageInput) { in.Body = strings.Repeat("a", WhatsAppButtonBodyMaxLen+1) }, ErrWhatsAppFlowCopyInvalid},
		{"no call to action", func(in *SendFlowMessageInput) { in.CTA = "" }, ErrWhatsAppFlowCopyInvalid},
		{"call to action too long", func(in *SendFlowMessageInput) { in.CTA = strings.Repeat("a", WhatsAppFlowCTAMaxLen+1) }, ErrWhatsAppFlowCopyInvalid},
		{"call to action with an emoji", func(in *SendFlowMessageInput) { in.CTA = "Abrir 🙂" }, ErrWhatsAppFlowCopyInvalid},
		{"header too long", func(in *SendFlowMessageInput) { in.Header = strings.Repeat("a", WhatsAppHeaderTextMaxLen+1) }, ErrWhatsAppFlowCopyInvalid},
		{"footer too long", func(in *SendFlowMessageInput) { in.Footer = strings.Repeat("a", WhatsAppFooterMaxLen+1) }, ErrWhatsAppFlowCopyInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validFlowMessage()
			tc.mutate(&in)
			if err := in.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateFlowCopyChecksOnlyTheText(t *testing.T) {
	if err := ValidateFlowCopy("", "Preencha", "", "Abrir"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFlowCopy("", "Preencha", "", "Abrir ✅"); !errors.Is(err, ErrWhatsAppFlowCopyInvalid) {
		t.Fatalf("err = %v", err)
	}
}
