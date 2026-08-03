package workflow_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/agent"
	domainmcp "vozko/domain/agent/mcp"
	label_domain "vozko/domain/label"
	media_domain "vozko/domain/media"
	rag_domain "vozko/domain/rag"
	businessphone_domain "vozko/domain/whatsapp/business_phone"
	template_domain "vozko/domain/whatsapp/template"
	"vozko/domain/workflow"
	workspace_domain "vozko/domain/workspace"
	dept_domain "vozko/domain/workspace/workspace_department"
)

type workspaceMemberLookup interface {
	GetMember(workspaceID, userID string) (*workspace_domain.Member, error)
}

type businessPhoneLookup interface {
	FindByID(id string) (*businessphone_domain.WhatsAppBusinessPhoneNumber, error)
}

type activateWorkflowUseCase struct {
	repo              workflow.WorkflowRepository
	catalogFn         func() []workflow.NodeDefinition
	templateRepo      template_domain.Repository
	agentRepo         agent.Repository
	mediaRepo         media_domain.MediaRepository
	labelRepo         label_domain.Repository
	departmentRepo    dept_domain.Repository
	workspaceRepo     workspaceMemberLookup
	businessPhoneRepo businessPhoneLookup
	mcpCollectionRepo domainmcp.CollectionRepository
	knowledgeBaseRepo knowledgeBaseLookup

	modelLookup ModelLookup
}

// knowledgeBaseLookup is the slice of the RAG knowledge-base repository the
// activation validator needs: resolve a set of IDs so an ai_agent
// node can't be activated pointing at knowledge bases from another workspace (or
// that no longer exist).
type knowledgeBaseLookup interface {
	FindByIDs(ctx context.Context, ids []string) ([]*rag_domain.KnowledgeBase, error)
}

// ModelLookup reports whether a single model id is one the AI provider offers, so
// a workflow pointed at a non-existent model is rejected at activation instead of
// failing at run time. It is expected to be backed by a TTL cache (see
// openrouter.ModelValidator) so validation is a cheap membership check on the few
// models actually used, not a per-call fetch of the whole catalog.
type ModelLookup interface {
	IsValidModel(ctx context.Context, modelID string) (bool, error)
}

func NewActivateWorkflowUseCase(repo workflow.WorkflowRepository) workflow.ActivateWorkflowUseCase {
	return &activateWorkflowUseCase{repo: repo}
}

func (uc *activateWorkflowUseCase) SetCatalogFn(fn func() []workflow.NodeDefinition) {
	uc.catalogFn = fn
}

func (uc *activateWorkflowUseCase) SetTemplateRepo(repo template_domain.Repository) {
	uc.templateRepo = repo
}

func (uc *activateWorkflowUseCase) SetAgentRepo(repo agent.Repository) {
	uc.agentRepo = repo
}

func (uc *activateWorkflowUseCase) SetMediaRepo(repo media_domain.MediaRepository) {
	uc.mediaRepo = repo
}

func (uc *activateWorkflowUseCase) SetLabelRepo(repo label_domain.Repository) {
	uc.labelRepo = repo
}

func (uc *activateWorkflowUseCase) SetDepartmentRepo(repo dept_domain.Repository) {
	uc.departmentRepo = repo
}

func (uc *activateWorkflowUseCase) SetWorkspaceRepo(repo workspace_domain.Repository) {
	uc.workspaceRepo = repo
}

func (uc *activateWorkflowUseCase) SetBusinessPhoneRepo(repo businessphone_domain.Repository) {
	uc.businessPhoneRepo = repo
}

func (uc *activateWorkflowUseCase) SetMCPCollectionRepo(repo domainmcp.CollectionRepository) {
	uc.mcpCollectionRepo = repo
}

func (uc *activateWorkflowUseCase) SetKnowledgeBaseRepo(repo rag_domain.KnowledgeBaseRepository) {
	uc.knowledgeBaseRepo = repo
}

func (uc *activateWorkflowUseCase) SetModelLookup(lookup ModelLookup) {
	uc.modelLookup = lookup
}

