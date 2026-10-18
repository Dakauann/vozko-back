package lead_usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"vozko/domain/lead"
	"vozko/domain/recordevent"
)

var errConsentIncomplete = errors.New("lead consent: the store and the change notifier are required")

type ConsentDeps struct {
	Store    lead.Store
	Notifier lead.ChangeNotifier
	Now      func() time.Time
	NewID    func() string
}

type ConsentCommands struct {
	writer recordWriter
	now    func() time.Time
	newID  func() string
}

type ConsentOutcome struct {
	Lead    *lead.Lead
	Changed bool
	EventID string
}

func NewConsentCommands(deps ConsentDeps) (*ConsentCommands, error) {
	if deps.Store == nil || deps.Notifier == nil {
		return nil, errConsentIncomplete
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	return &ConsentCommands{writer: recordWriter{store: deps.Store, notifier: deps.Notifier}, now: deps.Now, newID: deps.NewID}, nil
}

func (c *ConsentCommands) Record(ctx context.Context, workspaceID, leadID string, change lead.ConsentChange) (ConsentOutcome, error) {
	if err := requireRecordRef(workspaceID, leadID); err != nil {
		return ConsentOutcome{}, err
	}
	if change.At.IsZero() {
		change.At = c.now().UTC()
	}
	change.ID = c.newID()
	if err := lead.ConsentRules(change); err != nil {
		return ConsentOutcome{}, err
	}
	changed := false
	recorded, err := c.writer.retryingEdit(ctx, workspaceID, leadID, func(current, next *lead.Lead) (recordEdit, error) {
		applied, err := next.ApplyConsentChange(change)
		changed = applied
		if err != nil || !applied {
			return recordEdit{}, err
		}
		return recordEdit{events: []recordevent.Event{lead.ConsentEvent(change, current, next)}}, nil
	})
	if err != nil {
		return ConsentOutcome{}, err
	}
	outcome := ConsentOutcome{Lead: recorded, Changed: changed}
	if changed {
		outcome.EventID = change.ID
	}
	return outcome, nil
}

func (c *ConsentCommands) ClaimOptOutConfirmation(ctx context.Context, workspaceID, leadID, withdrawalEventID string) error {
	if err := requireRecordRef(workspaceID, leadID); err != nil {
		return err
	}
	_, err := c.writer.retryingEdit(ctx, workspaceID, leadID, func(_, next *lead.Lead) (recordEdit, error) {
		if err := next.ClaimOptOutConfirmation(withdrawalEventID, c.now().UTC()); err != nil {
			return recordEdit{}, err
		}
		return recordEdit{fields: []string{lead.FieldConsent}}, nil
	})
	return err
}
