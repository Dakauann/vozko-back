package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type TestMode string

const (
	TestModeDirect TestMode = "direct"

	TestModeMock TestMode = "mock"

	TestModeExecuteUntil TestMode = "execute_until"
)

type AnalyzeNodeInput struct {
	WorkflowID  string `json:"workflow_id"`
	WorkspaceID string `json:"workspace_id"`
	NodeID      string `json:"node_id"`
}

type AnalyzeNodeOutput struct {
	NodeID        string                        `json:"node_id"`
	NodeType      workflow.NodeType             `json:"node_type"`
	NodeLabel     string                        `json:"node_label"`
	Dependencies  []workflow.VariableDependency `json:"dependencies"`
	RequiredMocks []workflow.RequiredMock       `json:"required_mocks"`
	TestMode      TestMode                      `json:"test_mode"`
	HasAIDeps     bool                          `json:"has_ai_deps"`
	CanRunDirect  bool                          `json:"can_run_direct"`
}

type TestNodeInput struct {
	WorkflowID       string                    `json:"workflow_id"`
	WorkspaceID      string                    `json:"workspace_id"`
	NodeID           string                    `json:"node_id"`
	MockedState      map[string]interface{}    `json:"mocked_state"`
	TriggerVars      map[string]interface{}    `json:"trigger_vars"`
	SkipExecution    bool                      `json:"skip_execution"`
	ConfirmRealCalls bool                      `json:"confirm_real_calls"`
	ContinueWaits    bool                      `json:"continue_waits"`
	WaitAnswers      map[string]TestWaitAnswer `json:"wait_answers"`
}

type TestNodeOutput struct {
	NodeID              string                     `json:"node_id"`
	NodeType            workflow.NodeType          `json:"node_type"`
	Success             bool                       `json:"success"`
	Error               string                     `json:"error,omitempty"`
	ErrorKind           workflow.ErrorKind         `json:"error_kind,omitempty"`
	InterpolatedConfig  map[string]interface{}     `json:"interpolated_config"`
	ExecutionOutput     map[string]interface{}     `json:"execution_output,omitempty"`
	ExecutionDurationMs int64                      `json:"execution_duration_ms"`
	StateAfter          map[string]interface{}     `json:"state_after,omitempty"`
	Pinned              bool                       `json:"pinned,omitempty"`
	Step                *workflow.WorkflowRunLog   `json:"step,omitempty"`
	UpstreamSteps       []*workflow.WorkflowRunLog `json:"upstream_steps,omitempty"`
	Wait                *workflow.WaitPreview      `json:"wait,omitempty"`
	PathStop            *NodeTestPathStop          `json:"path_stop,omitempty"`
}

type TestNodeUseCase interface {
	Analyze(ctx context.Context, input AnalyzeNodeInput) (*AnalyzeNodeOutput, error)

	Execute(ctx context.Context, input TestNodeInput) (*TestNodeOutput, error)
}

type TestExecutors func(box SimSandbox) *NodeExecutorRegistry

const DefaultNodeTestTimeout = 60 * time.Second

type TestNodeDeps struct {
	WorkflowRepo workflow.WorkflowRepository
	Steps        StepNamer
	ExecutorDeps ExecutorDeps
	Executors    TestExecutors
	Timeout      time.Duration
	PinSources   PinSources
	ViewerOf     func(context.Context) (shared.Person, bool)
}

var ErrNodeTestUnavailable = errors.New("workflow: the node test has no workflow store to read from")

type testNodeUseCase struct {
	deps TestNodeDeps
}

func NewTestNodeUseCase(deps TestNodeDeps) TestNodeUseCase {
	return &testNodeUseCase{deps: deps}
}

func (uc *testNodeUseCase) loadNode(workflowID, workspaceID, nodeID string) (*workflow.Workflow, *workflow.Node, error) {
	if uc.deps.WorkflowRepo == nil {
		return nil, nil, ErrNodeTestUnavailable
	}
	wf, err := uc.deps.WorkflowRepo.FindByID(workflowID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load workflow: %w", err)
	}
	if wf == nil || workspaceID == "" || wf.WorkspaceID != workspaceID {
		return nil, nil, workflow.ErrWorkflowNotFound
	}
	node := wf.Graph.FindNode(nodeID)
	if node == nil {
		return nil, nil, fmt.Errorf("%w: %s", workflow.ErrNodeNotFound, nodeID)
	}
	return wf, node, nil
}

