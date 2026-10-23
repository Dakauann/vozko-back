package workflow

import (
	"encoding/json"
	"slices"
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
	WorkflowTypeVoice    WorkflowType = "voice"
)

func (t WorkflowType) Valid() bool {
	return t == WorkflowTypeMessages || t == WorkflowTypeVoice
}

type TriggerType string

const (
	TriggerFirstMessage    TriggerType = "trigger_first_message"
	TriggerMessageReceived TriggerType = "trigger_message_received"
	TriggerStageAdded      TriggerType = "trigger_stage_added"
	TriggerWebhook         TriggerType = "trigger_webhook"
	TriggerCallReceived    TriggerType = "trigger_call_received"
	TriggerFormSubmitted   TriggerType = "trigger_form_submitted"
	TriggerAgentCall       TriggerType = "trigger_agent_call"
	TriggerBirthday        TriggerType = "trigger_birthday"
)

func (t TriggerType) Valid() bool {
	switch t {
	case TriggerFirstMessage, TriggerMessageReceived,
		TriggerStageAdded, TriggerWebhook,
		TriggerCallReceived, TriggerFormSubmitted, TriggerAgentCall,
		TriggerBirthday:
		return true
	}
	return false
}

func (t TriggerType) WorkflowType() WorkflowType {
	if t == TriggerCallReceived {
		return WorkflowTypeVoice
	}
	return WorkflowTypeMessages
}

type NodeType string

const (
	NodeTypeTriggerFirstMessage    NodeType = "trigger_first_message"
	NodeTypeTriggerMessageReceived NodeType = "trigger_message_received"
	NodeTypeTriggerStageAdded      NodeType = "trigger_stage_added"
	NodeTypeTriggerWebhook         NodeType = "trigger_webhook"
	NodeTypeTriggerCallReceived    NodeType = "trigger_call_received"
	NodeTypeTriggerFormSubmitted   NodeType = "trigger_form_submitted"
	NodeTypeTriggerAgentCall       NodeType = "trigger_agent_call"
	NodeTypeTriggerBirthday        NodeType = "trigger_birthday"

	NodeTypeActionPlayAudio NodeType = "action_play_audio"
	NodeTypeWaitDTMF        NodeType = "wait_dtmf"
	NodeTypeTransferToQueue NodeType = "action_transfer_to_queue"

	NodeTypeActionSendText                  NodeType = "action_send_text"
	NodeTypeActionSendTemplate              NodeType = "action_send_template"
	NodeTypeActionSendEmail                 NodeType = "action_send_email"
	NodeTypeActionSendCarrier               NodeType = "action_send_carrier"
	NodeTypeActionSendPlatformEmail         NodeType = "action_send_platform_email"
	NodeTypeActionSendInteractive           NodeType = "action_send_interactive"
	NodeTypeActionSendForm                  NodeType = "action_send_form"
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
	NodeTypeActionCreateCalendarEvent       NodeType = "action_create_calendar_event"
	NodeTypeActionRescheduleCalendarEvent   NodeType = "action_reschedule_calendar_event"
	NodeTypeActionCheckCalendarAvailability NodeType = "action_check_calendar_availability"
	NodeTypeActionAddSheetRow               NodeType = "action_add_sheet_row"
	NodeTypeActionAssignLabel               NodeType = "action_assign_label"
	NodeTypeActionAssignMember              NodeType = "action_assign_member"
	NodeTypeActionTransferDepartment        NodeType = "action_transfer_department"
	NodeTypeActionFinishConversation        NodeType = "action_finish_conversation"
	NodeTypeActionMoveStage                 NodeType = "action_move_stage"
	NodeTypeActionManageOpportunity         NodeType = "action_manage_opportunity"
	NodeTypeActionUpdateLead                NodeType = "action_update_lead"
	NodeTypeActionConsent                   NodeType = "action_consent"
	NodeTypeActionReplyToAgent              NodeType = "action_reply_to_agent"

	NodeTypeWaitDuration              NodeType = "wait_duration"
	NodeTypeWaitForReply              NodeType = "wait_for_reply"
	NodeTypeWaitSchedule              NodeType = "wait_schedule"
	NodeTypeConditionBranch           NodeType = "condition_branch"
	NodeTypeConditionAIClassfy        NodeType = "condition_ai_classify"
	NodeTypeConditionTextMatch        NodeType = "condition_text_match"
	NodeTypeConditionFilter           NodeType = "condition_filter"
	NodeTypeConditionCheckLabel       NodeType = "condition_check_label"
	NodeTypeConditionCheckStage       NodeType = "condition_check_stage"
	NodeTypeConditionCheckOpportunity NodeType = "condition_check_opportunity"
	NodeTypeConditionChannel          NodeType = "condition_channel"
	NodeTypeConditionConsent          NodeType = "condition_consent"
	NodeTypeEnd                       NodeType = "end"

	NodeTypeDecorationBackground NodeType = "decoration_background"
)

