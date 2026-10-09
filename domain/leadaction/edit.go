package leadaction

import (
	"encoding/json"
	"fmt"
	"strings"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/selection"
)

type EditKind = lead.AssignmentKind

const (
	EditCustomField = lead.AssignCustomField
	EditOwner       = lead.AssignOwner
	EditBlocked     = lead.AssignBlocked
)

type Edit struct {
	Kind      EditKind
	Field     string
	Key       string
	Value     any
	Recorded  bool
	EventKind recordevent.Kind
}

func EditFor(a Action, p Params, defs []*customfield.Definition, viewer customfield.Viewer) (Edit, error) {
	if !a.Runs() {
		return Edit{}, ErrNotARun
	}
	if err := p.Validate(a); err != nil {
		return Edit{}, err
	}
	p = p.Normalized()
	switch a {
	case ActionClassify:
		var value any
		if err := json.Unmarshal(p.Value, &value); err != nil {
			return Edit{}, fmt.Errorf("%w: %v", ErrValueRequired, err)
		}
		def, err := customfield.ValidateOne(defs, viewer, p.Key, value)
		if err != nil {
			return Edit{}, err
		}
		return Edit{Kind: EditCustomField, Field: lead.CustomFieldName(def.Key), Key: def.Key, Value: value, Recorded: customfield.Recordable(defs, def.Key), EventKind: lead.EventUpdated}, nil
	case ActionAssignOwner:
		owner := strings.TrimSpace(*p.OwnerID)
		if err := lead.ValidateOwner(owner); err != nil {
			return Edit{}, err
		}
		return Edit{Kind: EditOwner, Field: lead.FieldOwner, Value: owner, Recorded: true, EventKind: lead.EventOwnerChange}, nil
	default:
		kind := lead.EventUnblocked
		if *p.Blocked {
			kind = lead.EventBlocked
		}
		return Edit{Kind: EditBlocked, Field: lead.FieldBlocked, Value: *p.Blocked, Recorded: true, EventKind: kind}, nil
	}
}

func (e Edit) Blocks() bool {
	blocks, _ := e.Value.(bool)
	return e.Kind == EditBlocked && blocks
}

func (e Edit) Owner() string {
	owner, _ := e.Value.(string)
	return owner
}

func (e Edit) Event(actorID string, before any) recordevent.Event {
	was, now := present(before), present(e.Value)
	if e.Kind == EditBlocked {
		was, now = flagged(!e.Blocks()), flagged(e.Blocks())
	}
	return lead.FieldChange(e.EventKind, actorID, e.Field, was, now, e.Recorded)
}

func (e Edit) Assignment() lead.Assignment {
	return lead.Assignment{Kind: e.Kind, Key: e.Key, Value: e.Value}
}

func (e Edit) PendingFor(s selection.Selection) *lead.Assignment {
	if s.Mode != selection.ModeFirstN {
		return nil
	}
	a := e.Assignment()
	return &a
}

func flagged(on bool) any {
	if on {
		return true
	}
	return nil
}

func present(v any) any {
	if s, ok := v.(string); ok && s == "" {
		return nil
	}
	return v
}
