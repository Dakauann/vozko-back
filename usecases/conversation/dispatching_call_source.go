package conversation_usecase

import (
	"context"

	conversation_domain "vozko/domain/conversation"
)

// DispatchingCallSource routes an outbound dial to a channel call source.
// WhatsApp calling is the only channel left since SIP telephony was retired, so
// the dispatch is a single hop; the indirection stays so a second channel can be
// added back without touching every caller.
type DispatchingCallSource struct {
	whatsapp conversation_domain.CallSource
}

func NewDispatchingCallSource(whatsapp conversation_domain.CallSource) *DispatchingCallSource {
	return &DispatchingCallSource{whatsapp: whatsapp}
}

func (d *DispatchingCallSource) Name() string { return "dispatch" }

func (d *DispatchingCallSource) Dial(ctx context.Context, input conversation_domain.CallDialInput) (conversation_domain.CRMCall, error) {
	if d.whatsapp == nil {
		return nil, conversation_domain.ErrNoCallSource
	}
	return d.whatsapp.Dial(ctx, input)
}

var _ conversation_domain.CallSource = (*DispatchingCallSource)(nil)
