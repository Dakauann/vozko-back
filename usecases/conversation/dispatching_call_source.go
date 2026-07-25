package conversation_usecase

import (
	"context"
	"strings"

	conversation_domain "vozko/domain/conversation"
)

type DispatchingCallSource struct {
	sip      conversation_domain.CallSource
	whatsapp conversation_domain.CallSource
}

func NewDispatchingCallSource(sip, whatsapp conversation_domain.CallSource) *DispatchingCallSource {
	return &DispatchingCallSource{sip: sip, whatsapp: whatsapp}
}

func (d *DispatchingCallSource) Name() string { return "dispatch" }

func (d *DispatchingCallSource) Dial(ctx context.Context, input conversation_domain.CallDialInput) (conversation_domain.CRMCall, error) {
	if d.whatsapp != nil && strings.TrimSpace(input.WhatsAppPhoneID) != "" {
		return d.whatsapp.Dial(ctx, input)
	}
	return d.sip.Dial(ctx, input)
}

var _ conversation_domain.CallSource = (*DispatchingCallSource)(nil)