func (uc *activateWorkflowUseCase) Execute(workflowID string) (*workflow.Workflow, error) {
	if workflowID == "" {
		return nil, workflow.ErrWorkflowNotFound
	}

	w, err := uc.repo.FindByID(workflowID)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, workflow.ErrWorkflowNotFound
	}

	if w.Status != workflow.WorkflowStatusDraft && w.Status != workflow.WorkflowStatusPaused {
		return nil, workflow.ErrCannotActivate
	}

	w.Normalize()

	leafNodes := collectExecuteModeLeafNodes(&w.Graph)
	var catalog []workflow.NodeDefinition
	if uc.catalogFn != nil {
		catalog = uc.catalogFn()
	}

	if err := workflow.ValidateGraph(&w.Graph, w.Type, leafNodes); err != nil {
		return nil, err
	}
	if err := validateNodeScopes(&w.Graph, w.Type, catalog); err != nil {
		return nil, err
	}

	if len(catalog) > 0 {
		if err := validateRequiredOutputEdges(&w.Graph, catalog); err != nil {
			return nil, err
		}

		workflowWorkspaceID := strings.TrimSpace(w.WorkspaceID)
		var validators []workflow.ConfigValidator
		if uc.businessPhoneRepo != nil {
			validators = append(validators, &businessPhoneValidator{repo: uc.businessPhoneRepo, workspaceID: workflowWorkspaceID})
		}
		if uc.templateRepo != nil {
			validators = append(validators, &templateValidator{repo: uc.templateRepo, businessPhoneRepo: uc.businessPhoneRepo, workspaceID: workflowWorkspaceID})
		}
		if uc.agentRepo != nil {
			validators = append(validators, &agentValidator{repo: uc.agentRepo, workspaceID: workflowWorkspaceID})
		}
		if uc.mcpCollectionRepo != nil {
			validators = append(validators, &mcpCollectionValidator{repo: uc.mcpCollectionRepo, workspaceID: workflowWorkspaceID})
		}
		if uc.knowledgeBaseRepo != nil {
			validators = append(validators, &knowledgeBaseValidator{repo: uc.knowledgeBaseRepo, workspaceID: workflowWorkspaceID})
		}
		if uc.mediaRepo != nil {
			validators = append(validators, &mediaValidator{repo: uc.mediaRepo, workspaceID: workflowWorkspaceID})
		}
		if uc.labelRepo != nil {
			validators = append(validators, &labelValidator{repo: uc.labelRepo, workspaceID: workflowWorkspaceID})
		}
		if uc.departmentRepo != nil {
			validators = append(validators, &departmentValidator{repo: uc.departmentRepo, workspaceID: workflowWorkspaceID})
		}
		if uc.workspaceRepo != nil {
			validators = append(validators, &memberValidator{repo: uc.workspaceRepo, workspaceID: workflowWorkspaceID})
		}
		if uc.modelLookup != nil {
			validators = append(validators, &modelValidator{lookup: uc.modelLookup})
		}
		// NOTE: tts_model validity is a PURE rule (workflow.ValidatePublicTTSModel
		// in PureGraphRules), so it runs in the builder lint AND activation, the AI
		// sees an invalid tts_model while building, not only here.
		validators = append(validators, &workflowReferenceValidator{repo: uc.repo, workspaceID: workflowWorkspaceID})
		if err := workflow.ValidateNodeConfigs(&w.Graph, catalog, validators...); err != nil {
			return nil, err
		}
	}

	// The SAME pure blocking rules the builder lint enforces, from the one shared
	// registry, so "the builder said valid" and "activation accepts it" can never
	// disagree on a pure rule. Repo-backed VALIDITY (does the id exist?) is the
	// only thing layered above, via the ConfigValidators run earlier.
	if err := workflow.RunPureGraphRules(&w.Graph); err != nil {
		return nil, err
	}

	// Required DYNAMIC output edges (e.g. an AI-agent's response/"default" path),
	// the SAME rule the builder lint runs, enforced here so the backend is the
	// source of truth: a workflow with an unhandled response path cannot activate.
	if err := workflow.ValidateRequiredDynamicOutputs(&w.Graph, builderHandleResolver); err != nil {
		return nil, err
	}

	w.Status = workflow.WorkflowStatusActive

	if err := uc.repo.Update(w); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(workflowID)
}

