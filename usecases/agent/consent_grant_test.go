package agent_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/agent"
	toolsdomain "vozko/domain/tools"
	"vozko/domain/workspace"
	tools_usecase "vozko/usecases/tools"
)

type agentStore struct {
	agent.Repository
	byID   map[string]*agent.Agent
	writes int
}

func (s *agentStore) Create(a *agent.Agent) error {
	s.writes++
	s.byID[a.ID] = a
	return nil
}

func (s *agentStore) Update(id string, a *agent.Agent) error {
	s.writes++
	s.byID[id] = a
	return nil
}

func (s *agentStore) FindByID(id string) (*agent.Agent, error) {
	stored, ok := s.byID[id]
	if !ok {
		return nil, nil
	}
	clone := *stored
	clone.InternalTools = append([]agent.ToolBinding(nil), stored.InternalTools...)
	return &clone, nil
}

type consentRegistry struct{ toolsdomain.Service }

func (consentRegistry) Definitions() []toolsdomain.Definition {
	return []toolsdomain.Definition{
		{Name: tools_usecase.RecordConsentToolName, ConfigSchema: map[string]toolsdomain.ConfigParameter{"purpose": {Type: "string"}}},
		{Name: tools_usecase.RecordOptOutToolName},
	}
}

type grantors map[string]bool

func (g grantors) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	return resource == "leads" && (action != "consent" || g[userID])
}

func consentBinding(purpose string) agent.ToolBinding {
	return agent.ToolBinding{Name: tools_usecase.RecordConsentToolName, Config: map[string]interface{}{"purpose": purpose}}
}

func optOutBinding() agent.ToolBinding {
	return agent.ToolBinding{Name: tools_usecase.RecordOptOutToolName}
}

func agentFixture(checker workspace.PermissionChecker) (agent.CreateAgentUseCase, agent.UpdateAgentUseCase, *agentStore) {
	store := &agentStore{byID: map[string]*agent.Agent{
		"ag-grant": {ID: "ag-grant", WorkspaceID: "ws1", Name: "Ana", MessagingPrompt: "p", MessagingModel: "m", Provider: agent.AgentProviderAI,
			InternalTools: []agent.ToolBinding{consentBinding("ofertas")}},
		"ag-plain": {ID: "ag-plain", WorkspaceID: "ws1", Name: "Bia", MessagingPrompt: "p", MessagingModel: "m", Provider: agent.AgentProviderAI,
			InternalTools: []agent.ToolBinding{optOutBinding()}},
	}}
	create := NewCreateAgentUseCase(store, nil, consentRegistry{}, nil, nil)
	update := NewUpdateAgentUseCase(store, nil, consentRegistry{}, nil, nil)
	if checker != nil {
		create.(ConsentGrantGuarded).GuardConsentGrants(checker)
		update.(ConsentGrantGuarded).GuardConsentGrants(checker)
	}
	return create, update, store
}

var agentGrantors = grantors{"u-grantor": true}

func createInput(by string, bindings ...agent.ToolBinding) agent.CreateAgentInput {
	return agent.CreateAgentInput{WorkspaceID: "ws1", Name: "Nova", MessagingPrompt: "p", MessagingModel: "m", Provider: agent.AgentProviderAI,
		InternalTools: bindings, RequestedBy: workspace.Requester{UserID: by}}
}

func TestCreatingAnAgentWithTheConsentSkillNeedsTheConsentPermission(t *testing.T) {
	create, _, store := agentFixture(agentGrantors)
	if _, err := create.Execute(context.Background(), createInput("u-plain", consentBinding(""))); !errors.Is(err, workspace.ErrConsentGrantForbidden) {
		t.Fatalf("err = %v", err)
	}
	if store.writes != 0 {
		t.Fatal("an agent was saved without the permission")
	}
	if _, err := create.Execute(context.Background(), createInput("u-grantor", consentBinding(""))); err != nil {
		t.Fatal(err)
	}
	if _, err := create.Execute(context.Background(), createInput("u-plain", optOutBinding())); err != nil {
		t.Fatalf("an agent without the consent skill needs no permission: %v", err)
	}
	if store.writes != 2 {
		t.Fatalf("writes = %d", store.writes)
	}
}

func TestUpdatingAnAgentNeedsTheConsentPermissionOnlyForANewOrChangedConsentSkill(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		tools   []agent.ToolBinding
		refused bool
	}{
		{"the skill kept as it was", "ag-grant", []agent.ToolBinding{consentBinding("ofertas"), optOutBinding()}, false},
		{"tools left untouched", "ag-grant", nil, false},
		{"the purpose changed", "ag-grant", []agent.ToolBinding{consentBinding("cobranca")}, true},
		{"the skill added", "ag-plain", []agent.ToolBinding{optOutBinding(), consentBinding("")}, true},
		{"the skill removed", "ag-grant", []agent.ToolBinding{optOutBinding()}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, update, store := agentFixture(agentGrantors)
			in := func(by string) agent.UpdateAgentInput {
				return agent.UpdateAgentInput{InternalTools: tc.tools, RequestedBy: workspace.Requester{UserID: by}}
			}
			_, err := update.Execute(context.Background(), tc.id, in("u-plain"))
			if !tc.refused {
				if err != nil || store.writes != 1 {
					t.Fatalf("err %v writes %d", err, store.writes)
				}
				return
			}
			if !errors.Is(err, workspace.ErrConsentGrantForbidden) || store.writes != 0 {
				t.Fatalf("err %v writes %d", err, store.writes)
			}
			if _, err := update.Execute(context.Background(), tc.id, in("u-grantor")); err != nil || store.writes != 1 {
				t.Fatalf("a grantor was refused: %v", err)
			}
		})
	}
}

func TestAnAgentConsentSkillFailsClosedWithoutAPermissionChecker(t *testing.T) {
	create, update, store := agentFixture(nil)
	if _, err := create.Execute(context.Background(), createInput("u-grantor", consentBinding(""))); !errors.Is(err, workspace.ErrConsentGrantForbidden) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := update.Execute(context.Background(), "ag-plain", agent.UpdateAgentInput{InternalTools: []agent.ToolBinding{consentBinding("")},
		RequestedBy: workspace.Requester{UserID: "u-grantor"}}); !errors.Is(err, workspace.ErrConsentGrantForbidden) {
		t.Fatalf("update err = %v", err)
	}
	if store.writes != 0 {
		t.Fatalf("writes = %d", store.writes)
	}
}
