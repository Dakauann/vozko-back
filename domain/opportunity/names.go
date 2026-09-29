package opportunity

import "vozko/domain/actor"

func NameParticipants(namer actor.Namer, opportunities ...*Opportunity) {
	if namer == nil {
		return
	}
	ids := make([]string, 0, len(opportunities)*3)
	for _, o := range opportunities {
		if o != nil {
			ids = append(ids, o.OwnerID, o.CreatedBy, o.ClosedBy)
		}
	}
	if len(ids) == 0 {
		return
	}
	names := namer.Names(ids...)
	for _, o := range opportunities {
		if o != nil {
			o.OwnerName = names[o.OwnerID]
			o.CreatedByName = names[o.CreatedBy]
			o.ClosedByName = names[o.ClosedBy]
		}
	}
}

func NameEventActors(namer actor.Namer, events []Event) {
	if namer == nil || len(events) == 0 {
		return
	}
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ActorID)
	}
	names := namer.Names(ids...)
	for i := range events {
		events[i].ActorName = names[events[i].ActorID]
	}
}