func (n NodeType) Valid() bool {
	switch n {
	case NodeTypeTriggerFirstMessage, NodeTypeTriggerMessageReceived,
		NodeTypeTriggerStageAdded,
		NodeTypeTriggerWebhook,
		NodeTypeTriggerCallReceived, NodeTypeTriggerFormSubmitted,
		NodeTypeTriggerAgentCall,
		NodeTypeTriggerBirthday,
		NodeTypeActionPlayAudio, NodeTypeWaitDTMF,
		NodeTypeTransferToQueue,
		NodeTypeActionSendText, NodeTypeActionSendTemplate,
		NodeTypeActionSendEmail, NodeTypeActionSendCarrier, NodeTypeActionSendPlatformEmail,
		NodeTypeActionSendInteractive, NodeTypeActionSendMedia,
		NodeTypeActionSendForm,
		NodeTypeActionAIAgent, NodeTypeActionAIExtract,
		NodeTypeActionSetVariable, NodeTypeActionHTTPRequest,
		NodeTypeActionRunTool,
		NodeTypeActionFormatDate, NodeTypeActionCode,
		NodeTypeActionRunWorkflow, NodeTypeActionLoop,
		NodeTypeActionGetCurrentTime,
		NodeTypeActionCheckCalendarAvailability,
		NodeTypeActionAddSheetRow,
		NodeTypeWaitDuration, NodeTypeWaitForReply,
		NodeTypeWaitSchedule,
		NodeTypeConditionBranch, NodeTypeConditionAIClassfy,
		NodeTypeConditionTextMatch, NodeTypeConditionFilter,
		NodeTypeActionCreateCalendarEvent, NodeTypeActionRescheduleCalendarEvent,
		NodeTypeActionAssignLabel, NodeTypeActionAssignMember,
		NodeTypeActionTransferDepartment,
		NodeTypeActionFinishConversation,
		NodeTypeActionMoveStage,
		NodeTypeActionManageOpportunity,
		NodeTypeActionUpdateLead,
		NodeTypeActionConsent,
		NodeTypeActionReplyToAgent,
		NodeTypeConditionCheckLabel,
		NodeTypeConditionCheckStage,
		NodeTypeConditionCheckOpportunity,
		NodeTypeConditionChannel,
		NodeTypeConditionConsent,
		NodeTypeEnd,
		NodeTypeDecorationBackground:
		return true
	}
	return false
}

var triggerNodeTypes = []NodeType{
	NodeTypeTriggerFirstMessage, NodeTypeTriggerMessageReceived,
	NodeTypeTriggerStageAdded,
	NodeTypeTriggerWebhook,
	NodeTypeTriggerCallReceived, NodeTypeTriggerFormSubmitted,
	NodeTypeTriggerAgentCall,
	NodeTypeTriggerBirthday,
}

func TriggerNodeTypes() []NodeType {
	return slices.Clone(triggerNodeTypes)
}

func (n NodeType) IsTrigger() bool {
	return slices.Contains(triggerNodeTypes, n)
}

func (n NodeType) WaitsForCaller() bool {
	return n == NodeTypeWaitDTMF
}

func (n NodeType) RetriesOnFailure() bool {
	return n != NodeTypeActionCode
}

func (n NodeType) IsWait() bool {
	switch n {
	case NodeTypeWaitDuration, NodeTypeWaitForReply, NodeTypeWaitSchedule:
		return true
	}
	return false
}

func (n NodeType) IsInteractivePrompt() bool {
	return n.Canonical() == NodeTypeActionSendInteractive
}

func (n NodeType) ParksForReply() bool {
	return n.IsWait() || n.IsInteractivePrompt()
}

func (n NodeType) IsFormPrompt() bool {
	return n == NodeTypeActionSendForm
}

func (n NodeType) AwaitsContact() bool {
	return n.ParksForReply() || n.IsFormPrompt()
}

