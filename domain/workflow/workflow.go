package workflow

import (
	"encoding/json"
	"strings"
	"time"
)

type WorkflowStatus string

const (
	WorkflowStatusDraft    WorkflowStatus = "draft"
	WorkflowStatusActive   WorkflowStatus = "active"
	WorkflowStatusPaused   WorkflowStatus = "paused"
	WorkflowStatusArchived WorkflowStatus = "archived"
)

func (s WorkflowStatus) Valid() bool {
	switch s {
	case WorkflowStatusDraft, WorkflowStatusActive, WorkflowStatusPaused, WorkflowStatusArchived:
		return true
	}
	return false
}

type WorkflowType string

const (
	WorkflowTypeMessages WorkflowType = "messages"
)

func (t WorkflowType) Valid() bool {
	return t == WorkflowTypeMessages
}

type TriggerType string

const (
	TriggerFirstMessage    TriggerType = "trigger_first_message"
	TriggerMessageReceived TriggerType = "trigger_message_received"
	TriggerCampaignSent    TriggerType = "trigger_campaign_sent"
	TriggerStageAdded      TriggerType = "trigger_stage_added"
	TriggerManual          TriggerType = "trigger_manual"
	TriggerNoReply         TriggerType = "trigger_no_reply"
	TriggerWebhook         TriggerType = "trigger_webhook"
)

func (t TriggerType) Valid() bool {
	switch t {
	case TriggerFirstMessage, TriggerMessageReceived, TriggerCampaignSent,
		TriggerStageAdded, TriggerManual, TriggerNoReply, TriggerWebhook:
		return true
	}
	return false
}

func (t TriggerType) WorkflowType() WorkflowType {
	return WorkflowTypeMessages
}

type NodeType string

const (
	NodeTypeTriggerFirstMessage    NodeType = "trigger_first_message"
	NodeTypeTriggerMessageReceived NodeType = "trigger_message_received"
	NodeTypeTriggerCampaignSent    NodeType = "trigger_campaign_sent"
	NodeTypeTriggerStageAdded      NodeType = "trigger_stage_added"
	NodeTypeTriggerManual          NodeType = "trigger_manual"
	NodeTypeTriggerNoReply         NodeType = "trigger_no_reply"
	NodeTypeTriggerWebhook         NodeType = "trigger_webhook"

	NodeTypeActionSendText     NodeType = "action_send_text"
	NodeTypeActionSendTemplate NodeType = "action_send_template"
	NodeTypeActionSendEmail    NodeType = "action_send_email"
	// NodeTypeActionSendInteractive is the single-choice prompt node. It works on
	// every channel with an InteractiveAdapter — WhatsApp buttons and lists,
	// Instagram quick replies, Telegram inline keyboards — so it no longer names
	// one channel. See NodeTypeActionSendWhatsappButtonLegacy for the value it
	// was persisted under before.
	NodeTypeActionSendInteractive           NodeType = "action_send_interactive"
	NodeTypeActionSendMedia                 NodeType = "action_send_media"
	NodeTypeActionAIAgent                   NodeType = "action_ai_agent"
	NodeTypeActionAIExtract                 NodeType = "action_ai_extract"
	NodeTypeActionSetVariable               NodeType = "action_set_variable"
	NodeTypeActionHTTPRequest               NodeType = "action_http_request"
	NodeTypeActionRunTool                   NodeType = "action_run_tool"
	NodeTypeActionFormatDate                NodeType = "action_format_date"
	NodeTypeActionCode                      NodeType = "action_code"
	NodeTypeActionRunWorkflow               NodeType = "action_run_workflow"
	NodeTypeActionLoop                      NodeType = "action_loop"
	NodeTypeActionGetCurrentTime            NodeType = "action_get_current_time"
	NodeTypeActionScheduleMeeting           NodeType = "action_schedule_meeting"
	NodeTypeActionRescheduleMeeting         NodeType = "action_reschedule_meeting"
	NodeTypeActionCheckCalendarAvailability NodeType = "action_check_calendar_availability"
	NodeTypeActionAssignLabel               NodeType = "action_assign_label"
	NodeTypeActionAssignMember              NodeType = "action_assign_member"
	NodeTypeActionTransferDepartment        NodeType = "action_transfer_department"
	NodeTypeActionFinishConversation        NodeType = "action_finish_conversation"

	NodeTypeWaitDuration        NodeType = "wait_duration"
	NodeTypeWaitForReply        NodeType = "wait_for_reply"
	NodeTypeWaitSchedule        NodeType = "wait_schedule"
	NodeTypeConditionBranch     NodeType = "condition_branch"
	NodeTypeConditionAIClassfy  NodeType = "condition_ai_classify"
	NodeTypeConditionTextMatch  NodeType = "condition_text_match"
	NodeTypeConditionFilter     NodeType = "condition_filter"
	NodeTypeConditionCheckLabel NodeType = "condition_check_label"
	NodeTypeEnd                 NodeType = "end"

	NodeTypeDecorationBackground NodeType = "decoration_background"
)

