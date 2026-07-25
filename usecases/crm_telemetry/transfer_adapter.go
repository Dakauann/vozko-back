package crm_telemetry_usecase

import (
	"vozko/domain/actor"
	ce "vozko/domain/conversation_event"
	dialer_domain "vozko/domain/dialer"
)

// TransferAdapter implements dialer TransferTelemetry via the CRM emitter (queue only).
type TransferAdapter struct {
	em *Emitter
}

func NewTransferAdapter(em *Emitter) *TransferAdapter {
	return &TransferAdapter{em: em}
}

func (a *TransferAdapter) OnTransferStage(workspaceID, callID, transferID, targetUserID, note string, stage dialer_domain.TransferStage) {
	if a == nil || a.em == nil {
		return
	}
	et, ok := mapTransferStage(stage)
	if !ok {
		return
	}
	a.em.Transfer(workspaceID, "", "voice", et, actor.KindSystem, actor.SystemID, transferID, callID, targetUserID, note)
}

func mapTransferStage(stage dialer_domain.TransferStage) (ce.EventType, bool) {
	switch stage {
	case dialer_domain.TransferStagePendingOffer:
		return ce.EventTransferOffered, true
	case dialer_domain.TransferStageCompleted:
		return ce.EventTransferCompleted, true
	case dialer_domain.TransferStageDeclined:
		return ce.EventTransferDeclined, true
	case dialer_domain.TransferStageFailed:
		return ce.EventTransferFailed, true
	case dialer_domain.TransferStageQueued:
		return ce.EventTransferQueued, true
	case dialer_domain.TransferStageTimedOut:
		return ce.EventTransferFailed, true
	case dialer_domain.TransferStageCancelled:
		return ce.EventTransferFailed, true
	default:
		return "", false
	}
}
