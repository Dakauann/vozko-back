package callhistory

import (
	"slices"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/calls/cdr"
	"vozko/domain/calls/recordings"
)

type EntryKind string

const (
	EntryStarted            EntryKind = "started"
	EntryAnswered           EntryKind = "answered"
	EntryTransferRequested  EntryKind = "transfer_requested"
	EntryTransferConnected  EntryKind = "transfer_connected"
	EntryTransferReturned   EntryKind = "transfer_returned"
	EntryTransferUnanswered EntryKind = "transfer_unanswered"
	EntryTransferCancelled  EntryKind = "transfer_cancelled"
	EntryCallerLeft         EntryKind = "caller_left"
	EntryEnded              EntryKind = "ended"
	EntryRecordingReady     EntryKind = "recording_ready"
)

type TimelineEntry struct {
	Kind          EntryKind
	At            time.Time
	ActorID       string
	TargetUserID  string
	TargetQueueID string
	Notes         string
	Reason        string
}

var transferOutcomeEntries = map[callrouting.TransferOutcome]EntryKind{
	callrouting.OutcomeConnected: EntryTransferConnected,
	callrouting.OutcomeReturned:  EntryTransferReturned,
	callrouting.OutcomeTimedOut:  EntryTransferUnanswered,
	callrouting.OutcomeAbandoned: EntryCallerLeft,
	callrouting.OutcomeCancelled: EntryTransferCancelled,
}

func BuildTimeline(call cdr.Call, transfers []callrouting.TransferRecord, recording *recordings.CallRecord) []TimelineEntry {
	agent := valueOf(call.AgentID)
	timeline := []TimelineEntry{{Kind: EntryStarted, At: call.StartedAt, ActorID: startedBy(call, agent)}}
	if call.AnsweredAt != nil {
		timeline = append(timeline, TimelineEntry{Kind: EntryAnswered, At: *call.AnsweredAt, ActorID: agent})
	}
	for _, transfer := range transfers {
		timeline = append(timeline, TimelineEntry{
			Kind: EntryTransferRequested, At: transfer.CreatedAt, ActorID: transfer.FromUserID,
			TargetUserID: transfer.Target.UserID, TargetQueueID: transfer.Target.QueueID, Notes: transfer.Notes,
		})
		if kind, finished := transferOutcomeEntries[transfer.Outcome]; finished && !transfer.FinishedAt.IsZero() {
			timeline = append(timeline, TimelineEntry{
				Kind: kind, At: transfer.FinishedAt, ActorID: transfer.AnsweredBy,
				TargetUserID: transfer.Target.UserID, TargetQueueID: transfer.Target.QueueID,
			})
		}
	}
	if call.IsTerminal() && call.EndedAt != nil {
		timeline = append(timeline, TimelineEntry{Kind: EntryEnded, At: *call.EndedAt, Reason: endReason(call)})
	}
	if recording != nil {
		timeline = append(timeline, TimelineEntry{Kind: EntryRecordingReady, At: recording.CreatedAt})
	}
	slices.SortStableFunc(timeline, func(a, b TimelineEntry) int { return a.At.Compare(b.At) })
	return timeline
}

func startedBy(call cdr.Call, agent string) string {
	if call.Direction == cdr.DirectionOutbound {
		return agent
	}
	return ""
}
