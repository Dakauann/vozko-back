package dialer_usecase

import (
	"context"

	"vozko/domain/dialer"
)

type endOutboundCallUseCase struct {
	admission dialer.CallAdmissionCoordinator
}

func NewEndOutboundCallUseCase(admission dialer.CallAdmissionCoordinator) dialer.EndOutboundCallUseCase {
	return &endOutboundCallUseCase{admission: admission}
}

func (uc *endOutboundCallUseCase) Execute(_ context.Context, input dialer.EndOutboundCallInput) error {
	if input.Hangup && input.Call != nil {
		_ = input.Call.Hangup()
	}
	if input.ReleaseAdmission && input.Admission != nil && uc.admission != nil {
		return uc.admission.Release(input.Admission)
	}
	return nil
}