func (uc *testNodeUseCase) forViewer(ctx context.Context, wf *workflow.Workflow) *workflow.Workflow {
	if uc.deps.ViewerOf == nil {
		return withoutProductionPins(wf)
	}
	viewer, ok := uc.deps.ViewerOf(ctx)
	if !ok {
		return withoutProductionPins(wf)
	}
	return withheldFor(uc.deps.PinSources, viewer, wf)
}

func (uc *testNodeUseCase) stepName(t workflow.NodeType) string {
	if uc.deps.Steps == nil {
		return string(t)
	}
	return uc.deps.Steps.StepName(t)
}

func (uc *testNodeUseCase) Analyze(ctx context.Context, input AnalyzeNodeInput) (*AnalyzeNodeOutput, error) {
	wf, node, err := uc.loadNode(input.WorkflowID, input.WorkspaceID, input.NodeID)
	if err != nil {
		return nil, err
	}

	deps := workflow.ExtractDependencies(node.Config)

	testMode, _, canRunDirect := resolveTestMode(deps)

	allMocks := workflow.GetRequiredMocks(deps)
	requiredMocks := filterMocksForAIDeps(allMocks, &wf.Graph)
	hasAIDeps := workflow.HasAIDependencies(deps) || len(requiredMocks) > 0

	if len(requiredMocks) > 0 && canRunDirect {
		canRunDirect = false
		testMode = TestModeExecuteUntil
		hasAIDeps = true
	}

	if node.Type == workflow.NodeTypeActionAIAgent {
		source, _ := node.Config["source"].(string)
		agentID, _ := node.Config["agent_id"].(string)
		if (source == "" || source == "agent") && agentID != "" && uc.deps.ExecutorDeps.AgentRepo != nil {
			agentRecord, agentErr := uc.deps.ExecutorDeps.AgentRepo.FindByID(agentID)
			if agentErr == nil && agentRecord != nil {
				varInfos := agentRecord.RequiredVariableInfos()
				for _, vi := range varInfos {
					mockKey := "campvars." + vi.Name
					displayName := vi.Name
					if vi.Description != "" {
						displayName = vi.Description
					}
					requiredMocks = append(requiredMocks, workflow.RequiredMock{
						StateKey:    mockKey,
						DisplayName: displayName,
						Source:      workflow.DependencySourceAgentVar,
					})
					if vi.Required {
						hasAIDeps = true
					}
				}
			}
		}
	}

	if node.Type == workflow.NodeTypeWaitDTMF {
		requiredMocks = append(requiredMocks, workflow.RequiredMock{
			StateKey:    testKeyMock,
			DisplayName: "Tecla pressionada (vazio simula silêncio)",
			Source:      workflow.DependencySourceCaller,
			Optional:    true,
		})
	}

	if node.Type == workflow.NodeTypeTransferToQueue {
		requiredMocks = append(requiredMocks, workflow.RequiredMock{
			StateKey:    testTransferMock,
			DisplayName: "Resultado da fila ('timeout' simula ninguém atendendo; vazio, atendido)",
			Source:      workflow.DependencySourceCaller,
			Optional:    true,
		})
	}

	nodeLabel := uc.stepName(node.Type)

	return &AnalyzeNodeOutput{
		NodeID:        node.ID,
		NodeType:      node.Type,
		NodeLabel:     nodeLabel,
		Dependencies:  deps,
		RequiredMocks: requiredMocks,
		TestMode:      testMode,
		HasAIDeps:     hasAIDeps,
		CanRunDirect:  canRunDirect,
	}, nil
}

func (uc *testNodeUseCase) Execute(ctx context.Context, input TestNodeInput) (*TestNodeOutput, error) {
	wf, node, err := uc.loadNode(input.WorkflowID, input.WorkspaceID, input.NodeID)
	if err != nil {
		return nil, err
	}
	wf = uc.forViewer(ctx, wf)
	if node = wf.Graph.FindNode(node.ID); node == nil {
		return nil, fmt.Errorf("%w: %s", workflow.ErrNodeNotFound, input.NodeID)
	}
	started := time.Now()
	t, err := uc.newNodeTest(wf, node, input)
	if err != nil {
		return nil, err
	}
	limit := uc.timeout()
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	finished := make(chan *TestNodeOutput, 1)
	go func() {
		defer close(t.done)
		defer func() {
			if r := recover(); r != nil {
				slog.Error("workflow.node_test.panic_recovered", append(runLogAttrs(t.run, node.ID), slog.Any("panic", r))...)
				finished <- t.crashed(r)
			}
		}()
		finished <- t.execute(ctx)
	}()

	var out *TestNodeOutput
	select {
	case out = <-finished:
	case <-ctx.Done():
		out = t.timedOut(ctx, limit)
	}
	out.ExecutionDurationMs = time.Since(started).Milliseconds()
	return redactedTestOutput(out), nil
}

