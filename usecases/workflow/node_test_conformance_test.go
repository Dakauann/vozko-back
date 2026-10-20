package workflow_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/tracepayload"
	"vozko/domain/workflow"
	email_usecase "vozko/usecases/email"
	"vozko/usecases/workflow/node_executors"
)

const (
	conformanceSecret      = "conformance-secret-value-91"
	conformanceCaseTimeout = 2 * time.Second
	conformanceHangGuard   = 10 * time.Second
)

type conformanceSpies struct {
	mu        sync.Mutex
	http      []string
	emails    []string
	outbound  []string
	waFactory int
}

var errConformanceNetworkRefused = errors.New("conformance: the network is closed in the node test suite")

func (s *conformanceSpies) RoundTrip(r *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	headers := make([]string, 0, len(r.Header))
	for k, v := range r.Header {
		headers = append(headers, k+"="+strings.Join(v, ","))
	}
	s.http = append(s.http, r.Method+" "+r.URL.String()+" "+strings.Join(headers, ";"))
	return nil, errConformanceNetworkRefused
}

func (s *conformanceSpies) Send(_ context.Context, _ email_usecase.SMTPConfig, message email_usecase.EmailMessage) (*email_usecase.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emails = append(s.emails, strings.Join(message.To, ",")+" "+message.Subject+" "+message.Body)
	return nil, errConformanceNetworkRefused
}

func (s *conformanceSpies) ClientForPhone(string) (conversation.WhatsAppClient, error) {
	s.mu.Lock()
	s.waFactory++
	s.mu.Unlock()
	return nil, errConformanceNetworkRefused
}

func (s *conformanceSpies) ClientForWABA(string) (conversation.WhatsAppClient, error) {
	return s.ClientForPhone("")
}

func (s *conformanceSpies) WABAIdForPhone(string) (string, error) {
	return "", errConformanceNetworkRefused
}

func (s *conformanceSpies) counts() (int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.http), len(s.emails), s.waFactory
}

func (s *conformanceSpies) texts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(append(append([]string(nil), s.http...), s.emails...), s.outbound...)
}

func (s *conformanceSpies) drain(outbound <-chan SimOutboundMessage) {
	for msg := range outbound {
		s.mu.Lock()
		s.outbound = append(s.outbound, msg.Text)
		s.mu.Unlock()
	}
}

type stubModel struct {
	ai.Service
}

func (stubModel) Generate(context.Context, ai.GenerateInput) (*ai.GenerateOutput, error) {
	return &ai.GenerateOutput{Message: ai.Message{Role: "assistant", Content: "Olá, sou o modelo de teste."}, FinishReason: "stop"}, nil
}

func (stubModel) GetAvaibleModels(context.Context) ([]string, error) {
	return []string{"stub/model"}, nil
}

func (stubModel) GetModelsWithPricing(context.Context) ([]ai.ModelInfo, error) {
	return []ai.ModelInfo{{ID: "stub/model", Name: "stub"}}, nil
}

type conformanceSecrets map[string]string

func (s conformanceSecrets) Resolve(_ context.Context, _ string, names []string) (map[string]string, error) {
	out := map[string]string{}
	for _, n := range names {
		if v, ok := s[n]; ok {
			out[n] = v
		}
	}
	return out, nil
}

type conformance struct {
	t        *testing.T
	spies    *conformanceSpies
	deps     ExecutorDeps
	outbound chan SimOutboundMessage
}

func newConformance(t *testing.T) *conformance {
	spies := &conformanceSpies{}
	c := &conformance{
		t:     t,
		spies: spies,
		deps: ExecutorDeps{
			AIService:             stubModel{},
			HTTPEgress:            node_executors.HTTPEgress{Client: &http.Client{Transport: spies}, LegacyTLS: &http.Client{Transport: spies}},
			EmailSender:           spies,
			Secrets:               conformanceSecrets{"token": conformanceSecret},
			WhatsAppClientFactory: spies,
		},
		outbound: make(chan SimOutboundMessage, 64),
	}
	go spies.drain(c.outbound)
	return c
}

func (c *conformance) executors() TestExecutors {
	return func(box SimSandbox) *NodeExecutorRegistry {
		box.Outbound = c.outbound
		return newSimulationRegistry(c.deps, box)
	}
}

