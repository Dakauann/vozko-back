package leadaction

import (
	"errors"
	"time"
	"unicode/utf8"

	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/domain/workspace"
)

const (
	ActionRestoreOptOut     Action     = "restore_opt_out"
	EditRestoreOptOut       EditKind   = "restore_opt_out"
	SkipOptOutNotRestorable SkipReason = "lead_opt_out_not_restorable"
	CapabilityConsent                  = workspace.CapabilityLeadConsent
)

var (
	ErrNoteRequired = errors.New("lead action: undoing opt-outs needs a note saying why")
	ErrNoteTooLong  = errors.New("lead action: the note is longer than 280 characters")
)

func (p Params) restoreFields() bool {
	return p.Note != "" || p.ConfirmCount != 0
}

func (p Params) validateRestore() error {
	if p.Key != "" || len(p.Value) > 0 || p.OwnerID != nil || p.Blocked != nil || p.BusinessPhoneID != "" ||
		p.exportFields() || p.audienceFields() || p.CallList != nil || p.Send != nil {
		return ErrParamsAmbiguous
	}
	return nil
}

func (p Params) ValidateCommit(a Action) error {
	if a != ActionRestoreOptOut {
		return nil
	}
	p = p.Normalized()
	if p.Note == "" {
		return ErrNoteRequired
	}
	if utf8.RuneCountInString(p.Note) > lead.MaxConsentExcerptLength {
		return ErrNoteTooLong
	}
	if p.ConfirmCount <= 0 {
		return selection.ErrCountRequired
	}
	return nil
}

func ConfirmSelected(a Action, p Params, selected int) error {
	if a != ActionRestoreOptOut || p.ConfirmCount == selected {
		return nil
	}
	return &selection.CountChangedError{Expected: p.ConfirmCount, Matched: selected}
}

func restoreEdit(p Params) Edit {
	return Edit{Kind: EditRestoreOptOut, Field: lead.FieldConsent, Value: p.Note, Recorded: true, EventKind: lead.EventConsentWithdrawalReverted}
}

func (e Edit) Note() string {
	note, _ := e.Value.(string)
	return note
}

func (e Edit) RevertChange(actorID, withdrawalEventID string, at time.Time) lead.ConsentChange {
	return lead.RevertWithdrawal(actorID, e.Note(), withdrawalEventID, at)
}