func (t *nodeTest) execute(ctx context.Context) *TestNodeOutput {
	wf, node, input := t.wf, t.target, t.input
	deps := workflow.ExtractDependencies(node.Config)
	testMode, _, _ := resolveTestMode(deps)
	requiredMocks := filterMocksForAIDeps(workflow.GetRequiredMocks(deps), &wf.Graph)
	if len(requiredMocks) > 0 && testMode == TestModeDirect {
		testMode = TestModeExecuteUntil
	}
	missingMocks := missingRequiredMocks(requiredMocks, input.MockedState)
	if shouldBlockForMissingMocks(testMode, input.MockedState, missingMocks) {
		return buildMissingMocksOutput(node, input.TriggerVars, input.MockedState, missingMocks)
	}

	state := &t.run.State
	applyMockedState(state, input.MockedState)
	t.satisfyCaptureDeps(mockRootKeys(input.MockedState))

	needsUpstreamExecution := testMode == TestModeExecuteUntil && (len(input.MockedState) == 0 || len(missingMocks) > 0)
	if needsUpstreamExecution {
		if stop := t.executeUntilTarget(ctx); stop != nil {
			applyMockedState(state, input.MockedState)
			message := stop.Message
			if len(missingMocks) > 0 {
				message = fmt.Sprintf("%s Forneca valores simulados para continuar o teste: %s", message, strings.Join(missingMocks, ", "))
			}
			upstream, _ := t.snapshot()
			return &TestNodeOutput{
				NodeID:             node.ID,
				NodeType:           node.Type,
				InterpolatedConfig: workflow.InterpolateMap(node.Config, state, nil),
				Error:              message,
				ErrorKind:          workflow.ErrorKindTargetNotReached,
				UpstreamSteps:      upstream,
				PathStop:           stop,
			}
		}
	}

	applyMockedState(state, input.MockedState)
	upstream, _ := t.snapshot()
	out := &TestNodeOutput{
		NodeID:             node.ID,
		NodeType:           node.Type,
		InterpolatedConfig: workflow.InterpolateMap(node.Config, state, nil),
		UpstreamSteps:      upstream,
	}
	if input.SkipExecution {
		out.Success = true
		return out
	}
	t.settle(out, t.runStep(node))
	return out
}

func buildTestRun(wf *workflow.Workflow, workspaceID string) *workflow.WorkflowRun {
	return &workflow.WorkflowRun{
		ID:          uuid.NewString(),
		WorkflowID:  wf.ID,
		WorkspaceID: workspaceID,
		Status:      workflow.RunStatusRunning,
		StartedAt:   time.Now().UTC(),
		EntryID:     uuid.NewString(),
		EntryType:   "test",
		State:       workflow.NewRunState(),
	}
}

func resolveTestMode(deps []workflow.VariableDependency) (TestMode, bool, bool) {
	hasAIDeps := workflow.HasAIDependencies(deps)
	requiresUpstream := workflow.RequiresUpstreamData(deps)
	canRunDirect := !requiresUpstream

	if canRunDirect {
		return TestModeDirect, hasAIDeps, canRunDirect
	}

	return TestModeExecuteUntil, hasAIDeps, canRunDirect
}

func isAINodeType(t workflow.NodeType) bool {
	return t.IsAI() || t == workflow.NodeTypeConditionAIClassfy
}

func getAICaptureVarNames(g *workflow.Graph) map[string]bool {
	result := make(map[string]bool)
	for _, n := range g.Nodes {
		if !isAINodeType(n.Type) {
			continue
		}
		if rv, ok := n.Config["response_variable"].(string); ok && rv != "" {
			result[rv] = true
		}
		if cv, ok := n.Config["capture_variable"].(string); ok && cv != "" {
			result[cv] = true
		}
	}
	return result
}