func (n NodeType) Valid() bool {
	switch n {
	case NodeTypeTriggerFirstMessage, NodeTypeTriggerMessageReceived,
		NodeTypeTriggerCampaignSent, NodeTypeTriggerStageAdded,
		NodeTypeTriggerManual, NodeTypeTriggerNoReply, NodeTypeTriggerWebhook,
		NodeTypeActionSendText, NodeTypeActionSendTemplate,
		NodeTypeActionSendEmail,
		NodeTypeActionSendInteractive, NodeTypeActionSendMedia,
		NodeTypeActionAIAgent, NodeTypeActionAIExtract,
		NodeTypeActionSetVariable, NodeTypeActionHTTPRequest,
		NodeTypeActionRunTool,
		NodeTypeActionFormatDate, NodeTypeActionCode,
		NodeTypeActionRunWorkflow, NodeTypeActionLoop,
		NodeTypeActionGetCurrentTime,
		NodeTypeActionCheckCalendarAvailability,
		NodeTypeWaitDuration, NodeTypeWaitForReply,
		NodeTypeWaitSchedule,
		NodeTypeConditionBranch, NodeTypeConditionAIClassfy,
		NodeTypeConditionTextMatch, NodeTypeConditionFilter,
		NodeTypeActionScheduleMeeting, NodeTypeActionRescheduleMeeting,
		NodeTypeActionAssignLabel, NodeTypeActionAssignMember,
		NodeTypeActionTransferDepartment,
		NodeTypeActionFinishConversation,
		NodeTypeConditionCheckLabel,
		NodeTypeEnd,
		NodeTypeDecorationBackground:
		return true
	}
	return false
}

func (n NodeType) IsTrigger() bool {
	switch n {
	case NodeTypeTriggerFirstMessage, NodeTypeTriggerMessageReceived,
		NodeTypeTriggerCampaignSent, NodeTypeTriggerStageAdded,
		NodeTypeTriggerManual, NodeTypeTriggerNoReply, NodeTypeTriggerWebhook:
		return true
	}
	return false
}

func (n NodeType) IsWait() bool {
	switch n {
	case NodeTypeWaitDuration, NodeTypeWaitForReply, NodeTypeWaitSchedule:
		return true
	}
	return false
}

// IsInteractivePrompt reports whether the node sends an interactive WhatsApp
// message (reply buttons or a list) and then PARKS the run waiting for the
// contact's choice, branching on the selected option ID. It is a send + wait +
// branch hybrid: like a wait node it parks (WaitReasonReply) and resumes on
// reply or timeout, and like condition_text_match it exposes one dynamic output
// handle per option. It is deliberately NOT folded into IsWait() (which also
// drives the wait catalog category); instead
// the reply-resume and timeout-resume paths gate on
// IsWait() || IsInteractivePrompt() so this node reuses that machinery without
// inheriting the wait node's other behaviors.
func (n NodeType) IsInteractivePrompt() bool {
	return n.Canonical() == NodeTypeActionSendInteractive
}

// ParksForReply reports whether the node suspends the run until the contact
// responds.
//
// It names the thing five call sites actually mean. Three of them wrote it out
// as IsWait() || IsInteractivePrompt(); two wrote only IsWait() and were wrong
// in a way nothing catches — the interactive prompt parks exactly like a wait
// node but is deliberately excluded from IsWait(), so a forgotten clause makes
// a button press behave as if the run had never paused. One predicate removes
// the chance to forget.
func (n NodeType) ParksForReply() bool {
	return n.IsWait() || n.IsInteractivePrompt()
}

// NodeTypeActionSendWhatsappButtonLegacy is the value the interactive prompt was
// persisted under while it was WhatsApp-only.
//
// It is never written again — Canonical maps it forward on read, so a graph
// saved before the rename keeps running and is rewritten to the new value the
// next time it is saved. Deleting this constant would orphan every workflow
// authored before the node became channel-neutral.
const NodeTypeActionSendWhatsappButtonLegacy NodeType = "action_send_whatsapp_button"

// legacyNodeTypes maps retired wire values onto their current ones.
var legacyNodeTypes = map[NodeType]NodeType{
	NodeTypeActionSendWhatsappButtonLegacy: NodeTypeActionSendInteractive,
}

// Canonical resolves a possibly-legacy node type to the one the code uses.
func (n NodeType) Canonical() NodeType {
	if current, ok := legacyNodeTypes[n]; ok {
		return current
	}
	return n
}

