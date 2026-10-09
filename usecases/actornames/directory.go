package actornames

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/actor"
	"vozko/domain/agent"
	"vozko/domain/user"
	"vozko/domain/workflow"
)

var ErrNameSourceMissing = errors.New("actor names: no source for this kind of actor")

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
	names, err := d.collect(actorIDs, false)
	if err != nil {
		log.Printf("[actor-names] could not resolve every name: %v", err)
	}
	return names
}

func (d Directory) ResolveNames(actorIDs ...string) (map[string]string, error) {
	names, err := d.collect(actorIDs, true)
	if err != nil {
		return nil, err
	}
	return names, nil
}

type nameSource struct {
	kind   actor.Kind
	label  string
	ready  bool
	lookup func(ids []string, names map[string]string) error
}

func (d Directory) sources() []nameSource {
	return []nameSource{
		{kind: actor.KindHuman, label: "user", ready: d.Users != nil, lookup: func(ids []string, names map[string]string) error {
			found, err := d.Users.FindByIDs(ids)
			if err != nil {
				return err
			}
			for _, u := range found {
				if u != nil {
					names[u.ID] = u.Username
				}
			}
			return nil
		}},
		{kind: actor.KindAI, label: "agent", ready: d.Agents != nil, lookup: func(ids []string, names map[string]string) error {
			found, err := d.Agents.FindByIDs(ids)
			if err != nil {
				return err
			}
			for _, a := range found {
				if a != nil {
					names[actor.FormatAI(a.ID)] = a.Name
				}
			}
			return nil
		}},
		{kind: actor.KindWorkflow, label: "workflow", ready: d.Workflows != nil, lookup: func(ids []string, names map[string]string) error {
			found, err := d.Workflows.FindByIDs(ids)
			if err != nil {
				return err
			}
			for _, w := range found {
				if w != nil {
					names[actor.FormatWorkflow(w.ID)] = w.Name
				}
			}
			return nil
		}},
	}
}

func (d Directory) collect(actorIDs []string, strict bool) (map[string]string, error) {
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
	var failures []error
	for _, source := range d.sources() {
		ids := wanted[source.kind]
		if len(ids) == 0 {
			continue
		}
		if !source.ready {
			if strict {
				failures = append(failures, fmt.Errorf("%s names: %w", source.label, ErrNameSourceMissing))
			}
			continue
		}
		if err := source.lookup(keys(ids), names); err != nil {
			failures = append(failures, fmt.Errorf("%s names: %w", source.label, err))
		}
	}
	return names, errors.Join(failures...)
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