func collectExecuteModeLeafNodes(g *workflow.Graph) map[string]bool {

	executeAgents := make(map[string]bool)
	for _, node := range g.Nodes {
		if node.Type != workflow.NodeTypeActionAIAgent {
			continue
		}
		toolMode, _ := node.Config["tool_mode"].(string)
		if toolMode == "execute" {
			executeAgents[node.ID] = true
		}
	}
	if len(executeAgents) == 0 {
		return nil
	}

	type edgeInfo struct {
		source string
		label  string
	}
	incomingByTarget := make(map[string][]edgeInfo)
	for _, e := range g.Edges {
		incomingByTarget[e.Target] = append(incomingByTarget[e.Target], edgeInfo{source: e.Source, label: e.Label})
	}

	reserved := map[string]bool{"default": true, "erro": true, "": true}

	leafNodes := make(map[string]bool)
	for targetID, edges := range incomingByTarget {
		allFromExecuteAgent := true
		for _, e := range edges {
			if !executeAgents[e.source] || reserved[e.label] {
				allFromExecuteAgent = false
				break
			}
		}
		if allFromExecuteAgent {
			leafNodes[targetID] = true
		}
	}
	return leafNodes
}

// The graph-configuration rules below now live in domain/workflow
// (config_rules.go) so the activation path and the AI Workflow Builder lint
// share a single implementation. These thin wrappers preserve the existing
// call sites (Execute + the activation tests) while delegating to the domain.

func validateSegmentedSendConflict(g *workflow.Graph) error {
	return workflow.ValidateSegmentedSendConflict(g)
}

func validateRequiredOutputEdges(graph *workflow.Graph, catalog []workflow.NodeDefinition) error {
	return workflow.ValidateRequiredOutputEdges(graph, catalog)
}

func validateNodeScopes(graph *workflow.Graph, wfType workflow.WorkflowType, catalog []workflow.NodeDefinition) error {
	return workflow.ValidateNodeScopes(graph, wfType, catalog)
}

type templateValidator struct {
	repo              template_domain.Repository
	businessPhoneRepo businessPhoneLookup
	workspaceID       string
}

func (v *templateValidator) Validate(n *workflow.Node) error {
	if n.Type != workflow.NodeTypeActionSendTemplate {
		return nil
	}
	templateID, _ := n.Config["template_id"].(string)
	if strings.TrimSpace(templateID) == "" {
		return nil
	}
	tmpl, err := v.repo.FindByID(templateID)
	if err != nil || tmpl == nil {
		return fmt.Errorf("%w: node %q template_id %q", workflow.ErrNodeInvalidTemplateID, n.ID, templateID)
	}
	if !tmpl.CanBeSent() {
		return fmt.Errorf("%w: node %q template %q is not approved (status: %s)", workflow.ErrNodeInvalidTemplateID, n.ID, tmpl.Name, tmpl.Status)
	}

	businessPhoneID, _ := n.Config["business_phone_id"].(string)
	businessPhoneID = strings.TrimSpace(businessPhoneID)
	if businessPhoneID == "" || v.businessPhoneRepo == nil {
		return nil
	}

	phone, err := v.businessPhoneRepo.FindByID(businessPhoneID)
	if err != nil || phone == nil || !phone.BelongsToWorkspace(v.workspaceID) {
		return nil
	}
	if strings.TrimSpace(tmpl.WABAId) != "" && strings.TrimSpace(phone.WABAId) != "" && strings.TrimSpace(tmpl.WABAId) != strings.TrimSpace(phone.WABAId) {
		return fmt.Errorf("%w: node %q template_id %q incompatible with business_phone_id %q", workflow.ErrNodeInvalidTemplateID, n.ID, templateID, businessPhoneID)
	}
	return nil
}

type agentValidator struct {
	repo        agent.Repository
	workspaceID string
}

func (v *agentValidator) Validate(n *workflow.Node) error {
	if n.Type != workflow.NodeTypeActionAIAgent {
		return nil
	}

	// Conditional required-field PRESENCE per source mode, prompt-mode
	// model/instructions AND agent-mode agent_id, is a PURE rule, enforced by
	// BOTH the builder lint and activation via workflow.ValidateAIAgentSourceConfig
	// (in PureGraphRules). The repo-backed validator only needs to check AGENT-mode
	// id VALIDITY here, the one thing the pure lint can't do (it needs the DB).
	source, _ := n.Config["source"].(string)
	if source != "prompt" {
		if agentID, _ := n.Config["agent_id"].(string); strings.TrimSpace(agentID) != "" {
			ag, err := v.repo.FindByID(agentID)
			if err != nil || ag == nil || !sameWorkspaceID(v.workspaceID, ag.WorkspaceID) {
				return fmt.Errorf("%w: node %q agent_id %q", workflow.ErrNodeInvalidAgentID, n.ID, agentID)
			}
		}
	}

	if n.Type == workflow.NodeTypeActionAIAgent {
		toolMode, _ := n.Config["tool_mode"].(string)
		if toolMode != "" && toolMode != "route" && toolMode != "execute" {
			return fmt.Errorf("%w: node %q tool_mode %q", workflow.ErrNodeInvalidToolMode, n.ID, toolMode)
		}
		responseMode, _ := n.Config["response_mode"].(string)
		if responseMode != "" && responseMode != "default" && responseMode != "segmented" {
			return fmt.Errorf("%w: node %q response_mode %q", workflow.ErrNodeInvalidResponseMode, n.ID, responseMode)
		}
	}
	return nil
}