func filterMocksForAIDeps(mocks []workflow.RequiredMock, g *workflow.Graph) []workflow.RequiredMock {
	aiCaptures := getAICaptureVarNames(g)
	var filtered []workflow.RequiredMock
	for _, m := range mocks {
		switch m.Source {
		case workflow.DependencySourceAI:
			filtered = append(filtered, m)
		case workflow.DependencySourceSpecificNode:
			if sourceNode := g.FindNode(m.SourceNode); sourceNode != nil && isAINodeType(sourceNode.Type) {
				filtered = append(filtered, m)
			}
		case workflow.DependencySourceCustom:
			scope := m.DisplayName
			if idx := strings.IndexByte(scope, '.'); idx >= 0 {
				scope = scope[:idx]
			}
			if aiCaptures[scope] {
				filtered = append(filtered, m)
			}
		case workflow.DependencySourceTrigger:

			if aiCaptures[m.StateKey] {
				filtered = append(filtered, m)
			}
		}
	}
	return filtered
}

func mockRootKeys(mockedState map[string]interface{}) map[string]bool {
	roots := make(map[string]bool, len(mockedState))
	for k := range mockedState {
		root := k
		if idx := strings.IndexByte(k, '.'); idx >= 0 {
			root = k[:idx]
		}
		roots[root] = true
	}
	return roots
}

func nodeOutputMocked(node *workflow.Node, mockRoots map[string]bool) bool {
	if node == nil || len(mockRoots) == 0 {
		return false
	}
	if rv, ok := node.Config["response_variable"].(string); ok && rv != "" && mockRoots[rv] {
		return true
	}
	if cv, ok := node.Config["capture_variable"].(string); ok && cv != "" && mockRoots[cv] {
		return true
	}
	return false
}

func findCaptureProducer(g *workflow.Graph, varName string) *workflow.Node {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if cv, ok := n.Config["capture_variable"].(string); ok && cv == varName {
			return n
		}
		if rv, ok := n.Config["response_variable"].(string); ok && rv == varName {
			return n
		}
	}
	return nil
}

func applyMockedState(state *workflow.RunState, mockedState map[string]interface{}) {
	if state == nil {
		return
	}
	for key, value := range mockedState {
		applyMockedStateValue(state, key, value)
	}
}

func applyMockedStateValue(state *workflow.RunState, key string, value interface{}) {
	if state == nil || key == "" {
		return
	}

	rootKey, nestedPath := splitMockStateKey(key)
	if nestedPath == "" {
		state.Set(rootKey, value)
		return
	}

	root, _ := state.Get(rootKey)
	rootMap, ok := root.(map[string]interface{})
	if !ok || rootMap == nil {
		rootMap = make(map[string]interface{})
	}
	setNestedMockValue(rootMap, nestedPath, value)
	state.Set(rootKey, rootMap)
}

func splitMockStateKey(key string) (string, string) {
	dotIdx := strings.IndexByte(key, '.')
	if dotIdx < 0 {
		return key, ""
	}
	return key[:dotIdx], key[dotIdx+1:]
}

func setNestedMockValue(root map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	current := root
	for idx, part := range parts {
		if idx == len(parts)-1 {
			current[part] = value
			return
		}
		next, ok := current[part].(map[string]interface{})
		if !ok || next == nil {
			next = make(map[string]interface{})
			current[part] = next
		}
		current = next
	}
}

func shouldBlockForMissingMocks(testMode TestMode, mockedState map[string]interface{}, missing []string) bool {
	if len(missing) == 0 {
		return false
	}

	switch testMode {
	case TestModeMock:
		return true
	case TestModeExecuteUntil:
		return len(mockedState) > 0
	default:
		return false
	}
}

func buildMissingMocksOutput(node *workflow.Node, triggerVars, mockedState map[string]interface{}, missing []string) *TestNodeOutput {
	state := workflow.NewRunState()
	seedTriggerVars(&state, triggerVars)
	applyMockedState(&state, mockedState)

	return &TestNodeOutput{
		NodeID:             node.ID,
		NodeType:           node.Type,
		Error:              fmt.Sprintf("Forneca valores simulados para continuar o teste: %s", strings.Join(missing, ", ")),
		ErrorKind:          workflow.ErrorKindMissingMocks,
		InterpolatedConfig: workflow.InterpolateMap(node.Config, &state, nil),
	}
}