func (c *conformance) catalog() []workflow.NodeDefinition {
	defs := newSimulationRegistry(c.deps, SimSandbox{WorkspaceID: "ws1", Messages: &simMessageRepo{}, Outbound: c.outbound}).Catalog()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Type < defs[j].Type })
	return defs
}

func conformanceWorkflow(def workflow.NodeDefinition, wfType workflow.WorkflowType, config map[string]interface{}) *workflow.Workflow {
	target := workflow.Node{ID: "n", Type: def.Type, Config: config}
	nodes := []workflow.Node{target, {ID: "end", Type: workflow.NodeTypeEnd}}
	edges := []workflow.Edge{{Source: "n", Target: "end"}}
	if !def.Type.IsTrigger() {
		trigger := workflow.NodeTypeTriggerMessageReceived
		if wfType == workflow.WorkflowTypeVoice {
			trigger = workflow.NodeTypeTriggerCallReceived
		}
		nodes = append([]workflow.Node{{ID: "t", Type: trigger, Config: map[string]interface{}{}}}, nodes...)
		edges = []workflow.Edge{{Source: "t", Target: "n"}}
		if !def.Type.IsEnd() {
			edges = append(edges, workflow.Edge{Source: "n", Target: "end"})
			for _, h := range def.Outputs {
				edges = append(edges, workflow.Edge{Source: "n", Target: "end", Label: h.ID})
			}
		}
	}
	return &workflow.Workflow{ID: "wf-conf", WorkspaceID: "ws1", Name: "Conformidade", Type: wfType, Graph: workflow.Graph{Nodes: nodes, Edges: edges}}
}

func cloneConfig(config map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(config))
	for k, v := range config {
		out[k] = v
	}
	return out
}

func wrongTypedConfig(config map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(config))
	for k, v := range config {
		switch v.(type) {
		case string:
			out[k] = float64(123)
		case float64, int, int64:
			out[k] = "não é número"
		case bool:
			out[k] = "talvez"
		case map[string]interface{}:
			out[k] = []interface{}{"x"}
		case []interface{}:
			out[k] = map[string]interface{}{"x": 1}
		default:
			out[k] = "?"
		}
	}
	return out
}

func templatedConfig(def workflow.NodeDefinition, template string) map[string]interface{} {
	out := cloneConfig(def.DefaultConfig)
	for k, v := range out {
		if _, isText := v.(string); isText {
			out[k] = template
		}
	}
	for _, f := range def.ConfigSchema {
		if _, set := out[f.Key]; !set && (f.Type == "text" || f.Type == "textarea" || f.Type == "string") {
			out[f.Key] = template
		}
	}
	return out
}

type conformanceRun struct {
	name      string
	wf        *workflow.Workflow
	input     TestNodeInput
	executors TestExecutors
	timeout   time.Duration
}

func (c *conformance) execute(run conformanceRun) (*TestNodeOutput, error) {
	repo := NewMockWorkflowRepository()
	_ = repo.Create(run.wf)
	executors := run.executors
	if executors == nil {
		executors = c.executors()
	}
	timeout := run.timeout
	if timeout == 0 {
		timeout = conformanceCaseTimeout
	}
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Executors: executors, Timeout: timeout})
	if run.input.WorkflowID == "" {
		run.input.WorkflowID = run.wf.ID
	}
	if run.input.WorkspaceID == "" {
		run.input.WorkspaceID = run.wf.WorkspaceID
	}
	if run.input.NodeID == "" {
		run.input.NodeID = "n"
	}
	type answer struct {
		out *TestNodeOutput
		err error
	}
	done := make(chan answer, 1)
	go func() {
		out, err := uc.Execute(context.Background(), run.input)
		done <- answer{out, err}
	}()
	select {
	case a := <-done:
		return a.out, a.err
	case <-time.After(conformanceHangGuard):
		c.t.Fatalf("%s: the node test hung past %s", run.name, conformanceHangGuard)
		return nil, nil
	}
}