type mcpCollectionValidator struct {
	repo        domainmcp.CollectionRepository
	workspaceID string
}

func (v *mcpCollectionValidator) Validate(n *workflow.Node) error {
	if n.Type != workflow.NodeTypeActionAIAgent {
		return nil
	}
	raw, ok := n.Config["mcp_collection_ids"]
	if !ok || raw == nil {
		return nil
	}
	ids := extractStringSlice(raw)
	if len(ids) == 0 {
		return nil
	}
	found, err := v.repo.ListByIDs(context.Background(), v.workspaceID, ids)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(found))
	for _, c := range found {
		seen[c.ID] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("%w: node %q mcp_collection_id %q", workflow.ErrNodeInvalidMCPCollectionID, n.ID, id)
		}
	}
	return nil
}

type knowledgeBaseValidator struct {
	repo        knowledgeBaseLookup
	workspaceID string
}

func (v *knowledgeBaseValidator) Validate(n *workflow.Node) error {
	if n.Type != workflow.NodeTypeActionAIAgent {
		return nil
	}
	ids := extractStringSlice(n.Config["knowledge_base_ids"])
	if len(ids) == 0 {
		return nil
	}
	found, err := v.repo.FindByIDs(context.Background(), ids)
	if err != nil {
		return err
	}
	inWorkspace := make(map[string]struct{}, len(found))
	for _, kb := range found {
		if kb != nil && sameWorkspaceID(v.workspaceID, kb.WorkspaceID) {
			inWorkspace[kb.ID] = struct{}{}
		}
	}
	for _, id := range ids {
		if _, ok := inWorkspace[id]; !ok {
			return fmt.Errorf("%w: node %q knowledge_base_id %q", workflow.ErrNodeInvalidKnowledgeBaseID, n.ID, id)
		}
	}
	return nil
}

