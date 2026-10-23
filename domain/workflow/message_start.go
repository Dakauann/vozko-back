package workflow

import "errors"

const (
	NeedsMessageTriggerCode = "workflow_needs_message_trigger"

	InboundTextMessage = "text"
	DataKeyWorkspaceID = "workspace_id"
)

var ErrWorkflowNeedsMessageTrigger = errors.New("workflow: a button tap or an operator start reaches the workflow as the customer's message, so it needs a message received or first message trigger")

var messageStartTriggers = []TriggerType{TriggerMessageReceived, TriggerFirstMessage}

func MessageStartTriggerTypes() []TriggerType {
	return append([]TriggerType(nil), messageStartTriggers...)
}

func MessageStartTrigger(nodes []Node) (*Node, error) {
	for _, wanted := range messageStartTriggers {
		for i := range nodes {
			if nodes[i].Type == NodeType(wanted) {
				return &nodes[i], nil
			}
		}
	}
	return nil, ErrWorkflowNeedsMessageTrigger
}

type MessageStart struct {
	Text             string
	Channel          string
	WorkspaceID      string
	Selection        *OptionSelection
	ContactNumber    string
	TriggerMessageID string
}

func MessageStartRunData(in MessageStart) map[string]interface{} {
	data := InboundMessageData(in.Text, InboundTextMessage, "", in.Selection)
	data[VarChannel] = in.Channel
	data[DataKeyWorkspaceID] = in.WorkspaceID
	ApplyContactNumber(data, in.ContactNumber)
	ApplyTriggerMessage(data, in.TriggerMessageID)
	return data
}