func (c *conformance) structured(name string, out *TestNodeOutput, err error) {
	c.t.Helper()
	if err != nil {
		c.t.Fatalf("%s: a node test answers with a result, not an error: %v", name, err)
	}
	if out == nil {
		c.t.Fatalf("%s: no result", name)
	}
	if !out.Success && out.ErrorKind == "" {
		c.t.Fatalf("%s: a failed test must carry an error kind: %+v", name, out)
	}
	for _, step := range append([]*workflow.WorkflowRunLog{out.Step}, out.UpstreamSteps...) {
		if step != nil && step.Status == workflow.LogStatusFailed && step.ErrorKind == "" {
			c.t.Fatalf("%s: a failed step must carry an error kind: %+v", name, step)
		}
	}
	if _, jerr := json.Marshal(out); jerr != nil {
		c.t.Fatalf("%s: the result must serialise: %v", name, jerr)
	}
	if _, _, wa := c.spies.counts(); wa != 0 {
		c.t.Fatalf("%s: the live WhatsApp client factory was used %d times", name, wa)
	}
}

func (c *conformance) realExecutor(name string, out *TestNodeOutput) {
	c.t.Helper()
	if out.ErrorKind == workflow.ErrorKindPanic || (out.Step != nil && out.Step.ErrorKind == workflow.ErrorKindPanic) {
		c.t.Errorf("%s: the executor panicked instead of refusing the input: %s", name, out.Error)
	}
}

func (c *conformance) noRealCalls(name string, before [3]int) {
	c.t.Helper()
	h, e, w := c.spies.counts()
	if h != before[0] || e != before[1] || w != before[2] {
		c.t.Fatalf("%s: a real call went out without confirmation (http %d->%d, email %d->%d)", name, before[0], h, before[1], e)
	}
}

func (c *conformance) snapshot() [3]int {
	h, e, w := c.spies.counts()
	return [3]int{h, e, w}
}

func (c *conformance) noNilText(name string, out *TestNodeOutput, before int) {
	c.t.Helper()
	texts := c.spies.texts()
	for _, text := range texts[min(before, len(texts)):] {
		if strings.Contains(text, "<nil>") {
			c.t.Errorf("%s: a null value went out as <nil>: %q", name, text)
		}
	}
	if out.Step != nil {
		encoded, _ := json.Marshal(out.Step.Input)
		if strings.Contains(string(encoded), "<nil>") {
			c.t.Errorf("%s: the parameters used show <nil>: %s", name, encoded)
		}
	}
}

func workflowTypesOf(def workflow.NodeDefinition) []workflow.WorkflowType {
	if def.Type.IsTrigger() {
		return []workflow.WorkflowType{workflow.TriggerType(def.Type).WorkflowType()}
	}
	return []workflow.WorkflowType{workflow.WorkflowTypeMessages, workflow.WorkflowTypeVoice}
}

func TestEveryRegisteredNodeTypeDeclaresAConsistentTestBehaviour(t *testing.T) {
	c := newConformance(t)
	defs := c.catalog()
	if len(defs) < 40 {
		t.Fatalf("the catalog looks incomplete: %d definitions", len(defs))
	}
	for _, def := range defs {
		if err := workflow.TestBehaviorConsistent(def); err != nil {
			t.Errorf("%s: %v", def.Type, err)
		}
	}
}

func TestEveryRegisteredNodeTypeAnswersTheNodeTestWithAStructuredResult(t *testing.T) {
	c := newConformance(t)
	for _, def := range c.catalog() {
		for _, wfType := range workflowTypesOf(def) {
			c.conformType(def, wfType)
		}
	}
}

