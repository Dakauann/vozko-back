package workflow_usecase

import (
	"context"
	"encoding/json"
	"fmt"
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
	WorkflowID    string                 `json:"workflow_id"`
	WorkspaceID   string                 `json:"workspace_id"`
	NodeID        string                 `json:"node_id"`
	MockedState   map[string]interface{} `json:"mocked_state"`
	TriggerVars   map[string]interface{} `json:"trigger_vars"`
	SkipExecution bool                   `json:"skip_execution"`
}

type TestNodeOutput struct {
	NodeID             string                 `json:"node_id"`
	NodeType           workflow.NodeType      `json:"node_type"`
	Success            bool                   `json:"success"`
	Error              string                 `json:"error,omitempty"`
	InterpolatedConfig map[string]interface{} `json:"interpolated_config"`
	ExecutionOutput    map[string]interface{} `json:"execution_output,omitempty"`
	ExecutionDuration  time.Duration          `json:"execution_duration_ms"`
	StateAfter         map[string]interface{} `json:"state_after,omitempty"`
}

type preparedTestExecution struct {
	run      *workflow.WorkflowRun
	registry *NodeExecutorRegistry
}

type TestNodeUseCase interface {
	Analyze(ctx context.Context, input AnalyzeNodeInput) (*AnalyzeNodeOutput, error)

	Execute(ctx context.Context, input TestNodeInput) (*TestNodeOutput, error)
}

type TestNodeDeps struct {
	WorkflowRepo workflow.WorkflowRepository
	Registry     *NodeExecutorRegistry
	ExecutorDeps ExecutorDeps
}

type testNodeUseCase struct {
	deps TestNodeDeps
}

func NewTestNodeUseCase(deps TestNodeDeps) TestNodeUseCase {
	return &testNodeUseCase{deps: deps}
}

