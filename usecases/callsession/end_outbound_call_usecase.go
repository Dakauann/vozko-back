package callsession_usecase

import (
	"context"

	"vozko/domain/callsession"
)

type endOutboundCallUseCase struct {
	admission callsession.CallAdmissionCoordinator
}

func NewEndOutboundCallUseCase(admission callsession.CallAdmissionCoordinator) callsession.EndOutboundCallUseCase {
	return &endOutboundCallUseCase{admission: admission}
}

func (uc *endOutboundCallUseCase) Execute(_ context.Context, input callsession.EndOutboundCallInput) error {
	if input.Hangup && input.Call != nil {
		_ = input.Call.Hangup()
	}
	if input.ReleaseAdmission && input.Admission != nil && uc.admission != nil {
		return uc.admission.Release(input.Admission)
	}
	return nil
}