func (c *conformance) conformType(def workflow.NodeDefinition, wfType workflow.WorkflowType) {
	label := fmt.Sprintf("%s in %s", def.Type, wfType)
	allowed := def.Type.IsTrigger() || def.Type.IsEnd() || workflow.DefinitionAllowedForType(def, wfType)

	before := c.snapshot()
	out, err := c.execute(conformanceRun{name: label + " default config", wf: conformanceWorkflow(def, wfType, cloneConfig(def.DefaultConfig))})
	c.structured(label+" default config", out, err)
	c.noRealCalls(label+" default config", before)
	if !allowed {
		if out.ErrorKind != workflow.ErrorKindWrongWorkflowType {
			c.t.Errorf("%s: a node of the other workflow type must be refused as wrong_workflow_type, got %+v", label, out)
		}
		return
	}
	c.realExecutor(label+" default config", out)
	if workflow.ExternalCallNode(workflow.Node{Type: def.Type, Config: def.DefaultConfig}) && out.ErrorKind != workflow.ErrorKindRealCallNotConfirmed {
		c.t.Errorf("%s: an external call without confirmation must be refused, got %+v", label, out)
	}

	confirmed := TestNodeInput{ConfirmRealCalls: true}
	cases := []struct {
		name   string
		config map[string]interface{}
		input  TestNodeInput
	}{
		{"default config confirmed", cloneConfig(def.DefaultConfig), confirmed},
		{"empty config", map[string]interface{}{}, confirmed},
		{"wrong config types", wrongTypedConfig(def.DefaultConfig), confirmed},
		{"missing upstream variables", templatedConfig(def, "{{ai.response}}"), confirmed},
		{"secret that exists", templatedConfig(def, "{{secret.token}}"), confirmed},
		{"secret that does not exist", templatedConfig(def, "{{secret.nao_existe}}"), confirmed},
	}
	for _, tc := range cases {
		name := label + " " + tc.name
		out, err := c.execute(conformanceRun{name: name, wf: conformanceWorkflow(def, wfType, tc.config), input: tc.input})
		c.structured(name, out, err)
		c.realExecutor(name, out)
		encoded, _ := json.Marshal(out)
		if strings.Contains(string(encoded), conformanceSecret) {
			c.t.Errorf("%s: a resolved secret reached the response", name)
		}
	}

	texts := len(c.spies.texts())
	nilName := label + " null variable"
	out, err = c.execute(conformanceRun{name: nilName, wf: conformanceWorkflow(def, wfType, templatedConfig(def, "{{nulo}}")), input: TestNodeInput{ConfirmRealCalls: true, TriggerVars: map[string]interface{}{"nulo": nil}}})
	c.structured(nilName, out, err)
	c.realExecutor(nilName, out)
	c.noNilText(nilName, out, texts)

	partial := TestNodeInput{MockedState: map[string]interface{}{"sem_relacao": "x"}}
	if mocks := workflow.GetRequiredMocks(workflow.ExtractDependencies(templatedConfig(def, "{{ai.response}}"))); len(mocks) > 0 && !def.Type.IsTrigger() {
		name := label + " required mocks missing"
		out, err := c.execute(conformanceRun{name: name, wf: conformanceWorkflow(def, wfType, templatedConfig(def, "{{ai.response}}")), input: partial})
		c.structured(name, out, err)
		if out.ErrorKind != workflow.ErrorKindMissingMocks {
			c.t.Errorf("%s: missing mocks must be named, got %+v", name, out)
		}
	}

	if def.TestBehavior == workflow.NodeTestWaits {
		name := label + " waits"
		out, err := c.execute(conformanceRun{name: name, wf: conformanceWorkflow(def, wfType, cloneConfig(def.DefaultConfig)), input: confirmed})
		c.structured(name, out, err)
		if out.Success && out.Wait == nil && out.Step != nil && out.Step.Status == workflow.LogStatusWaiting {
			c.t.Errorf("%s: a wait must be described, not silent: %+v", name, out)
		}
	}

	c.conformWrapper(def, wfType, label)
	c.conformAccess(def, wfType, label)
}

