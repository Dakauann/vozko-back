package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

var errLocationsIncomplete = errors.New("lead locations: a required dependency is missing")

type MessageReader interface {
	GetByID(id string) (*conversation.Message, error)
}

type LocationDeps struct {
	Store       lead.Store
	Permissions Permissions
	Definitions DefinitionSource
	Notifier    lead.ChangeNotifier
	Messages    MessageReader
	Access      EntryAccessResolver
	EntryLeads  lead.EntryLeads
	Now         func() time.Time
}

type Locations struct {
	deps    LocationDeps
	writer  recordWriter
	viewers viewers
}

func NewLocations(deps LocationDeps) (*Locations, error) {
	missing := map[string]bool{
		"store":       deps.Store == nil,
		"permissions": deps.Permissions == nil,
		"definitions": deps.Definitions == nil,
		"notifier":    deps.Notifier == nil,
		"messages":    deps.Messages == nil,
		"access":      deps.Access == nil,
		"entry leads": deps.EntryLeads == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errLocationsIncomplete, name)
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Locations{
		deps:    deps,
		writer:  recordWriter{store: deps.Store, notifier: deps.Notifier},
		viewers: viewers{permissions: deps.Permissions, definitions: deps.Definitions},
	}, nil
}

func (s *Locations) PinLocation(ctx context.Context, a Actor, leadID, addressID string, point geo.Point) (*lead.Lead, error) {
	v, err := s.viewers.authorize(a, workspace.ActionUpdate, workspace.ActionReadAddresses)
	if err != nil {
		return nil, err
	}
	fix, err := geo.Pinned(point, geo.SourceManual, s.deps.Now())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", lead.ErrLocationInvalid, err)
	}
	return s.pin(ctx, a, v, leadID, lead.EventLocationPinned, func(l *lead.Lead) (bool, error) {
		return l.PinLocation(addressID, fix)
	})
}

func (s *Locations) AcceptLocation(ctx context.Context, a Actor, leadID, messageID string) (*lead.Lead, error) {
	v, err := s.viewers.authorize(a, workspace.ActionUpdate)
	if err != nil {
		return nil, err
	}
	if err := requireRecordRef(a.WorkspaceID, leadID); err != nil {
		return nil, err
	}
	candidate, err := s.candidate(ctx, a, leadID, messageID)
	if err != nil {
		return nil, err
	}
	fix, err := geo.Pinned(candidate.Point, geo.SourceLeadPin, s.deps.Now())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", lead.ErrLocationNotFound, err)
	}
	return s.pin(ctx, a, v, leadID, lead.EventLocationAccepted, func(l *lead.Lead) (bool, error) {
		return l.AcceptLocation(fix)
	})
}

func (s *Locations) candidate(ctx context.Context, a Actor, leadID, messageID string) (conversation.LocationCandidate, error) {
	if _, err := uuid.Parse(messageID); err != nil {
		return conversation.LocationCandidate{}, lead.ErrLocationNotFound
	}
	message, err := s.deps.Messages.GetByID(messageID)
	if errors.Is(err, conversation.ErrMessageNotFound) || (err == nil && message == nil) {
		return conversation.LocationCandidate{}, lead.ErrLocationNotFound
	}
	if err != nil {
		return conversation.LocationCandidate{}, fmt.Errorf("message %s: %w", messageID, err)
	}
	candidate, ok := conversation.LocationCandidateOf(message)
	if !ok {
		return conversation.LocationCandidate{}, lead.ErrLocationNotFound
	}
	ref := shared.EntryRef{EntryID: message.EntryID, EntryType: message.EntryType}
	visible, err := visibleEntries(s.deps.Access, a, []shared.EntryRef{ref})
	if err != nil {
		return conversation.LocationCandidate{}, err
	}
	if !visible[ref] {
		return conversation.LocationCandidate{}, lead.ErrLocationNotFound
	}
	owner, err := s.deps.EntryLeads.LeadOfEntry(ctx, a.WorkspaceID, ref)
	if errors.Is(err, lead.ErrLeadNotFound) {
		return conversation.LocationCandidate{}, lead.ErrLocationNotFound
	}
	if err != nil {
		return conversation.LocationCandidate{}, fmt.Errorf("lead of entry %s: %w", ref.EntryID, err)
	}
	if owner != leadID {
		return conversation.LocationCandidate{}, lead.ErrLocationNotFound
	}
	return candidate, nil
}

func (s *Locations) pin(ctx context.Context, a Actor, v lead.Viewer, leadID string, kind recordevent.Kind, change func(*lead.Lead) (bool, error)) (*lead.Lead, error) {
	sent := []string{lead.FieldAddresses}
	pinned, err := s.writer.retryingEdit(ctx, a.WorkspaceID, leadID, func(current, next *lead.Lead) (recordEdit, error) {
		changed, err := change(next)
		if err != nil || !changed {
			return recordEdit{}, err
		}
		return recordEdit{events: []recordevent.Event{lead.PinEvent(kind, a.UserID, current, next)}, fields: sent}, nil
	})
	if errors.Is(err, errStillContended) {
		return nil, s.writer.conflict(ctx, a.WorkspaceID, leadID, v, sent)
	}
	if err != nil {
		return nil, err
	}
	return pinned.VisibleTo(v, sent), nil
}
