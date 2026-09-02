package conversation_event_usecase

import (
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/actor"
	agentdomain "vozko/domain/agent"
	ce "vozko/domain/conversation_event"
	labeldomain "vozko/domain/label"
	stagedomain "vozko/domain/stage"
	"vozko/domain/user"
)

type listEventsUseCase struct {
	repo   ce.Repository
	users  user.UserRepository
	agents agentdomain.Repository
	stages stagedomain.Repository
	labels labeldomain.Repository
}

// NewListEventsUseCase wires the timeline reader.
//
// Every name source is optional: without them the timeline still renders, it
// just falls back to kinds and ids, which is what it did before. They are
// separate parameters rather than one resolver interface because each
// repository already exposes the batch lookup this needs, and the lead-memory
// timeline resolves its actor labels the same way.
//
// Names are resolved on READ rather than stamped at write time so the timeline
// follows a rename and, more to the point, so every event ALREADY STORED gets
// its names — including the years of rows written while stage and label events
// carried nothing but uuids.
func NewListEventsUseCase(
	repo ce.Repository,
	users user.UserRepository,
	agents agentdomain.Repository,
	stages stagedomain.Repository,
	labels labeldomain.Repository,
) ce.ListEventsUseCase {
	return &listEventsUseCase{repo: repo, users: users, agents: agents, stages: stages, labels: labels}
}

func (uc *listEventsUseCase) Execute(workspaceID, entryID, entryType string, limit, offset int) ([]*ce.ConversationEvent, int64, error) {
	events, total, err := uc.repo.ListByEntry(workspaceID, entryID, entryType, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	uc.resolveNames(events)
	uc.resolveSubjectNames(workspaceID, events)
	return events, total, nil
}

// resolveSubjectNames backfills stage_name / label_name into the details of
// events that stored only an id.
//
// The use cases now write the names at emit time, so this is for history: rows
// written before that rendered as a bare "Stage changed" with nothing saying
// which stage. Two workspace-wide reads for the whole page, not one per row —
// a workspace has tens of stages and labels, so listing them all is cheaper
// than a lookup per event, and it is the same read the board already does.
//
// It never overwrites a name the event already carries.
func (uc *listEventsUseCase) resolveSubjectNames(workspaceID string, events []*ce.ConversationEvent) {
	type subject struct {
		idKey, nameKey string
		names          map[string]string
	}
	subjects := []*subject{
		{idKey: "stage_id", nameKey: "stage_name"},
		{idKey: "to_stage_id", nameKey: "stage_name"},
		{idKey: "from_stage_id", nameKey: "from_stage_name"},
		{idKey: "label_id", nameKey: "label_name"},
	}

	// Load lazily: a page with no stage or label event does no read at all.
	var stageNames, labelNames map[string]string
	needs := func(idKey string) map[string]string {
		if idKey == "label_id" {
			if labelNames == nil {
				labelNames = uc.labelNames(workspaceID)
			}
			return labelNames
		}
		if stageNames == nil {
			stageNames = uc.stageNames(workspaceID)
		}
		return stageNames
	}

	for _, ev := range events {
		if ev == nil {
			continue
		}
		details := ev.DetailsMap()
		if len(details) == 0 {
			continue
		}
		changed := false
		for _, s := range subjects {
			id := strings.TrimSpace(details[s.idKey])
			if id == "" || strings.TrimSpace(details[s.nameKey]) != "" {
				continue
			}
			if s.names == nil {
				s.names = needs(s.idKey)
			}
			if name := s.names[id]; name != "" {
				details[s.nameKey] = name
				changed = true
			}
		}
		if changed {
			ev.Details = ce.DetailsJSON(details)
		}
	}
}

func (uc *listEventsUseCase) stageNames(workspaceID string) map[string]string {
	out := map[string]string{}
	if uc.stages == nil {
		return out
	}
	found, err := uc.stages.ListByWorkspace(workspaceID)
	if err != nil {
		log.Printf("[conversation_event] could not resolve stage names: %v", err)
		return out
	}
	for _, s := range found {
		if s != nil {
			out[s.ID] = s.Name
		}
	}
	return out
}

func (uc *listEventsUseCase) labelNames(workspaceID string) map[string]string {
	out := map[string]string{}
	if uc.labels == nil {
		return out
	}
	found, err := uc.labels.ListByWorkspace(workspaceID)
	if err != nil {
		log.Printf("[conversation_event] could not resolve label names: %v", err)
		return out
	}
	for _, l := range found {
		if l != nil {
			out[l.ID] = l.Name
		}
	}
	return out
}

// resolveNames fills ActorName / FromName / ToName on a page of events.
//
// Two batch queries for the whole page, never one per row: a 50-event page of a
// busy conversation names a handful of distinct people. Best-effort throughout,
// a failed lookup logs and leaves the name empty, and the UI falls back to the
// actor-kind badge it showed before.
func (uc *listEventsUseCase) resolveNames(events []*ce.ConversationEvent) {
	if len(events) == 0 {
		return
	}

	userIDs := map[string]bool{}
	agentIDs := map[string]bool{}
	// want records an id we will need a name for, routed by shape: "ai:x" is an
	// agent, "system"/"" is neither, anything else is a user. The routing is by
	// id and not by the event's actor_kind because from/to ids carry no kind.
	//
	// Non-uuid values are dropped rather than queried: a voice transfer's
	// `target` can be an extension or a queue name, and users.id is a uuid
	// column, so passing one through would fail the whole batch with 22P02 and
	// cost the page every other name.
	want := func(id string) {
		if !isUUID(strings.TrimPrefix(id, actor.AIPrefix)) {
			return
		}
		switch actor.KindOf(id) {
		case actor.KindAI:
			if bare := actor.ParseAI(id); bare != "" {
				agentIDs[bare] = true
			}
		case actor.KindHuman:
			userIDs[id] = true
		}
	}

	details := make([]map[string]string, len(events))
	for i, ev := range events {
		if ev == nil {
			continue
		}
		want(ev.ActorID)
		d := ev.DetailsMap()
		details[i] = d
		want(ce.LookupDetailID(d, ce.FromActorIDKeys))
		want(ce.LookupDetailID(d, ce.ToActorIDKeys))
	}

	names := map[string]string{}
	if uc.users != nil && len(userIDs) > 0 {
		if found, err := uc.users.FindByIDs(mapKeys(userIDs)); err != nil {
			log.Printf("[conversation_event] could not resolve user names: %v", err)
		} else {
			for _, u := range found {
				if u != nil {
					names[u.ID] = u.Username
				}
			}
		}
	}
	if uc.agents != nil && len(agentIDs) > 0 {
		if found, err := uc.agents.FindByIDs(mapKeys(agentIDs)); err != nil {
			log.Printf("[conversation_event] could not resolve agent names: %v", err)
		} else {
			for _, a := range found {
				if a != nil {
					names[actor.FormatAI(a.ID)] = a.Name
				}
			}
		}
	}

	for i, ev := range events {
		if ev == nil {
			continue
		}
		ev.ActorName = names[ev.ActorID]
		ev.FromName = names[ce.LookupDetailID(details[i], ce.FromActorIDKeys)]
		ev.ToName = names[ce.LookupDetailID(details[i], ce.ToActorIDKeys)]
	}
}

// isUUID reports whether s has the shape users.id and agents.id are stored in.
func isUUID(s string) bool {
	_, err := uuid.Parse(strings.TrimSpace(s))
	return err == nil
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