func (c *conformance) conformWrapper(def workflow.NodeDefinition, wfType workflow.WorkflowType, label string) {
	if def.Type.IsTrigger() || def.Type.IsEnd() {
		return
	}
	huge := strings.Repeat("y", tracepayload.MaxBytes+1024)
	release := make(chan struct{})
	defer close(release)
	behaviours := []struct {
		name string
		kind workflow.ErrorKind
		run  func(*workflow.NodeContext) (*workflow.NodeResult, error)
	}{
		{"executor error", workflow.ErrorKindTransport, func(*workflow.NodeContext) (*workflow.NodeResult, error) {
			return nil, workflow.Fail(workflow.ErrorKindTransport, errors.New("falhou de propósito"))
		}},
		{"executor panic", workflow.ErrorKindPanic, func(*workflow.NodeContext) (*workflow.NodeResult, error) {
			panic("pânico de propósito")
		}},
		{"executor timeout", workflow.ErrorKindTimeout, func(*workflow.NodeContext) (*workflow.NodeResult, error) {
			<-release
			return &workflow.NodeResult{}, nil
		}},
		{"oversized output", "", func(*workflow.NodeContext) (*workflow.NodeResult, error) {
			return &workflow.NodeResult{Output: map[string]interface{}{"body": huge}}, nil
		}},
	}
	for _, b := range behaviours {
		name := label + " " + b.name
		stub := funcExecutor{def: def, run: b.run}
		executors := func(box SimSandbox) *NodeExecutorRegistry {
			box.Outbound = c.outbound
			registry := newSimulationRegistry(c.deps, box)
			registry.Register(def.Type, stub)
			return registry
		}
		timeout := conformanceCaseTimeout
		if b.kind == workflow.ErrorKindTimeout {
			timeout = 30 * time.Millisecond
		}
		out, err := c.execute(conformanceRun{name: name, wf: conformanceWorkflow(def, wfType, cloneConfig(def.DefaultConfig)), input: TestNodeInput{ConfirmRealCalls: true}, executors: executors, timeout: timeout})
		c.structured(name, out, err)
		if b.kind != "" && out.ErrorKind != b.kind {
			c.t.Errorf("%s: kind = %q, want %q", name, out.ErrorKind, b.kind)
		}
		if b.kind == "" && (out.Step == nil || out.Step.Output[tracepayload.TruncatedKey] != true) {
			c.t.Errorf("%s: the output must be capped: %+v", name, out.Step)
		}
	}
}

func (c *conformance) conformAccess(def workflow.NodeDefinition, wfType workflow.WorkflowType, label string) {
	wf := conformanceWorkflow(def, wfType, cloneConfig(def.DefaultConfig))
	if _, err := c.execute(conformanceRun{name: label + " other workspace", wf: wf, input: TestNodeInput{WorkspaceID: "ws-other"}}); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		c.t.Errorf("%s: another workspace must read as not found, got %v", label, err)
	}
	if _, err := c.execute(conformanceRun{name: label + " unknown node", wf: wf, input: TestNodeInput{NodeID: "nao-existe"}}); !errors.Is(err, workflow.ErrNodeNotFound) {
		c.t.Errorf("%s: an unknown node must read as not found, got %v", label, err)
	}
}

func agentCallWorkflow(withTrigger bool) *workflow.Workflow {
	trigger := workflow.Node{ID: "t", Type: workflow.NodeTypeTriggerAgentCall, Config: map[string]interface{}{
		workflow.AgentCallInputsKey: []interface{}{map[string]interface{}{"name": "cpf", "type": "string", "required": true}},
	}}
	if !withTrigger {
		trigger = workflow.Node{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}}
	}
	return &workflow.Workflow{ID: "wf-agent", WorkspaceID: "ws1", Type: workflow.WorkflowTypeMessages, Graph: workflow.Graph{
		Nodes: []workflow.Node{trigger, {ID: "reply", Type: workflow.NodeTypeActionReplyToAgent, Config: map[string]interface{}{
			workflow.ReplyToAgentFieldsKey: []interface{}{map[string]interface{}{"key": "status", "value": "ok"}, map[string]interface{}{"key": "cpf", "value": "{{input.cpf}}"}},
		}}, {ID: "end", Type: workflow.NodeTypeEnd}},
		Edges: []workflow.Edge{{Source: "t", Target: "reply"}, {Source: "reply", Target: "end"}},
	}}
}

func TestTheAgentCallTriggerValidatesTheInputsTheAgentWouldSend(t *testing.T) {
	c := newConformance(t)
	cases := []struct {
		name    string
		vars    map[string]interface{}
		success bool
	}{
		{"valid inputs", map[string]interface{}{"input": map[string]interface{}{"cpf": "52998224725"}, "agent": map[string]interface{}{"id": "a1", "name": "Bia"}}, true},
		{"missing required input", map[string]interface{}{"input": map[string]interface{}{}}, false},
		{"wrong input type", map[string]interface{}{"input": map[string]interface{}{"cpf": float64(529)}}, false},
	}
	for _, tc := range cases {
		out, err := c.execute(conformanceRun{name: tc.name, wf: agentCallWorkflow(true), input: TestNodeInput{NodeID: "t", TriggerVars: tc.vars}})
		c.structured(tc.name, out, err)
		if out.Success != tc.success {
			t.Fatalf("%s: out = %+v", tc.name, out)
		}
		if !tc.success && out.ErrorKind != workflow.ErrorKindValidation {
			t.Fatalf("%s: kind = %q", tc.name, out.ErrorKind)
		}
	}
	out, err := c.execute(conformanceRun{name: "reply after an invalid agent call", wf: agentCallWorkflow(true), input: TestNodeInput{NodeID: "reply", TriggerVars: map[string]interface{}{"input": map[string]interface{}{}}}})
	c.structured("reply after an invalid agent call", out, err)
	if out.Success {
		t.Fatalf("execute_until must stop at the invalid trigger: %+v", out)
	}
}

