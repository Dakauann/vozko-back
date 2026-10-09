package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

const targetedAttempts = 3

var errStillContended = fmt.Errorf("lead: the record kept changing while it was written: %w", shared.ErrVersionConflict)

type recordChange func(*lead.Lead) ([]string, error)

type recordEdit struct {
	events []recordevent.Event
	fields []string
}

func (e recordEdit) changed() bool {
	return len(e.fields) > 0 || len(recordevent.FieldsOf(e.events)) > 0
}

func (e recordEdit) notified() []string {
	if fields := recordevent.FieldsOf(e.events); len(fields) > 0 {
		return fields
	}
	return e.fields
}

type editChange func(current, next *lead.Lead) (recordEdit, error)

type recordWriter struct {
	store    lead.Store
	notifier lead.ChangeNotifier
}

func (w recordWriter) record(workspaceID, id string) (*lead.Lead, error) {
	if err := requireRecordRef(workspaceID, id); err != nil {
		return nil, err
	}
	return found(w.store.FindByID(workspaceID, id))
}

func requireRecordRef(workspaceID, id string) error {
	if strings.TrimSpace(workspaceID) == "" {
		return lead.ErrLeadWorkspaceRequired
	}
	if strings.TrimSpace(id) == "" {
		return lead.ErrLeadRequired
	}
	return nil
}

func (w recordWriter) load(ctx context.Context, workspaceID, id string) (*lead.Lead, error) {
	if err := requireRecordRef(workspaceID, id); err != nil {
		return nil, err
	}
	return found(w.store.Load(ctx, workspaceID, id))
}

func (w recordWriter) conflict(ctx context.Context, workspaceID, id string, v lead.Viewer, sent []string) error {
	latest, err := w.load(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	return &lead.VersionConflict{Current: latest.VisibleTo(v, sent)}
}

func (w recordWriter) retrying(ctx context.Context, workspaceID, actorID, id string, kind recordevent.Kind, change recordChange) (*lead.Lead, error) {
	return w.retryingEdit(ctx, workspaceID, id, func(current, next *lead.Lead) (recordEdit, error) {
		fields, err := change(next)
		if err != nil || len(fields) == 0 {
			return recordEdit{}, err
		}
		return recordEdit{events: []recordevent.Event{lead.Changes(kind, actorID, current, next, nil)}, fields: fields}, nil
	})
}

func (w recordWriter) retryingEdit(ctx context.Context, workspaceID, id string, change editChange) (*lead.Lead, error) {
	for attempt := 0; attempt < targetedAttempts; attempt++ {
		current, err := w.load(ctx, workspaceID, id)
		if err != nil {
			return nil, err
		}
		next := *current
		edit, err := change(current, &next)
		if err != nil {
			return nil, err
		}
		if !edit.changed() {
			return current, nil
		}
		err = w.store.Save(ctx, &next, current.Version, edit.events)
		if err == nil {
			w.notify(&next, edit.notified())
			return &next, nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			return nil, err
		}
	}
	return nil, errStillContended
}

func (w recordWriter) notify(l *lead.Lead, fields []string) {
	w.notifier.LeadChanged(lead.Change{
		WorkspaceID: l.WorkspaceID,
		LeadID:      l.ID,
		Version:     l.Version,
		Fields:      fields,
	})
}
