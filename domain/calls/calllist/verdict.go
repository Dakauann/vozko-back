package calllist

import (
	"time"

	"vozko/domain/calls/cdr"
)

func FactsOf(call *cdr.Call) *CallFacts {
	if call == nil {
		return nil
	}
	facts := &CallFacts{ID: call.ID, WorkspaceID: call.WorkspaceID}
	if call.LeadID != nil {
		facts.LeadID = *call.LeadID
	}
	if call.AgentID != nil {
		facts.AgentID = *call.AgentID
	}
	return facts
}

func (l *List) Closable(i Item, by string, call *CallFacts, now time.Time) bool {
	if l == nil || l.ID != i.ListID || l.AcceptsOutcomes() != nil {
		return false
	}
	return i.ClosableBy(by, call, now) == nil
}

func (l *List) StatusMoves() []Status {
	moves := []Status{}
	for _, to := range managedTransitions[l.Status] {
		if to != l.Status {
			moves = append(moves, to)
		}
	}
	return moves
}

type ListVerdict struct {
	AcceptsOutcomes bool
	StatusMoves     []Status
}

func (l *List) Verdict(manages bool) ListVerdict {
	accepts := l.AcceptsOutcomes() == nil
	moves := []Status{}
	if manages {
		moves = l.StatusMoves()
	}
	return ListVerdict{
		AcceptsOutcomes: accepts,
		StatusMoves:     moves,
	}
}

type Progress struct {
	Closed    int
	Called    int
	Callbacks int
}

func (p Progress) None() bool {
	return p == Progress{}
}

func flag(set bool) int {
	if set {
		return 1
	}
	return 0
}

func (i Item) progress() Progress {
	return Progress{
		Closed:    flag(i.State == StateClosed),
		Called:    flag(i.LastCallID != ""),
		Callbacks: flag(i.State != StateClosed && i.Disposition == DispositionCallback),
	}
}

func ProgressChange(before, after Item) Progress {
	was, is := before.progress(), after.progress()
	return Progress{Closed: is.Closed - was.Closed, Called: is.Called - was.Called, Callbacks: is.Callbacks - was.Callbacks}
}
