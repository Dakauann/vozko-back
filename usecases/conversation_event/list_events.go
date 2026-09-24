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
	"vozko/domain/workflow"
)

// WorkflowNameLookup names the workflows that appear as actors (workflow:<id>).
type WorkflowNameLookup interface {
	FindByIDs(ids []string) ([]*workflow.Workflow, error)
}

type listEventsUseCase struct {
	repo   ce.Repository
	users  user.UserRepository
	agents agentdomain.Repository
	stages stagedomain.Repository
	labels labeldomain.Repository

	workflows WorkflowNameLookup
}

func (uc *listEventsUseCase) SetWorkflowNames(workflows WorkflowNameLookup) {
	uc.workflows = workflows
}

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

func (uc *listEventsUseCase) resolveNames(events []*ce.ConversationEvent) {
	if len(events) == 0 {
		return
	}

	userIDs := map[string]bool{}
	agentIDs := map[string]bool{}
	workflowIDs := map[string]bool{}
	want := func(id string) {
		switch actor.KindOf(id) {
		case actor.KindAI:
			if bare := actor.ParseAI(id); isUUID(bare) {
				agentIDs[bare] = true
			}
		case actor.KindWorkflow:
			if bare := actor.ParseWorkflow(id); isUUID(bare) {
				workflowIDs[bare] = true
			}
		case actor.KindHuman:
			if isUUID(id) {
				userIDs[id] = true
			}
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

	if uc.workflows != nil && len(workflowIDs) > 0 {
		if found, err := uc.workflows.FindByIDs(mapKeys(workflowIDs)); err != nil {
			log.Printf("[conversation_event] could not resolve workflow names: %v", err)
		} else {
			for _, w := range found {
				if w != nil {
					names[actor.FormatWorkflow(w.ID)] = w.Name
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