func (uc *testNodeUseCase) Analyze(ctx context.Context, input AnalyzeNodeInput) (*AnalyzeNodeOutput, error) {

	wf, err := uc.deps.WorkflowRepo.FindByID(input.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow: %w", err)
	}
	if wf == nil {
		return nil, workflow.ErrWorkflowNotFound
	}

	if wf.WorkspaceID != input.WorkspaceID {
		return nil, fmt.Errorf("workspace mismatch")
	}

	node := wf.Graph.FindNode(input.NodeID)
	if node == nil {
		return nil, fmt.Errorf("node not found: %s", input.NodeID)
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

	nodeLabel := string(node.Type)
	if def, ok := uc.deps.Registry.definitions[node.Type]; ok {
		nodeLabel = def.Label
	}

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
	startTime := time.Now()

	wf, err := uc.deps.WorkflowRepo.FindByID(input.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow: %w", err)
	}
	if wf == nil {
		return nil, workflow.ErrWorkflowNotFound
	}

	if wf.WorkspaceID != input.WorkspaceID {
		return nil, fmt.Errorf("workspace mismatch")
	}

	node := wf.Graph.FindNode(input.NodeID)
	if node == nil {
		return nil, fmt.Errorf("node not found: %s", input.NodeID)
	}

	deps := workflow.ExtractDependencies(node.Config)
	testMode, _, _ := resolveTestMode(deps)
	allMocks := workflow.GetRequiredMocks(deps)
	requiredMocks := filterMocksForAIDeps(allMocks, &wf.Graph)

	if len(requiredMocks) > 0 && testMode == TestModeDirect {
		testMode = TestModeExecuteUntil
	}

	missingMocks := missingRequiredMocks(requiredMocks, input.MockedState)

	if shouldBlockForMissingMocks(testMode, input.MockedState, missingMocks) {
		return buildMissingMocksOutput(node, input.TriggerVars, input.MockedState, missingMocks, startTime), nil
	}

	prepared, prepErr := uc.prepareExecution(wf, input.WorkspaceID, input.TriggerVars)
	if prepErr != nil {
		return nil, prepErr
	}

	state := prepared.run.State
	registry := prepared.registry

	// Seed provided mocks up front so dependency satisfaction can recognise an
	// already-mocked output (e.g. an AI response) and skip re-executing it.
	applyMockedState(&state, input.MockedState)

	// Satisfy NON-AI capture-variable dependencies, e.g. an HTTP node that
	// captures an auth token into token_consulta_cadastro, by executing their
	// producer node directly. Those are not "required mocks", so the mode gate
	// below never runs them; without this, a reference like {{token[0].token}}
	// resolves to nothing and the node fails (an empty Bearer header -> 401).
	// Targets the specific producer instead of walking the whole graph, so it
	// works even when the graph is strongly connected through an AI-agent hub.
	uc.satisfyCaptureDeps(ctx, prepared.run, wf, node, registry, mockRootKeys(input.MockedState))

	hasUserProvidedMocks := len(input.MockedState) > 0
	needsUpstreamExecution := testMode == TestModeExecuteUntil &&
		(!hasUserProvidedMocks || len(missingMocks) > 0)

	if needsUpstreamExecution {
		if err := uc.executeUntilTarget(ctx, prepared.run, wf, node.ID, registry); err != nil {
			applyMockedState(&state, input.MockedState)
			errorMessage := err.Error()
			if len(missingMocks) > 0 {
				errorMessage = fmt.Sprintf(
					"%s. Forneca valores simulados para continuar o teste: %s",
					err.Error(),
					strings.Join(missingMocks, ", "),
				)
			}
			return &TestNodeOutput{
				NodeID:             node.ID,
				NodeType:           node.Type,
				InterpolatedConfig: workflow.InterpolateMap(node.Config, &state, nil),
				Error:              errorMessage,
				ExecutionDuration:  time.Since(startTime),
			}, nil
		}
	}

	applyMockedState(&state, input.MockedState)

	interpolatedConfig := workflow.InterpolateMap(node.Config, &state, nil)

	output := &TestNodeOutput{
		NodeID:             node.ID,
		NodeType:           node.Type,
		InterpolatedConfig: interpolatedConfig,
	}

	if input.SkipExecution {
		output.Success = true
		output.ExecutionDuration = time.Since(startTime)
		return output, nil
	}

	executor, ok := registry.Get(node.Type)
	if !ok {
		output.Error = fmt.Sprintf("no executor for node type: %s", node.Type)
		output.ExecutionDuration = time.Since(startTime)
		return output, nil
	}

	nodeCtx := &workflow.NodeContext{
		Run:      prepared.run,
		Node:     node,
		State:    &state,
		Graph:    &wf.Graph,
		Workflow: wf,
		Runtime:  nil,
	}

	nodeCtx.Node = &workflow.Node{
		ID:     node.ID,
		Type:   node.Type,
		Config: interpolatedConfig,
	}

	result, execErr := executor.Execute(nodeCtx)

	output.ExecutionDuration = time.Since(startTime)

	if execErr != nil {
		output.Error = execErr.Error()
		return output, nil
	}
	if result == nil {
		result = &workflow.NodeResult{}
	}
	if result.Error != "" {
		output.Error = result.Error
		return output, nil
	}

	output.Success = true

	if result != nil && result.Output != nil {
		output.ExecutionOutput = result.Output
		mergeNodeOutputIntoState(&state, node.ID, result.Output)
	}

	if state.Vars != nil {
		output.StateAfter = copyState(state)
	}

	return output, nil
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

func buildTestExecutorDeps(deps ExecutorDeps) interface{} {

	return deps
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

// mockRootKeys reduces the provided mock keys to their root variable names
// (e.g. "ana_response.tool_args.cpf" -> "ana_response"), the granularity at which
// a node's output is considered "already supplied by a mock".
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

// nodeOutputMocked reports whether a node's captured output is already provided
// by a mock, used to avoid re-executing it (notably AI agents, which are
// expensive/nondeterministic and are meant to be mocked in a node test).
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

// findCaptureProducer returns the node whose capture_variable/response_variable
// equals varName, i.e. the node that produces that variable when executed.
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

// satisfyCaptureDeps walks a node's variable dependencies and, for every NON-AI
// capture variable that is neither mocked nor already present in state, executes
// the node that produces it (resolving that producer's own deps first). This is
// how a node like s2_2 gets its upstream token: run s2_1 to populate
// token_consulta_cadastro. AI-produced outputs are left to the mock layer.
func (uc *testNodeUseCase) satisfyCaptureDeps(ctx context.Context, run *workflow.WorkflowRun, wf *workflow.Workflow, target *workflow.Node, registry *NodeExecutorRegistry, mockRoots map[string]bool) {
	visited := map[string]bool{}
	var satisfy func(n *workflow.Node)
	satisfy = func(n *workflow.Node) {
		if n == nil || visited[n.ID] {
			return
		}
		visited[n.ID] = true
		for _, d := range workflow.ExtractDependencies(n.Config) {
			// Only default-scope refs name a capture variable directly (e.g.
			// token_consulta_cadastro). var/sys need no upstream; last/node/ai are
			// handled by mocks or the upstream walk.
			switch d.Scope {
			case "var", "sys", "last", "node", "ai":
				continue
			}
			// A direct-index ref ({{token[0].field}}) leaves the bracket on the
			// scope segment; trim it to the root variable name.
			varName := d.Scope
			if i := strings.IndexByte(varName, '['); i >= 0 {
				varName = varName[:i]
			}
			if varName == "" || mockRoots[varName] {
				continue // supplied by a provided mock
			}
			if _, ok := run.State.Get(varName); ok {
				continue // already in state
			}
			producer := findCaptureProducer(&wf.Graph, varName)
			if producer == nil || producer.ID == target.ID || nodeOutputMocked(producer, mockRoots) {
				continue
			}
			satisfy(producer) // resolve the producer's own dependencies first
			uc.executeProducer(ctx, run, wf, producer, registry)
		}
	}
	satisfy(target)
}

// executeProducer runs a single upstream node against the shared run state,
// letting its executor populate its capture variable and node/last outputs.
func (uc *testNodeUseCase) executeProducer(_ context.Context, run *workflow.WorkflowRun, wf *workflow.Workflow, node *workflow.Node, registry *NodeExecutorRegistry) {
	executor, ok := registry.Get(node.Type)
	if !ok {
		return
	}
	interpolated := workflow.InterpolateMap(node.Config, &run.State, nil)
	result, err := executor.Execute(&workflow.NodeContext{
		Run:      run,
		Node:     &workflow.Node{ID: node.ID, Type: node.Type, Config: interpolated},
		State:    &run.State,
		Graph:    &wf.Graph,
		Workflow: wf,
	})
	if err != nil || result == nil {
		return
	}
	if result.Output != nil {
		mergeNodeOutputIntoState(&run.State, node.ID, result.Output)
	}
}

func mergeNodeOutputIntoState(state *workflow.RunState, nodeID string, output map[string]interface{}) {
	if state == nil || output == nil {
		return
	}
	for k, v := range output {
		state.Set("_last_"+k, v)
		state.Set("_node_"+nodeID+"_"+k, v)
	}
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

func hasAllRequiredMocks(required []workflow.RequiredMock, mockedState map[string]interface{}) bool {
	return len(missingRequiredMocks(required, mockedState)) == 0
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

func buildMissingMocksOutput(node *workflow.Node, triggerVars, mockedState map[string]interface{}, missing []string, startedAt time.Time) *TestNodeOutput {
	state := workflow.NewRunState()
	seedTriggerVars(&state, triggerVars)
	applyMockedState(&state, mockedState)

	return &TestNodeOutput{
		NodeID:             node.ID,
		NodeType:           node.Type,
		Error:              fmt.Sprintf("Forneca valores simulados para continuar o teste: %s", strings.Join(missing, ", ")),
		InterpolatedConfig: workflow.InterpolateMap(node.Config, &state, nil),
		ExecutionDuration:  time.Since(startedAt),
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

func (uc *testNodeUseCase) prepareExecution(wf *workflow.Workflow, workspaceID string, triggerVars map[string]interface{}) (*preparedTestExecution, error) {
	run := buildTestRun(wf, workspaceID)
	registry := uc.deps.Registry

	if uc.shouldUseSimulationRegistry() {
		simLeadID := uuid.NewString()
		simEntryID := uuid.NewString()
		simMsgRepo := &simMessageRepo{}
		outboundCh := make(chan SimOutboundMessage, 256)

		registry = newTestSimulationRegistry(uc.deps.ExecutorDeps, workspaceID, simLeadID, simMsgRepo, outboundCh)
		run.EntryID = simEntryID
		run.EntryType = string(shared.EntryTypeWhatsApp)

		seedTriggerVars(&run.State, triggerVars)
		seedSimulationConversation(simMsgRepo, run, triggerVars)
	} else {
		seedTriggerVars(&run.State, triggerVars)
	}

	if registry == nil {
		return nil, fmt.Errorf("node executor registry is not configured")
	}

	return &preparedTestExecution{
		run:      run,
		registry: registry,
	}, nil
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

func newTestSimulationRegistry(deps ExecutorDeps, workspaceID, simLeadID string, simMsgRepo *simMessageRepo, outboundCh chan<- SimOutboundMessage) *NodeExecutorRegistry {
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{
		AIService:               deps.AIService,
		AgentRepo:               deps.AgentRepo,
		MessageRepo:             simMsgRepo,
		HistoryManager:          &simHistoryManager{},
		LeadRepo:                &simLeadRepo{},
		WhatsAppEntryRepo:       &simWhatsAppEntryRepo{leadID: simLeadID},
		BusinessPhoneRepo:       &simBusinessPhoneRepo{workspaceID: workspaceID},
		MessageWindowRepo:       &simMessageWindowRepo{},
		WhatsAppClientFactory:   newSimWhatsAppClientFactory(outboundCh),
		ToolRegistry:            deps.ToolRegistry,
		TemplateRepo:            deps.TemplateRepo,
		MediaRepo:               deps.MediaRepo,
		ConsumeWhatsappTemplate: &simBalanceConsumer{},
		SubWorkflowRunner:       &simSubWorkflowRunner{},
		SharedState:             deps.SharedState,
		CalendarRepo:            deps.CalendarRepo,
		GoogleCalendar:          deps.GoogleCalendar,
		BillingPub:              deps.BillingPub,
		LabelRepo:               deps.LabelRepo,
		DepartmentRepo:          deps.DepartmentRepo,
		InboxAssignmentRepo:     deps.InboxAssignmentRepo,
		WorkspaceRepo:           deps.WorkspaceRepo,
		CachedBalanceChecker:    deps.CachedBalanceChecker,
	})
	return registry
}

func (uc *testNodeUseCase) shouldUseSimulationRegistry() bool {
	deps := uc.deps.ExecutorDeps
	return deps.AIService != nil ||
		deps.AgentRepo != nil ||
		deps.MessageRepo != nil ||
		deps.HistoryManager != nil ||
		deps.LeadRepo != nil ||
		deps.WhatsAppEntryRepo != nil ||
		deps.BusinessPhoneRepo != nil ||
		deps.MessageWindowRepo != nil ||
		deps.WhatsAppClientFactory != nil ||
		deps.ToolRegistry != nil ||
		deps.TemplateRepo != nil ||
		deps.MediaRepo != nil ||
		deps.CalendarRepo != nil ||
		deps.GoogleCalendar != nil ||
		deps.LabelRepo != nil ||
		deps.DepartmentRepo != nil ||
		deps.InboxAssignmentRepo != nil ||
		deps.WorkspaceRepo != nil ||
		deps.CachedBalanceChecker != nil
}

func (uc *testNodeUseCase) executeUntilTarget(ctx context.Context, run *workflow.WorkflowRun, wf *workflow.Workflow, targetNodeID string, registry *NodeExecutorRegistry) error {

	ancestors := wf.Graph.AncestorsOf(targetNodeID)

	startNodeID := ""
	if trigger := wf.Graph.TriggerNode(); trigger != nil && ancestors[trigger.ID] {
		startNodeID = trigger.ID
	} else {
		startNodeID = findAncestorRoot(ancestors, &wf.Graph)
	}
	if startNodeID == "" {
		return fmt.Errorf("no upstream path found to node %s", targetNodeID)
	}

	run.CurrentNodeID = startNodeID

	executionCount := 0
	nodeVisitCounts := make(map[string]int)
	insideLoop := loopBodyNodes(&wf.Graph)
	const maxNodeRevisits = 3

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if run.CurrentNodeID == targetNodeID {
			return nil
		}

		if executionCount >= workflow.MaxExecutionsPerRun {
			return workflow.ErrMaxExecutionsReached
		}

		node := wf.Graph.FindNode(run.CurrentNodeID)
		if node == nil {
			return fmt.Errorf("node not found: %s", run.CurrentNodeID)
		}
		if node.Type.IsEnd() {
			return fmt.Errorf("workflow completed before reaching node %s", targetNodeID)
		}

		nodeVisitCounts[node.ID]++
		revisitLimit := maxNodeRevisits
		if node.Type == workflow.NodeTypeActionLoop || insideLoop[node.ID] {
			revisitLimit = workflow.MaxExecutionsPerRun
		}
		if nodeVisitCounts[node.ID] > revisitLimit {
			return fmt.Errorf("loop detected while executing until node %s", targetNodeID)
		}

		if node.Type.IsTrigger() {
			next := pickAncestorEdge(wf.Graph.OutgoingEdges(node.ID), ancestors, targetNodeID)
			if next == "" {
				return fmt.Errorf("trigger node has no outgoing edges toward target")
			}
			run.State.Set("_prev_node_id", node.ID)
			run.CurrentNodeID = next
			executionCount++
			continue
		}

		executor, ok := registry.Get(node.Type)
		if !ok {
			return fmt.Errorf("no executor for upstream node type: %s", node.Type)
		}

		result, err := executor.Execute(&workflow.NodeContext{
			Run:      run,
			Node:     node,
			Graph:    &wf.Graph,
			Workflow: wf,
			State:    &run.State,
		})
		executionCount++

		if err != nil {
			return fmt.Errorf("failed to execute upstream node %s: %w", node.ID, err)
		}
		if result == nil {
			result = &workflow.NodeResult{}
		}
		if result.Error != "" {
			return fmt.Errorf("upstream node %s failed: %s", node.ID, result.Error)
		}

		mergeNodeOutputIntoState(&run.State, node.ID, result.Output)

		if result.Wait != nil {
			return fmt.Errorf("workflow paused on node %s before reaching node %s", node.ID, targetNodeID)
		}
		if result.Complete {
			return fmt.Errorf("workflow completed before reaching node %s", targetNodeID)
		}

		nextNodeID := result.NextNodeID
		if nextNodeID != "" && nextNodeID != targetNodeID && !ancestors[nextNodeID] {

			nextNodeID = ""
		}
		if nextNodeID == "" {
			nextNodeID = pickAncestorEdge(wf.Graph.OutgoingEdges(node.ID), ancestors, targetNodeID)
		}
		if nextNodeID == "" {
			return fmt.Errorf("workflow ended before reaching node %s", targetNodeID)
		}

		run.State.Set("_prev_node_id", node.ID)
		run.CurrentNodeID = nextNodeID
	}
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

func (o TestNodeOutput) MarshalJSON() ([]byte, error) {
	type Alias TestNodeOutput
	return json.Marshal(&struct {
		Alias
		ExecutionDuration int64 `json:"execution_duration_ms"`
	}{
		Alias:             Alias(o),
		ExecutionDuration: o.ExecutionDuration.Milliseconds(),
	})
}
