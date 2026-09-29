package conversation_usecase

import (
	"context"
	"strings"

	conversation_domain "vozko/domain/conversation"
)

type DispatchingCallSource struct {
	whatsapp conversation_domain.CallSource
	trunks   conversation_domain.CallSource
}

func NewDispatchingCallSource(whatsapp, trunks conversation_domain.CallSource) *DispatchingCallSource {
	return &DispatchingCallSource{whatsapp: whatsapp, trunks: trunks}
}

func (d *DispatchingCallSource) Name() string { return "dispatch" }

func (d *DispatchingCallSource) Dial(ctx context.Context, input conversation_domain.CallDialInput) (conversation_domain.CRMCall, error) {
	source := d.whatsapp
	if strings.TrimSpace(input.TrunkID) != "" {
		source = d.trunks
	}
	if source == nil {
		return nil, conversation_domain.ErrNoCallSource
	}
	return source.Dial(ctx, input)
}

var _ conversation_domain.CallSource = (*DispatchingCallSource)(nil)
