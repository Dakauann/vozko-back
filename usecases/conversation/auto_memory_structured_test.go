package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	toolsdomain "vozko/domain/tools"
	tools_usecase "vozko/usecases/tools"
)

type namedHandler struct {
	toolsdomain.Handler
	name string
}

func (h namedHandler) Definition() toolsdomain.Definition {
	return toolsdomain.Definition{Name: h.name, Parameters: map[string]toolsdomain.Parameter{
		tools_usecase.ProfileBirthDateParam: {Type: "string"}, "bairro": {Type: "string"}, "campos": {Type: "object"},
	}}
}

type agentsStub struct {
	agents map[string]*agent.Agent
	err    error
}

func (a agentsStub) FindByID(id string) (*agent.Agent, error) {
	return a.agents[id], a.err
}

func profileAgentWith(workspaceID string, names ...string) agentsStub {
	bindings := make([]agent.ToolBinding, 0, len(names))
	for _, name := range names {
		bindings = append(bindings, agent.ToolBinding{Name: name})
	}
	return agentsStub{agents: map[string]*agent.Agent{"agent-1": {ID: "agent-1", WorkspaceID: workspaceID, InternalTools: bindings}}}
}

func memoryJob(agents ProfileAgents, handlers ...string) (*analysisDebounceJob, *generateRecorder) {
	recorder := &generateRecorder{}
	registered := map[string]toolsdomain.Handler{}
	for _, name := range handlers {
		registered[name] = namedHandler{name: name}
	}
	subject := instagramSubject()
	subject.EnableAutoMemory = true
	job := &analysisDebounceJob{
		messageRepo:  transcriptStub{},
		aiService:    recorder,
		toolRegistry: registryStub{handlers: registered},
		resolvers: map[shared.EntryType]AnalysisSubjectResolver{
			shared.EntryTypeInstagram: func(context.Context, string) (*AnalysisSubject, error) { return &subject, nil },
		},
	}
	if agents != nil {
		job.SetProfileAgents(agents)
	}
	return job, recorder
}

func memoryInput(t *testing.T, recorder *generateRecorder) ai.GenerateInput {
	t.Helper()
	for _, input := range recorder.inputs {
		for _, tool := range input.Tools {
			if tool.Name == tools_usecase.ManageLeadMemoryToolName {
				return input
			}
		}
	}
	t.Fatalf("no memory task among %d model calls", len(recorder.inputs))
	return ai.GenerateInput{}
}

func TestAutoMemoryRecordsTheBirthDateOnlyWhenTheAgentMayUpdateTheProfile(t *testing.T) {
	agents := profileAgentWith("ws", tools_usecase.ManageLeadMemoryToolName, tools_usecase.UpdateLeadProfileToolName)
	job, recorder := memoryJob(agents, tools_usecase.ManageLeadMemoryToolName, tools_usecase.UpdateLeadProfileToolName)
	if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err != nil {
		t.Fatal(err)
	}
	input := memoryInput(t, recorder)
	if len(input.Tools) != 2 || input.Tools[1].Name != tools_usecase.UpdateLeadProfileToolName {
		t.Fatalf("tools = %+v", input.Tools)
	}
	if params := input.Tools[1].Parameters; len(params) != 1 || params[tools_usecase.ProfileBirthDateParam].Type != "string" {
		t.Fatalf("the profile tool offers %+v", params)
	}
	for _, name := range []string{tools_usecase.ManageLeadMemoryToolName, tools_usecase.UpdateLeadProfileToolName} {
		config := input.ToolConfigs[name]
		if config["__lead_id"] != "lead-1" || config["__workspace_id"] != "ws" || config["__agent_id"] != "agent-1" {
			t.Fatalf("%s config = %+v", name, config)
		}
	}
	for key, value := range tools_usecase.BirthDateOnlyProfileConfig() {
		if _, ok := input.ToolConfigs[tools_usecase.UpdateLeadProfileToolName][key]; !ok || value == nil {
			t.Fatalf("the profile tool runs without its scope: %+v", input.ToolConfigs[tools_usecase.UpdateLeadProfileToolName])
		}
		if _, leaked := input.ToolConfigs[tools_usecase.ManageLeadMemoryToolName][key]; leaked {
			t.Fatal("the memory tool got the profile scope")
		}
	}
	if !strings.Contains(input.SystemPrompt, "update_lead_profile") || !strings.Contains(input.SystemPrompt, "Família") || strings.Contains(input.SystemPrompt, "o bairro ou o endereço") {
		t.Fatalf("the memory prompt must route only birthdays to the record and relatives to Família:\n%s", input.SystemPrompt)
	}
}

func TestAutoMemoryNeverWritesTheProfileWithoutTheAgentOptIn(t *testing.T) {
	cases := map[string]ProfileAgents{
		"no agent directory":           nil,
		"an agent without the tool":    profileAgentWith("ws", tools_usecase.ManageLeadMemoryToolName),
		"an agent of another place":    profileAgentWith("other-ws", tools_usecase.UpdateLeadProfileToolName),
		"an agent that is gone":        agentsStub{agents: map[string]*agent.Agent{}},
		"an agent that cannot be read": agentsStub{err: errors.New("db down")},
	}
	for name, agents := range cases {
		t.Run(name, func(t *testing.T) {
			job, recorder := memoryJob(agents, tools_usecase.ManageLeadMemoryToolName, tools_usecase.UpdateLeadProfileToolName)
			if err := job.runAnalysisForEntry("entry-1", shared.EntryTypeInstagram); err != nil {
				t.Fatal(err)
			}
			input := memoryInput(t, recorder)
			if len(input.Tools) != 1 {
				t.Fatalf("tools = %+v", input.Tools)
			}
			if strings.Contains(input.SystemPrompt, "update_lead_profile") || !strings.Contains(input.SystemPrompt, "data de nascimento") {
				t.Fatalf("without the profile tool the prompt still keeps birthdays out of memory, without naming a missing tool:\n%s", input.SystemPrompt)
			}
		})
	}
}

func TestTheAutoMemoryRulesNoLongerAskForRelativesOrImportantDates(t *testing.T) {
	prompt := BuildAutoMemoryPrompt(AutoMemoryPromptInput{ContainerName: "c", History: []*conversation.Message{customerSays("oi")}})
	if strings.Contains(prompt.System, "datas importantes") {
		t.Fatalf("the rules still ask to remember dates as free text:\n%s", prompt.System)
	}
}