func missingRequiredMocks(required []workflow.RequiredMock, mockedState map[string]interface{}) []string {
	if len(required) == 0 {
		return nil
	}
	missing := make([]string, 0, len(required))
	for _, mock := range required {
		value, ok := mockedState[mock.StateKey]
		if !ok {
			missing = append(missing, mock.DisplayName)
			continue
		}
		if text, isString := value.(string); isString && strings.TrimSpace(text) == "" {
			missing = append(missing, mock.DisplayName)
		}
	}
	return missing
}

func copyState(state workflow.RunState) map[string]interface{} {
	stateAfter := make(map[string]interface{}, len(state.Vars))
	for k, v := range state.Vars {
		stateAfter[k] = v
	}
	return stateAfter
}

func seedTriggerVars(state *workflow.RunState, triggerVars map[string]interface{}) {
	if state == nil {
		return
	}
	for k, v := range triggerVars {
		state.Set(k, v)
	}
}

func seedSimulationConversation(repo *simMessageRepo, run *workflow.WorkflowRun, triggerVars map[string]interface{}) {
	if repo == nil || run == nil {
		return
	}
	message, _ := triggerVars["message"].(string)
	if message == "" {
		return
	}
	_ = repo.Create(&conversation.Message{
		ID:          uuid.NewString(),
		EntryID:     run.EntryID,
		EntryType:   shared.EntryType(run.EntryType),
		MessageType: conversation.MessageTypeUserMessage,
		From:        "5511999990000",
		To:          "sim-business-phone",
		Text:        message,
		CreatedAt:   time.Now().UTC(),
	})
}

func pickAncestorEdge(edges []workflow.Edge, ancestors map[string]bool, targetNodeID string) string {
	for _, e := range edges {
		if e.Target == targetNodeID || ancestors[e.Target] {
			return e.Target
		}
	}
	return ""
}

func findAncestorRoot(ancestors map[string]bool, g *workflow.Graph) string {
	for nodeID := range ancestors {
		isRoot := true
		for _, e := range g.IncomingEdges(nodeID) {
			if ancestors[e.Source] {
				isRoot = false
				break
			}
		}
		if isRoot {
			return nodeID
		}
	}
	return ""
}

type MockFieldSpec struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Source      string `json:"source"`
	SourceNode  string `json:"source_node,omitempty"`
	Hint        string `json:"hint,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
}

type UIAnalysis struct {
	NodeID       string          `json:"node_id"`
	NodeType     string          `json:"node_type"`
	NodeLabel    string          `json:"node_label"`
	TestMode     TestMode        `json:"test_mode"`
	MockFields   []MockFieldSpec `json:"mock_fields"`
	CanRunDirect bool            `json:"can_run_direct"`
	HasAIDeps    bool            `json:"has_ai_deps"`
	Message      string          `json:"message"`
}

func (a *AnalyzeNodeOutput) ToUIAnalysis() UIAnalysis {
	mockFields := make([]MockFieldSpec, 0, len(a.RequiredMocks))
	for _, m := range a.RequiredMocks {
		mockFields = append(mockFields, MockFieldSpec{
			Key:         m.StateKey,
			DisplayName: m.DisplayName,
			Source:      string(m.Source),
			SourceNode:  m.SourceNode,
			Optional:    m.Optional,
		})
	}

	var message string
	switch a.TestMode {
	case TestModeDirect:
		message = "Este nó não tem dependências de nós anteriores. Pode ser testado diretamente."
	case TestModeMock:
		message = "Este nó depende de dados de nós anteriores. Preencha os valores simulados abaixo."
	case TestModeExecuteUntil:
		if a.HasAIDeps {
			message = "Este nó depende de saídas de IA. Preencha os valores simulados para testar sem executar a IA."
		} else {
			message = "Este nó depende de nós anteriores. Os nós upstream serão executados automaticamente."
		}
	}

	return UIAnalysis{
		NodeID:       a.NodeID,
		NodeType:     string(a.NodeType),
		NodeLabel:    a.NodeLabel,
		TestMode:     a.TestMode,
		MockFields:   mockFields,
		CanRunDirect: a.CanRunDirect,
		HasAIDeps:    a.HasAIDeps,
		Message:      message,
	}
}
