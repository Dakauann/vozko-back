package actornames

import (
	"errors"
	"testing"

	"vozko/domain/actor"
	"vozko/domain/agent"
	"vozko/domain/user"
	"vozko/domain/workflow"
)

const (
	userID     = "11111111-1111-1111-1111-111111111111"
	agentID    = "22222222-2222-2222-2222-222222222222"
	workflowID = "33333333-3333-3333-3333-333333333333"
)

type users struct {
	asked [][]string
	err   error
}

func (u *users) FindByIDs(ids []string) ([]*user.User, error) {
	u.asked = append(u.asked, ids)
	return []*user.User{{ID: userID, Username: "Ana"}}, u.err
}

type agents struct{}

func (agents) FindByIDs([]string) ([]*agent.Agent, error) {
	return []*agent.Agent{{ID: agentID, Name: "Sofia"}}, nil
}

type workflows struct{}

func (workflows) FindByIDs([]string) ([]*workflow.Workflow, error) {
	return []*workflow.Workflow{{ID: workflowID, Name: "Boas-vindas"}}, nil
}

func TestPeopleAgentsAndWorkflowsAreNamedByTheirActorID(t *testing.T) {
	d := Directory{Users: &users{}, Agents: agents{}, Workflows: workflows{}}

	names := d.Names(userID, actor.FormatAI(agentID), actor.FormatWorkflow(workflowID), actor.SystemID, "")

	if names[userID] != "Ana" || names[actor.FormatAI(agentID)] != "Sofia" || names[actor.FormatWorkflow(workflowID)] != "Boas-vindas" {
		t.Fatalf("names = %v", names)
	}
	if _, named := names[actor.SystemID]; named {
		t.Fatal("the system has no looked-up name")
	}
}

func TestEachKindIsLookedUpOnceAndIDsThatAreNotUUIDsAreSkipped(t *testing.T) {
	u := &users{}
	d := Directory{Users: u}

	d.Names(userID, userID, "not-a-uuid")

	if len(u.asked) != 1 || len(u.asked[0]) != 1 || u.asked[0][0] != userID {
		t.Fatalf("asked = %v", u.asked)
	}
}

func TestAFailedLookupLeavesThatKindUnnamed(t *testing.T) {
	d := Directory{Users: &users{err: errors.New("db down")}, Agents: agents{}}

	names := d.Names(userID, actor.FormatAI(agentID))

	if _, named := names[userID]; named {
		t.Fatal("a failed lookup names nobody")
	}
	if names[actor.FormatAI(agentID)] != "Sofia" {
		t.Fatal("other kinds still resolve")
	}
}

func TestAMissingSourceIsSkipped(t *testing.T) {
	if names := (Directory{}).Names(userID, actor.FormatAI(agentID)); len(names) != 0 {
		t.Fatalf("names = %v", names)
	}
}
