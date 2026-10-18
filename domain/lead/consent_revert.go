package lead

import (
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/recordevent"
)

const (
	ConsentRevertWithdrawal        ConsentKind = "withdrawal_reverted"
	EventConsentWithdrawalReverted             = recordevent.Kind("consent_withdrawal_reverted")
)

var (
	ErrOptOutNotRestorable = errors.New("lead: only the current opt-out recorded by a template button, an exit word, an AI agent or a workflow can be undone")
	ErrLeadNotOptedOut     = errors.New("lead: this lead is not opted out")
)

var automatedWithdrawals = []ConsentSource{ConsentButtonReply, ConsentKeyword, ConsentAIAgent, ConsentWorkflow}

func (s ConsentSource) RecordsAutomatedWithdrawals() bool {
	return slices.Contains(automatedWithdrawals, s)
}

func RevertWithdrawal(userID, note, withdrawalEventID string, at time.Time) ConsentChange {
	return ConsentChange{
		Kind:     ConsentRevertWithdrawal,
		Actor:    ConsentActor{Type: ConsentActorUser, ID: strings.TrimSpace(userID)},
		Evidence: ConsentEvidence{Type: EvidenceNote, Excerpt: strings.TrimSpace(note)},
		Reverts:  strings.TrimSpace(withdrawalEventID),
		At:       at,
	}
}

func revertRules(c ConsentChange) error {
	if c.Source != "" || c.Purpose != "" {
		return ErrConsentNotAllowed
	}
	if c.RequestedBy != "" {
		return ErrLeadOptOutSourceInvalid
	}
	if c.At.IsZero() {
		return ErrConsentTimeRequired
	}
	if c.Actor.Type != ConsentActorUser || strings.TrimSpace(c.Actor.ID) == "" {
		return ErrConsentActorInvalid
	}
	if c.Evidence.Type != EvidenceNote || c.Evidence.Ref != "" {
		return ErrConsentEvidenceInvalid
	}
	if strings.TrimSpace(c.Evidence.Excerpt) == "" {
		return ErrConsentEvidenceRequired
	}
	if utf8.RuneCountInString(c.Evidence.Excerpt) > MaxConsentExcerptLength {
		return ErrConsentExcerptTooLong
	}
	if strings.TrimSpace(c.Reverts) == "" {
		return ErrOptOutNotRestorable
	}
	return nil
}

func (l *Lead) WithdrawalToRevert(chosenAt time.Time) (string, error) {
	current, err := l.automatedWithdrawal()
	if err != nil {
		return "", err
	}
	if current.At.After(chosenAt) {
		return "", ErrOptOutNotRestorable
	}
	return current.EventID, nil
}

func (l *Lead) automatedWithdrawal() (*ConsentRecord, error) {
	if l.OptedOutAt == nil {
		return nil, ErrLeadNotOptedOut
	}
	current := l.ConsentRecord
	if current == nil || current.Kind != ConsentWithdraw || current.EventID == "" || !current.Source.RecordsAutomatedWithdrawals() {
		return nil, ErrOptOutNotRestorable
	}
	return current, nil
}

func (l *Lead) revertWithdrawal(c ConsentChange) (bool, error) {
	current, err := l.automatedWithdrawal()
	if err != nil {
		return false, err
	}
	if current.EventID != c.Reverts {
		return false, ErrOptOutNotRestorable
	}
	l.WhatsAppOptIn = current.PriorGrant.clone()
	l.OptedOutAt, l.OptOutSource = nil, ""
	l.ConsentRecord = &ConsentRecord{
		Kind: c.Kind, Source: current.Source, Actor: c.Actor, Evidence: c.Evidence, At: c.At, EventID: c.ID, Reverts: c.Reverts,
	}
	return true, nil
}

func (c *Consent) clone() *Consent {
	if c == nil {
		return nil
	}
	out := *c
	return &out
}

func (c ConsentChange) revertDetail(before *Lead) map[string]any {
	source := ""
	if before != nil && before.ConsentRecord != nil {
		source = string(before.ConsentRecord.Source)
	}
	return map[string]any{
		"kind": string(c.Kind), "source": source,
		"actorType": string(c.Actor.Type), "actorId": c.Actor.ID,
		"evidenceType": string(c.Evidence.Type), "note": c.Evidence.Excerpt, "revertedEventId": c.Reverts,
	}
}
