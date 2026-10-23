package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

func multiTriggerWorkflow() *workflow.Workflow {
	return &workflow.Workflow{
		ID: "wf-1", WorkspaceID: "ws1", Name: "Matrícula", Type: workflow.WorkflowTypeMessages, Status: workflow.WorkflowStatusDraft,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "agente", Type: workflow.NodeTypeTriggerAgentCall, Config: map[string]interface{}{
					"display_name": "Chamado pela Bia",
					workflow.AgentCallInputsKey: []interface{}{
						map[string]interface{}{"name": "cpf", "type": "string", "required": true},
						map[string]interface{}{"name": "curso", "type": "string"},
					},
				}},
				{ID: "hook", Type: workflow.NodeTypeTriggerWebhook},
				{ID: "msg", Type: workflow.NodeTypeTriggerMessageReceived},
				{ID: "etapa", Type: workflow.NodeTypeTriggerStageAdded},
				{ID: "copia", Type: workflow.NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "resumo", "value": "{{input.cpf}}|{{webhook.body.nome}}|{{message}}"}},
				{ID: "end", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "agente", Target: "copia"},
				{Source: "hook", Target: "copia"},
				{Source: "msg", Target: "copia"},
				{Source: "etapa", Target: "copia"},
				{Source: "copia", Target: "end"},
			},
		},
	}
}

type readyFrame struct {
	TriggerType          string                      `json:"triggerType"`
	DefaultTriggerNodeID string                      `json:"defaultTriggerNodeId"`
	Triggers             []workflow.TriggerInputSpec `json:"triggers"`
}

type refusedFrame struct {
	Command string                       `json:"command"`
	Error   string                       `json:"error"`
	Code    string                       `json:"code"`
	Issues  []workflow.TriggerInputIssue `json:"issues"`
}

type triggerStartedFrame struct {
	Trigger struct {
		NodeID      string `json:"nodeId"`
		TriggerType string `json:"triggerType"`
	} `json:"trigger"`
}

type failingStages struct{}

func (failingStages) TriggerStages(string) ([]workflow.TriggerInputOption, error) {
	return nil, errors.New("stages unavailable")
}

func TestSimReadyDescribesEveryTriggerWithThePrimaryAsDefault(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: multiTriggerWorkflow(), awaitStart: true})
	ready := decode[readyFrame](t, firstFrame(t, sim, "sim_ready"))

	if ready.DefaultTriggerNodeID != "agente" || ready.TriggerType != string(workflow.TriggerAgentCall) || len(ready.Triggers) != 4 {
		t.Fatalf("sim_ready = %+v", ready)
	}
	agent := ready.Triggers[0]
	if agent.NodeID != "agente" || agent.Label != "Chamado pela Bia" || agent.Shape != workflow.TriggerInputFields || len(agent.Fields) != 3 || agent.Fields[0].Key != "cpf" || !agent.Fields[0].Required {
		t.Fatalf("agent spec = %+v", agent)
	}
	if ready.Triggers[1].Shape != workflow.TriggerInputJSON || ready.Triggers[2].Shape != workflow.TriggerInputMessage {
		t.Fatalf("specs = %+v", ready.Triggers)
	}
}

func firstFrame(t *testing.T, sim *debugSim, want string) simFrame {
	t.Helper()
	frame, _ := readUntil(t, sim.conn, want)
	return frame
}

func TestAStartMissingARequiredAgentInputIsRefusedAndTheSessionKeepsWaiting(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: multiTriggerWorkflow(), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")

	sim.send(t, "start", nil)
	refused := decode[refusedFrame](t, firstFrame(t, sim, "command_refused"))
	if refused.Command != "start" || refused.Code != "trigger_input_invalid" || refused.Error == "" ||
		len(refused.Issues) != 1 || refused.Issues[0] != (workflow.TriggerInputIssue{Code: workflow.IssueRequired, Key: "cpf"}) {
		t.Fatalf("command_refused = %+v", refused)
	}

	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "agente", "input": map[string]interface{}{"cpf": "52998224725", "agent.name": "Bia"}})
	started := decode[triggerStartedFrame](t, firstFrame(t, sim, "sim_started"))
	if started.Trigger.NodeID != "agente" || started.Trigger.TriggerType != string(workflow.TriggerAgentCall) {
		t.Fatalf("sim_started.trigger = %+v", started.Trigger)
	}
	completed, frames := readUntil(t, sim.conn, "run_completed")
	vars := decode[statePayload](t, completed).Vars
	agent, _ := vars["agent"].(map[string]interface{})
	if agent["name"] != "Bia" {
		t.Fatalf("vars = %v, want the run seeded by the agent-call builder", vars)
	}
	if steps := stepsIn(frames); len(steps) == 0 || steps[0].NodeID != "agente" {
		t.Fatalf("steps = %+v, want the run to start at the chosen trigger", steps)
	}
}

