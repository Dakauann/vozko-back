package actornames

import (
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/actor"
	"vozko/domain/agent"
	"vozko/domain/user"
	"vozko/domain/workflow"
)

type UserLookup interface {
	FindByIDs(ids []string) ([]*user.User, error)
}

type AgentLookup interface {
	FindByIDs(ids []string) ([]*agent.Agent, error)
}

type WorkflowLookup interface {
	FindByIDs(ids []string) ([]*workflow.Workflow, error)
}

type Directory struct {
	Users     UserLookup
	Agents    AgentLookup
	Workflows WorkflowLookup
}

func (d Directory) Names(actorIDs ...string) map[string]string {
	wanted := map[actor.Kind]map[string]bool{actor.KindHuman: {}, actor.KindAI: {}, actor.KindWorkflow: {}}
	for _, id := range actorIDs {
		kind := actor.KindOf(id)
		if bare := bareID(kind, id); isUUID(bare) {
			if ids, ok := wanted[kind]; ok {
				ids[bare] = true
			}
		}
	}

	names := map[string]string{}
	if d.Users != nil && len(wanted[actor.KindHuman]) > 0 {
		found, err := d.Users.FindByIDs(keys(wanted[actor.KindHuman]))
		if resolved(err, "user") {
			for _, u := range found {
				if u != nil {
					names[u.ID] = u.Username
				}
			}
		}
	}
	if d.Agents != nil && len(wanted[actor.KindAI]) > 0 {
		found, err := d.Agents.FindByIDs(keys(wanted[actor.KindAI]))
		if resolved(err, "agent") {
			for _, a := range found {
				if a != nil {
					names[actor.FormatAI(a.ID)] = a.Name
				}
			}
		}
	}
	if d.Workflows != nil && len(wanted[actor.KindWorkflow]) > 0 {
		found, err := d.Workflows.FindByIDs(keys(wanted[actor.KindWorkflow]))
		if resolved(err, "workflow") {
			for _, w := range found {
				if w != nil {
					names[actor.FormatWorkflow(w.ID)] = w.Name
				}
			}
		}
	}
	return names
}

func bareID(kind actor.Kind, id string) string {
	switch kind {
	case actor.KindAI:
		return actor.ParseAI(id)
	case actor.KindWorkflow:
		return actor.ParseWorkflow(id)
	}
	return strings.TrimSpace(id)
}

func resolved(err error, kind string) bool {
	if err != nil {
		log.Printf("[actor-names] could not resolve %s names: %v", kind, err)
		return false
	}
	return true
}

func isUUID(s string) bool {
	_, err := uuid.Parse(strings.TrimSpace(s))
	return err == nil
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
