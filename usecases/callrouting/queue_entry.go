package callrouting_usecase

import (
	"context"

	"vozko/domain/callrouting"
)

var _ callrouting.QueueEntrance = (*TransferCall)(nil)

func (uc *TransferCall) EnterQueue(ctx context.Context, entry callrouting.QueueEntry) (callrouting.TransferOutcome, error) {
	notes, err := callrouting.NormalizeTransferNotes(entry.Notes)
	if err != nil {
		return "", err
	}
	if entry.Call == nil || entry.Call.WorkspaceID() != entry.WorkspaceID {
		return "", callrouting.ErrCallNotFound
	}
	queue, err := uc.deps.Queues.FindInWorkspace(ctx, entry.WorkspaceID, entry.QueueID)
	if err != nil {
		return "", err
	}
	callID := entry.Call.ID()
	if _, err := uc.begin(callID, ""); err != nil {
		return "", err
	}
	defer uc.end(callID)

	record := uc.newRecord(entry.WorkspaceID, callID, "", callrouting.TransferTarget{Kind: callrouting.TargetQueue, QueueID: queue.ID}, notes)
	if err := uc.deps.Log.Record(ctx, record); err != nil {
		return "", err
	}
	result, err := uc.deps.Dispatcher.Enqueue(ctx, EnqueueInput{Queue: queue, Call: entry.Call, Notes: notes, FromName: entry.From})
	if err != nil {
		entry.Call.StopHold()
		uc.finish(record, callrouting.OutcomeCancelled, "")
		return "", err
	}
	if result.Outcome == callrouting.OutcomeTimedOut {
		entry.Call.StopHold()
	}
	uc.finish(record, result.Outcome, result.AgentUserID)
	return result.Outcome, nil
}