func extractStringSlice(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// modelValidator rejects ai_agent / voip_ai_agent nodes whose inline `model`
// points at a model the provider doesn't offer. Mirrors the other resource-id
// validators (department, label, agent…); the valid set is the provider's model
// catalog, checked one id at a time through a TTL-cached lookup.
type modelValidator struct {
	lookup ModelLookup
}

func (v *modelValidator) Validate(n *workflow.Node) error {
	if n.Type != workflow.NodeTypeActionAIAgent {
		return nil
	}
	// In agent mode the model comes from the selected agent, not this field.
	if source, _ := n.Config["source"].(string); strings.TrimSpace(source) == "agent" {
		return nil
	}
	model, _ := n.Config["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" || strings.Contains(model, "{{") {
		return nil
	}
	valid, err := v.lookup.IsValidModel(context.Background(), model)
	if err != nil {
		return nil // catalog unavailable → don't block activation
	}
	if !valid {
		return fmt.Errorf("%w: node %q model %q", workflow.ErrNodeInvalidModelID, n.ID, model)
	}
	return nil
}

type mediaValidator struct {
	repo        media_domain.MediaRepository
	workspaceID string
}

func (v *mediaValidator) Validate(n *workflow.Node) error {
	if n.Type == workflow.NodeTypeActionSendMedia {
		mediaURL, _ := n.Config["media_url"].(string)
		if strings.TrimSpace(mediaURL) != "" {
			return nil
		}
		mediaID, _ := n.Config["media_id"].(string)
		if strings.TrimSpace(mediaID) == "" {
			return nil
		}
		m, err := v.repo.GetMediaByID(mediaID)
		if err != nil || m == nil || !sameWorkspaceID(v.workspaceID, m.WorkspaceID) {
			return fmt.Errorf("%w: node %q media_id %q", workflow.ErrNodeInvalidMediaID, n.ID, mediaID)
		}
		return nil
	}
	return nil
}

type labelValidator struct {
	repo        label_domain.Repository
	workspaceID string
}

func (v *labelValidator) Validate(n *workflow.Node) error {
	switch n.Type {
	case workflow.NodeTypeActionAssignLabel, workflow.NodeTypeConditionCheckLabel:
		labelID, _ := n.Config["label_id"].(string)
		if strings.TrimSpace(labelID) == "" {
			return nil
		}
		l, err := v.repo.FindByID(labelID)
		if err != nil || l == nil || !sameWorkspaceID(v.workspaceID, l.WorkspaceID) {
			return fmt.Errorf("%w: node %q label_id %q", workflow.ErrNodeInvalidLabelID, n.ID, labelID)
		}
	}
	return nil
}

type departmentValidator struct {
	repo        dept_domain.Repository
	workspaceID string
}

func (v *departmentValidator) Validate(n *workflow.Node) error {
	if n.Type != workflow.NodeTypeActionTransferDepartment {
		return nil
	}
	departmentID, _ := n.Config["department_id"].(string)
	if strings.TrimSpace(departmentID) == "" {
		return nil
	}
	d, err := v.repo.GetDepartmentByID(departmentID)
	if err != nil || d == nil || !sameWorkspaceID(v.workspaceID, d.WorkspaceID) {
		return fmt.Errorf("%w: node %q department_id %q", workflow.ErrNodeInvalidDepartmentID, n.ID, departmentID)
	}
	return nil
}

type memberValidator struct {
	repo        workspaceMemberLookup
	workspaceID string
}

func (v *memberValidator) Validate(n *workflow.Node) error {
	// memberKey is the config field naming a workspace member for each node type
	// that targets one. assign_member uses member_id.
	if n.Type != workflow.NodeTypeActionAssignMember {
		return nil
	}
	const memberKey = "member_id"

	memberID, _ := n.Config[memberKey].(string)
	memberID = strings.TrimSpace(memberID)
	// Empty is left to the required-field check; an interpolated value
	// ({{state.target_user_id}}) is only known at runtime, so skip it here.
	if memberID == "" || strings.Contains(memberID, "{{") {
		return nil
	}
	m, err := v.repo.GetMember(v.workspaceID, memberID)
	if err != nil || m == nil || !sameWorkspaceID(v.workspaceID, m.WorkspaceID) {
		return fmt.Errorf("%w: node %q %s %q", workflow.ErrNodeInvalidMemberID, n.ID, memberKey, memberID)
	}
	return nil
}

type businessPhoneValidator struct {
	repo        businessPhoneLookup
	workspaceID string
}

func (v *businessPhoneValidator) Validate(n *workflow.Node) error {
	if n.Type != workflow.NodeTypeActionSendTemplate {
		return nil
	}
	businessPhoneID, _ := n.Config["business_phone_id"].(string)
	if strings.TrimSpace(businessPhoneID) == "" {
		return nil
	}
	phone, err := v.repo.FindByID(businessPhoneID)
	if err != nil || phone == nil || !phone.BelongsToWorkspace(v.workspaceID) {
		return fmt.Errorf("%w: node %q business_phone_id %q", workflow.ErrNodeInvalidBusinessPhoneID, n.ID, businessPhoneID)
	}
	return nil
}

type workflowReferenceValidator struct {
	repo        workflow.WorkflowRepository
	workspaceID string
}

func (v *workflowReferenceValidator) Validate(n *workflow.Node) error {
	if n.Type != workflow.NodeTypeActionRunWorkflow {
		return nil
	}
	workflowID, _ := n.Config["workflow_id"].(string)
	if strings.TrimSpace(workflowID) == "" {
		return nil
	}
	referencedWorkflow, err := v.repo.FindByID(workflowID)
	if err != nil || referencedWorkflow == nil || !sameWorkspaceID(v.workspaceID, referencedWorkflow.WorkspaceID) {
		return fmt.Errorf("%w: node %q workflow_id %q", workflow.ErrNodeInvalidWorkflowID, n.ID, workflowID)
	}
	return nil
}

func sameWorkspaceID(workflowWorkspaceID, resourceWorkspaceID string) bool {
	return strings.TrimSpace(workflowWorkspaceID) != "" && strings.TrimSpace(workflowWorkspaceID) == strings.TrimSpace(resourceWorkspaceID)
}