// UnmarshalJSON normalizes a legacy node type as the graph is decoded.
//
// This is the single choke point: workflow graphs are stored as JSONB and
// arrive from the API as JSON, so normalizing here means nothing downstream —
// the executor registry, the lint rules, the AI builder, the editor — ever sees
// a retired value. Normalizing at each comparison site instead would mean
// finding every one of them, and missing one fails silently, on old workflows
// only, which is the worst way to find out.
func (n *NodeType) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*n = NodeType(raw).Canonical()
	return nil
}

func (n NodeType) IsCondition() bool {
	switch n {
	case NodeTypeConditionBranch, NodeTypeConditionAIClassfy, NodeTypeConditionTextMatch, NodeTypeConditionFilter, NodeTypeConditionCheckLabel:
		return true
	}
	return false
}

func (n NodeType) IsEnd() bool {
	return n == NodeTypeEnd
}

func (n NodeType) IsLogic() bool {
	switch n {
	case NodeTypeActionLoop, NodeTypeActionCode, NodeTypeActionFormatDate,
		NodeTypeActionGetCurrentTime, NodeTypeActionSetVariable:
		return true
	}
	return false
}

func (n NodeType) IsAI() bool {
	switch n {
	case NodeTypeActionAIAgent, NodeTypeActionAIExtract:
		return true
	}
	return false
}

func (n NodeType) IsMessaging() bool {
	switch n {
	case NodeTypeActionSendText, NodeTypeActionSendTemplate,
		NodeTypeActionSendEmail,
		NodeTypeActionSendInteractive, NodeTypeActionSendMedia:
		return true
	}
	return false
}

func (n NodeType) IsDecoration() bool {
	switch n {
	case NodeTypeDecorationBackground:
		return true
	}
	return false
}

type Node struct {
	ID       string                 `json:"id"`
	Type     NodeType               `json:"type"`
	Position Position               `json:"position"`
	Config   map[string]interface{} `json:"config"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Edge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type Workflow struct {
	ID           string         `json:"id"`
	WorkspaceID  string         `json:"workspaceId"`
	DepartmentID string         `json:"departmentId,omitempty"`
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	Status       WorkflowStatus `json:"status"`
	Type         WorkflowType   `json:"type"`

	TriggerType TriggerType `json:"triggerType,omitempty"`

	TriggerConfig map[string]interface{} `json:"triggerConfig,omitempty"`
	Graph         Graph                  `json:"graph"`
	Version       int                    `json:"version"`
	CreatedAt     time.Time              `json:"createdAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`

	RequiredCampaignVars []string `json:"requiredCampaignVars,omitempty"`
}

func (w *Workflow) Normalize() {
	w.ID = strings.TrimSpace(w.ID)
	w.WorkspaceID = strings.TrimSpace(w.WorkspaceID)
	w.DepartmentID = strings.TrimSpace(w.DepartmentID)
	w.Name = strings.TrimSpace(w.Name)
	w.Description = strings.TrimSpace(w.Description)
	if w.TriggerConfig == nil {
		w.TriggerConfig = make(map[string]interface{})
	}
	if w.Status == "" {
		w.Status = WorkflowStatusDraft
	}

	if !w.TriggerType.Valid() {
		if primary := w.Graph.PrimaryTriggerType(); primary != "" {
			w.TriggerType = primary
		}
	}
	if w.Type == "" {
		if primary := w.Graph.PrimaryTriggerType(); primary != "" {
			w.Type = primary.WorkflowType()
		} else if w.TriggerType.Valid() {
			w.Type = w.TriggerType.WorkflowType()
		} else {
			w.Type = WorkflowTypeMessages
		}
	}

	w.normalizeAIAgentResponseEdges()
}

// normalizeAIAgentResponseEdges migrates legacy graphs that wired an AI-agent's
// text response with an UNLABELED edge: the response handle is now "default", and
// it is required. Relabeling here keeps routing, validation, and the frontend's
// handle rendering uniform without breaking workflows saved before the change.
func (w *Workflow) normalizeAIAgentResponseEdges() {
	aiNodes := make(map[string]bool)
	for _, n := range w.Graph.Nodes {
		if n.Type == NodeTypeActionAIAgent {
			aiNodes[n.ID] = true
		}
	}
	if len(aiNodes) == 0 {
		return
	}
	for i := range w.Graph.Edges {
		e := &w.Graph.Edges[i]
		if aiNodes[e.Source] && strings.TrimSpace(e.Label) == "" {
			e.Label = "default"
		}
	}
}

