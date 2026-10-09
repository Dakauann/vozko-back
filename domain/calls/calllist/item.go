package calllist

import (
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

type State string

const (
	StatePending  State = "pending"
	StateReserved State = "reserved"
	StateClosed   State = "closed"
)

func (s State) Valid() bool {
	return s == StatePending || s == StateReserved || s == StateClosed
}

const (
	ReservationTTL   = 15 * time.Minute
	MaxNoteLength    = 2000
	MaxCallbackAhead = 90 * 24 * time.Hour
	RefusalRecheck   = 24 * time.Hour
)

const (
	DispositionCallback = conversation.ReservedOutcomePrefix + "callback"
	DispositionRefused  = conversation.ReservedOutcomePrefix + "refused"
)

type Item struct {
	ID            string
	ListID        string
	WorkspaceID   string
	LeadID        string
	Phone         string
	Position      int
	State         State
	ReservedBy    string
	ReservedUntil *time.Time
	Disposition   string
	Note          string
	Refusal       string
	CallbackAt    *time.Time
	LastCallID    string
	ClosedBy      string
	ClosedAt      *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (i Item) reservation() shared.Reservation {
	if i.State != StateReserved || i.ReservedUntil == nil {
		return shared.Reservation{}
	}
	return shared.Reservation{Holder: i.ReservedBy, Until: *i.ReservedUntil}
}

func (i Item) LiveFor(userID string, now time.Time) bool {
	return i.reservation().HeldBy(userID, now)
}

func (i Item) waiting(now time.Time) bool {
	return i.State == StatePending && i.CallbackAt != nil && i.CallbackAt.After(now)
}

func (i Item) Claimable(now time.Time) bool {
	switch i.State {
	case StatePending:
		return !i.waiting(now)
	case StateReserved:
		return !i.reservation().Live(now)
	}
	return false
}

func (i *Item) Reserve(userID string, now time.Time) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ErrActorRequired
	}
	if i.State == StateClosed || i.waiting(now) || !i.reservation().TakeableBy(userID, now) {
		return ErrItemTaken
	}
	held := shared.ReserveFor(userID, now, ReservationTTL)
	i.State, i.ReservedBy, i.ReservedUntil, i.Refusal, i.UpdatedAt = StateReserved, held.Holder, &held.Until, "", now
	return nil
}

func (i *Item) clearReservation() {
	i.ReservedBy, i.ReservedUntil = "", nil
}

func (i *Item) Release(userID string, now time.Time) error {
	if i.State != StateReserved || strings.TrimSpace(userID) == "" || i.ReservedBy != userID {
		return ErrItemNotReserved
	}
	i.State, i.UpdatedAt = StatePending, now
	i.clearReservation()
	return nil
}

func (i Item) CheckDial(userID, leadID, number string, now time.Time) error {
	if !i.LiveFor(userID, now) {
		return ErrItemNotReserved
	}
	if strings.TrimSpace(leadID) != i.LeadID || !lead.SameNumber(i.Phone, number) {
		return ErrItemMismatch
	}
	return nil
}

func (i *Item) Stamp(by, callRecordID string, now time.Time) error {
	by = strings.TrimSpace(by)
	switch {
	case by == "":
		return ErrActorRequired
	case i.State == StateClosed:
		return ErrItemClosed
	case i.ReservedBy != "" && i.ReservedBy != by:
		return ErrItemTaken
	}
	i.LastCallID, i.UpdatedAt = callRecordID, now
	if i.Disposition == DispositionCallback {
		i.Disposition = ""
	}
	if i.State != StateReserved {
		return nil
	}
	if extended := now.Add(ReservationTTL); i.ReservedUntil == nil || extended.After(*i.ReservedUntil) {
		i.ReservedUntil = &extended
	}
	return nil
}

type CallFacts struct {
	ID          string
	WorkspaceID string
	LeadID      string
	AgentID     string
}

type Closing struct {
	By          string
	Disposition string
	Note        string
	CallbackAt  *time.Time
}

func (c Closing) normalized() Closing {
	c.By = strings.TrimSpace(c.By)
	c.Disposition = strings.ToLower(strings.TrimSpace(c.Disposition))
	c.Note = strings.TrimSpace(c.Note)
	return c
}

func (i Item) ownCall(by string, call *CallFacts) error {
	if i.LastCallID == "" {
		return ErrItemNotCalled
	}
	if call == nil || call.ID != i.LastCallID || call.WorkspaceID != i.WorkspaceID || call.LeadID != i.LeadID || call.AgentID != by {
		return ErrCallNotTheItems
	}
	return nil
}

func (i Item) untakenFor(by string, now time.Time) error {
	if i.LiveFor(by, now) {
		return nil
	}
	if i.ReservedBy != "" && i.ReservedBy != by {
		return ErrItemTaken
	}
	return nil
}

func disposition(c Closing, catalogue *conversation.OutcomeCapture, now time.Time) (string, error) {
	if c.Disposition == "" {
		return "", ErrDispositionRequired
	}
	if c.Disposition == DispositionCallback {
		if c.CallbackAt == nil || !c.CallbackAt.After(now) || c.CallbackAt.After(now.Add(MaxCallbackAhead)) {
			return "", ErrCallbackTime
		}
		return c.Disposition, nil
	}
	if c.CallbackAt != nil {
		return "", ErrCallbackTime
	}
	if conversation.IsReservedOutcome(c.Disposition) {
		return "", ErrDispositionReserved
	}
	if catalogue == nil || len(catalogue.Outcomes) == 0 {
		return "", ErrNoOutcomes
	}
	outcome, found := catalogue.Lookup(c.Disposition)
	if !found {
		return "", ErrDispositionUnknown
	}
	return outcome.Code, nil
}

func (i Item) ClosableBy(by string, call *CallFacts, now time.Time) error {
	by = strings.TrimSpace(by)
	switch {
	case by == "":
		return ErrActorRequired
	case i.State == StateClosed:
		return ErrItemClosed
	case i.Disposition == DispositionCallback:
		return ErrItemNotCalled
	}
	if err := i.ownCall(by, call); err != nil {
		return err
	}
	return i.untakenFor(by, now)
}

func (i *Item) Close(c Closing, call *CallFacts, catalogue *conversation.OutcomeCapture, now time.Time) (bool, error) {
	c = c.normalized()
	if err := i.ClosableBy(c.By, call, now); err != nil {
		return false, err
	}
	if utf8.RuneCountInString(c.Note) > MaxNoteLength {
		return false, ErrNoteTooLong
	}
	code, err := disposition(c, catalogue, now)
	if err != nil {
		return false, err
	}
	i.Disposition, i.Note, i.UpdatedAt = code, c.Note, now
	i.clearReservation()
	if code == DispositionCallback {
		callback := *c.CallbackAt
		i.State, i.CallbackAt = StatePending, &callback
		return false, nil
	}
	closed := now
	i.State, i.CallbackAt, i.ClosedBy, i.ClosedAt = StateClosed, nil, c.By, &closed
	return true, nil
}

func (i *Item) Refuse(reason SkipReason, now time.Time) (bool, error) {
	if i.State == StateClosed {
		return false, ErrItemClosed
	}
	i.Refusal, i.UpdatedAt = string(reason), now
	i.clearReservation()
	if reason.Reversible() {
		recheck := now.Add(RefusalRecheck)
		i.State, i.CallbackAt = StatePending, &recheck
		return false, nil
	}
	closed := now
	i.State, i.Disposition, i.CallbackAt, i.ClosedBy, i.ClosedAt = StateClosed, DispositionRefused, nil, "", &closed
	return true, nil
}