func TestReplyToAgentWithoutTheAgentTriggerIsRefusedAsConfig(t *testing.T) {
	c := newConformance(t)
	out, err := c.execute(conformanceRun{name: "reply without trigger", wf: agentCallWorkflow(false), input: TestNodeInput{NodeID: "reply"}})
	c.structured("reply without trigger", out, err)
	if out.Success || out.ErrorKind != workflow.ErrorKindConfig {
		t.Fatalf("out = %+v", out)
	}
	out, err = c.execute(conformanceRun{name: "reply with trigger", wf: agentCallWorkflow(true), input: TestNodeInput{NodeID: "reply", TriggerVars: map[string]interface{}{"input": map[string]interface{}{"cpf": "1"}}}})
	c.structured("reply with trigger", out, err)
	if !out.Success || out.ExecutionOutput["status"] != "ok" {
		t.Fatalf("out = %+v", out)
	}
}

func TestTheAINodeRunsOnTheInjectedModel(t *testing.T) {
	c := newConformance(t)
	wf := &workflow.Workflow{ID: "wf-ai", WorkspaceID: "ws1", Type: workflow.WorkflowTypeMessages, Graph: workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}},
			{ID: "n", Type: workflow.NodeTypeActionAIAgent, Config: map[string]interface{}{"source": "prompt", "instructions": "Você ajuda com CEP.", "model": "stub/model"}},
			{ID: "end", Type: workflow.NodeTypeEnd},
		},
		Edges: []workflow.Edge{{Source: "t", Target: "n"}, {Source: "n", Target: "end"}},
	}}
	out, err := c.execute(conformanceRun{name: "ai with stub model", wf: wf, input: TestNodeInput{TriggerVars: map[string]interface{}{"message": "oi"}}})
	c.structured("ai with stub model", out, err)
	c.realExecutor("ai with stub model", out)
	if !out.Success {
		t.Fatalf("the AI node must run on the stub model: %+v", out)
	}
}

func TestAnHTTPNodeResolvesAKnownSecretMaskedAndRefusesAnUnknownOneBeforeSending(t *testing.T) {
	c := newConformance(t)
	http := workflow.NodeDefinition{Type: workflow.NodeTypeActionHTTPRequest}
	config := func(token string) map[string]interface{} {
		return map[string]interface{}{"method": "GET", "url": "https://api.example.com/x", "headers": map[string]interface{}{"Authorization": "Bearer " + token}}
	}

	before := c.snapshot()
	out, err := c.execute(conformanceRun{name: "known secret", wf: conformanceWorkflow(http, workflow.WorkflowTypeMessages, config("{{secret.token}}")), input: TestNodeInput{ConfirmRealCalls: true}})
	c.structured("known secret", out, err)
	if h, _, _ := c.spies.counts(); h != before[0]+1 {
		t.Fatalf("the confirmed call must reach the (failing) transport once, calls %d -> %d", before[0], h)
	}
	encoded, _ := json.Marshal(out)
	if strings.Contains(string(encoded), conformanceSecret) {
		t.Fatal("the secret value leaked into the response")
	}

	before = c.snapshot()
	out, err = c.execute(conformanceRun{name: "unknown secret", wf: conformanceWorkflow(http, workflow.WorkflowTypeMessages, config("{{secret.nao_existe}}")), input: TestNodeInput{ConfirmRealCalls: true}})
	c.structured("unknown secret", out, err)
	c.noRealCalls("unknown secret", before)
	if out.Success || !strings.Contains(out.Error, "nao_existe") {
		t.Fatalf("an unknown secret must be refused by name: %+v", out)
	}
}