func TestAStartOnAnUnknownTriggerIsRefused(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: multiTriggerWorkflow(), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "copia"})
	if refused := decode[refusedFrame](t, firstFrame(t, sim, "command_refused")); refused.Code != "trigger_not_found" || refused.Issues != nil {
		t.Fatalf("command_refused = %+v", refused)
	}
}

func TestAStartFromASecondaryWebhookTriggerSeedsTheBody(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: multiTriggerWorkflow(), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "hook", "input": map[string]interface{}{"nome": "Ana"}})

	started := decode[triggerStartedFrame](t, firstFrame(t, sim, "sim_started"))
	if started.Trigger.NodeID != "hook" || started.Trigger.TriggerType != string(workflow.TriggerWebhook) {
		t.Fatalf("sim_started.trigger = %+v", started.Trigger)
	}
	completed, _ := readUntil(t, sim.conn, "run_completed")
	if resumo := decode[statePayload](t, completed).Vars["resumo"]; resumo != "{{input.cpf}}|Ana|{{message}}" {
		t.Fatalf("resumo = %v, want the webhook body read by the flow", resumo)
	}
}

func TestAStartFromAMessageTriggerWaitsForTheFirstMessage(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: multiTriggerWorkflow(), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "msg", "input": map[string]interface{}{"ignored": true}})

	waiting := decode[map[string]string](t, firstFrame(t, sim, "waiting_trigger"))
	if waiting["nodeId"] != "msg" || waiting["triggerType"] != string(workflow.TriggerMessageReceived) {
		t.Fatalf("waiting_trigger = %v", waiting)
	}
	sim.send(t, "reply", map[string]interface{}{"text": "quero me matricular"})
	if started := decode[triggerStartedFrame](t, firstFrame(t, sim, "sim_started")); started.Trigger.NodeID != "msg" {
		t.Fatalf("sim_started.trigger = %+v", started.Trigger)
	}
	completed, _ := readUntil(t, sim.conn, "run_completed")
	if resumo := decode[statePayload](t, completed).Vars["resumo"]; resumo != "{{input.cpf}}|{{webhook.body.nome}}|quero me matricular" {
		t.Fatalf("resumo = %v", resumo)
	}
}

func TestATriggerWhoseOptionsCannotBeReadRefusesTheStart(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: multiTriggerWorkflow(), awaitStart: true, triggerInputs: TriggerInputReaders{Stages: failingStages{}}})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "etapa", "input": map[string]interface{}{"stage_id": "s1"}})
	if refused := decode[refusedFrame](t, firstFrame(t, sim, "command_refused")); refused.Code != "trigger_input_unavailable" {
		t.Fatalf("command_refused = %+v", refused)
	}
}

func TestASessionWithoutAwaitStartRefusesADefaultTriggerMissingRequiredInputAndWaitsForAStart(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: multiTriggerWorkflow()})

	refusal, before := readUntil(t, sim.conn, "command_refused")
	refused := decode[refusedFrame](t, refusal)
	if refused.Command != "start" || refused.Code != "trigger_input_invalid" ||
		len(refused.Issues) != 1 || refused.Issues[0] != (workflow.TriggerInputIssue{Code: workflow.IssueRequired, Key: "cpf"}) {
		t.Fatalf("command_refused = %+v", refused)
	}
	for _, frame := range before {
		if frame.Type == "sim_started" || frame.Type == "node_executed" {
			t.Fatalf("frames before the refusal = %+v, want nothing run", before)
		}
	}

	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "agente", "input": map[string]interface{}{"cpf": "52998224725", "agent.name": "Bia"}})
	if started := decode[triggerStartedFrame](t, firstFrame(t, sim, "sim_started")); started.Trigger.NodeID != "agente" {
		t.Fatalf("sim_started.trigger = %+v", started.Trigger)
	}
	completed, _ := readUntil(t, sim.conn, "run_completed")
	if agent, _ := decode[statePayload](t, completed).Vars["agent"].(map[string]interface{}); agent["name"] != "Bia" {
		t.Fatalf("agent = %v, want the run seeded by the validated start", agent)
	}
}

func TestASessionWithoutAwaitStartSeedsTheDefaultTriggerThroughTheValidatedStart(t *testing.T) {
	wf := multiTriggerWorkflow()
	wf.Graph.Nodes[0].Config[workflow.AgentCallInputsKey] = []interface{}{map[string]interface{}{"name": "curso", "type": "string"}}
	sim := startDebugSimulation(t, debugSimOptions{workflow: wf})

	started, before := readUntil(t, sim.conn, "sim_started")
	for _, frame := range before {
		if frame.Type == "command_refused" {
			t.Fatalf("frames before the start = %+v", before)
		}
	}
	if trigger := decode[triggerStartedFrame](t, started).Trigger; trigger.NodeID != "agente" {
		t.Fatalf("sim_started.trigger = %+v", trigger)
	}
	completed, _ := readUntil(t, sim.conn, "run_completed")
	if agent, _ := decode[statePayload](t, completed).Vars["agent"].(map[string]interface{}); agent["name"] != "Agente de teste" {
		t.Fatalf("agent = %v, want the default trigger seeded by its builder", agent)
	}
}