func (w *Workflow) Validate() error {
	if w.WorkspaceID == "" {
		return ErrWorkspaceIDRequired
	}
	if w.Name == "" {
		return ErrNameRequired
	}
	if !w.Status.Valid() {
		return ErrInvalidStatus
	}
	if !w.Type.Valid() {
		return ErrInvalidWorkflowType
	}

	for _, trig := range w.Graph.TriggerTypes() {
		if !trig.Valid() {
			return ErrInvalidTriggerType
		}
		if trig.WorkflowType() != w.Type {
			return ErrTriggerNotAllowedForType
		}
	}

	if w.TriggerType != "" {
		if !w.TriggerType.Valid() {
			return ErrInvalidTriggerType
		}
		if w.TriggerType.WorkflowType() != w.Type {
			return ErrTriggerNotAllowedForType
		}
	}
	return nil
}

func (w *Workflow) HasTriggerType(t TriggerType) bool {
	return w.Graph.TriggerNodeByType(t) != nil
}

func (w *Workflow) GraphJSON() ([]byte, error) {
	return json.Marshal(w.Graph)
}

func (w *Workflow) TriggerConfigJSON() ([]byte, error) {
	return json.Marshal(w.TriggerConfig)
}

func (g *Graph) FindNode(nodeID string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == nodeID {
			return &g.Nodes[i]
		}
	}
	return nil
}

func (g *Graph) OutgoingEdges(nodeID string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.Source == nodeID {
			out = append(out, e)
		}
	}
	return out
}

func (g *Graph) IncomingEdges(nodeID string) []Edge {
	var in []Edge
	for _, e := range g.Edges {
		if e.Target == nodeID {
			in = append(in, e)
		}
	}
	return in
}

func (g *Graph) TriggerNode() *Node {
	if primary := g.PrimaryTriggerType(); primary != "" {
		return g.TriggerNodeByType(primary)
	}
	for i := range g.Nodes {
		if g.Nodes[i].Type.IsTrigger() {
			return &g.Nodes[i]
		}
	}
	return nil
}

func (g *Graph) TriggerNodes() []*Node {
	var out []*Node
	for i := range g.Nodes {
		if g.Nodes[i].Type.IsTrigger() {
			out = append(out, &g.Nodes[i])
		}
	}
	return out
}

func (g *Graph) TriggerNodeByType(t TriggerType) *Node {
	want := NodeType(t)
	for i := range g.Nodes {
		if g.Nodes[i].Type == want {
			return &g.Nodes[i]
		}
	}
	return nil
}

func (g *Graph) TriggerTypes() []TriggerType {
	var out []TriggerType
	for i := range g.Nodes {
		if g.Nodes[i].Type.IsTrigger() {
			out = append(out, TriggerType(g.Nodes[i].Type))
		}
	}
	return out
}

func (g *Graph) PrimaryTriggerType() TriggerType {
	for i := range g.Nodes {
		if g.Nodes[i].Type.IsTrigger() {
			return TriggerType(g.Nodes[i].Type)
		}
	}
	return ""
}

func (g *Graph) AncestorsOf(targetNodeID string) map[string]bool {
	ancestors := make(map[string]bool)
	queue := []string{targetNodeID}
	visited := map[string]bool{targetNodeID: true}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range g.IncomingEdges(cur) {
			if !visited[e.Source] {
				visited[e.Source] = true
				ancestors[e.Source] = true
				queue = append(queue, e.Source)
			}
		}
	}
	return ancestors
}

const (
	MaxNodesPerWorkflow = 200
	MaxEdgesPerWorkflow = 400
	// MaxExecutionsPerRun bounds node executions within a SINGLE engine pass
	// (one wake). It is re-initialised every time the engine re-enters after a
	// wait, so on its own it cannot catch a loop that passes through a wait node.
	MaxExecutionsPerRun = 500
	// MaxDurableExecutionsPerRun bounds the TOTAL nodes a single run may execute
	// over its ENTIRE life, including across waits/sleeps/retries. Unlike
	// MaxExecutionsPerRun, this counter is persisted in run state and never
	// resets, so a cycle that passes through a wait node cannot loop forever
	// (the failure mode that hammered a partner API on an unbounded 401-retry).
	// Aligned with AWS Step Functions' 25,000-history-event hard stop — roughly
	// 10k node executions once the per-node event overhead is discounted — and
	// ~20x the work of even a very long legitimate conversational or drip run,
	// so it never trips a real workflow.
	MaxDurableExecutionsPerRun = 10000
	MaxRetriesPerNode          = 3
	StuckRunTimeoutMinutes     = 5
)

// StateKeyDurableSteps holds the durable, cross-wait total node-execution
// counter used by the run engine's lifetime circuit breaker.
const StateKeyDurableSteps = "_engine_total_steps"