const (
	NodeTypeActionSendWhatsappButtonLegacy NodeType = "action_send_whatsapp_button"
	NodeTypeActionScheduleMeetingLegacy    NodeType = "action_schedule_meeting"
	NodeTypeActionRescheduleMeetingLegacy  NodeType = "action_reschedule_meeting"
)

var legacyNodeTypes = map[NodeType]NodeType{
	NodeTypeActionSendWhatsappButtonLegacy: NodeTypeActionSendInteractive,
	NodeTypeActionScheduleMeetingLegacy:    NodeTypeActionCreateCalendarEvent,
	NodeTypeActionRescheduleMeetingLegacy:  NodeTypeActionRescheduleCalendarEvent,
}

func LegacyNodeTypes() []NodeType {
	legacy := make([]NodeType, 0, len(legacyNodeTypes))
	for nodeType := range legacyNodeTypes {
		legacy = append(legacy, nodeType)
	}
	slices.Sort(legacy)
	return legacy
}

func (n NodeType) Canonical() NodeType {
	if current, ok := legacyNodeTypes[n]; ok {
		return current
	}
	return n
}

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
	case NodeTypeConditionBranch, NodeTypeConditionAIClassfy, NodeTypeConditionTextMatch, NodeTypeConditionFilter, NodeTypeConditionCheckLabel, NodeTypeConditionCheckStage, NodeTypeConditionCheckOpportunity, NodeTypeConditionChannel, NodeTypeConditionConsent:
		return true
	}
	return false
}

func (n NodeType) IsEnd() bool {
	return n == NodeTypeEnd
}

func (n NodeType) EndsPath() bool {
	return n.IsEnd() || n == NodeTypeActionReplyToAgent
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
		NodeTypeActionSendEmail, NodeTypeActionSendCarrier, NodeTypeActionSendPlatformEmail,
		NodeTypeActionSendInteractive, NodeTypeActionSendMedia,
		NodeTypeActionSendForm:
		return true
	}
	return false
}

func (n NodeType) SendsToCustomer() bool {
	canonical := n.Canonical()
	return canonical.IsMessaging() && canonical != NodeTypeActionSendEmail
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
	Pinned   *NodePin               `json:"pinned,omitempty"`

	TestInputs []TestInput `json:"testInputs,omitempty"`
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

	PublishedVersion      *int       `json:"publishedVersion"`
	PublishedAt           *time.Time `json:"publishedAt"`
	HasUnpublishedChanges bool       `json:"hasUnpublishedChanges"`
	PublishedGraphHash    string     `json:"-"`
	GraphVersion          *int       `json:"-"`

	EntryPoints []EntryPoint `json:"entryPoints,omitempty"`

	Handles map[string][]HandleDefinition `json:"handles,omitempty"`
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
		} else {
			w.TriggerType = ""
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
	CanonicalizeStructuredConfigs(&w.Graph)
}

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

	if err := ValidatePins(&w.Graph); err != nil {
		return err
	}
	if err := ValidateTestInputs(&w.Graph); err != nil {
		return err
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
	return g.AncestorsAvoiding(targetNodeID, "")
}

func (g *Graph) AncestorsAvoiding(targetNodeID, avoidNodeID string) map[string]bool {
	ancestors := make(map[string]bool)
	for nodeID := range g.AncestorDistances(targetNodeID, avoidNodeID) {
		if nodeID != targetNodeID {
			ancestors[nodeID] = true
		}
	}
	return ancestors
}

func (g *Graph) AncestorDistances(targetNodeID, avoidNodeID string) map[string]int {
	sources := make(map[string][]string, len(g.Nodes))
	for _, e := range g.Edges {
		sources[e.Target] = append(sources[e.Target], e.Source)
	}
	distance := map[string]int{targetNodeID: 0}
	queue := []string{targetNodeID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, source := range sources[cur] {
			if avoidNodeID != "" && source == avoidNodeID {
				continue
			}
			if _, seen := distance[source]; !seen {
				distance[source] = distance[cur] + 1
				queue = append(queue, source)
			}
		}
	}
	return distance
}

const (
	MaxNodesPerWorkflow        = 200
	MaxEdgesPerWorkflow        = 400
	MaxExecutionsPerRun        = 500
	MaxDurableExecutionsPerRun = 10000
	MaxRetriesPerNode          = 3
	StuckRunTimeoutMinutes     = 5
)

const (
	CodeNodeDefaultTimeout = 5 * time.Second
	CodeNodeMaxTimeout     = 60 * time.Second
)

const StateKeyDurableSteps = "_engine_total_steps"
