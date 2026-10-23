package node_executors

import (
	"encoding/json"
	"fmt"

	"vozko/domain/ai"
	"vozko/domain/workflow"
)

const maxWorkflowToolTurns = 12
const maxIdenticalWorkflowToolCalls = 2

func toolHistoryKey(nodeID string) string { return "_ai_tool_history:" + nodeID }

func readToolHistory(ctx *workflow.NodeContext) ([]ai.Message, error) {
	raw := ctx.State.GetString(toolHistoryKey(ctx.Node.ID))
	if raw == "" {
		return nil, nil
	}
	var messages []ai.Message
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		return nil, fmt.Errorf("workflow: cannot read the agent's tool history: %w", err)
	}
	if len(messages) > 2*maxWorkflowToolTurns {
		return nil, fmt.Errorf("workflow: agent tool history exceeds its turn limit")
	}
	for i, message := range messages {
		if i%2 == 0 {
			if message.Role != ai.RoleAssistant || len(message.ToolCalls) != 1 || message.ToolCalls[0].ID == "" || message.ToolCalls[0].Name == "" {
				return nil, fmt.Errorf("workflow: invalid assistant call in tool history")
			}
		} else if message.Role != ai.RoleTool || message.ToolCallID != messages[i-1].ToolCalls[0].ID {
			return nil, fmt.Errorf("workflow: unmatched result in tool history")
		}
	}
	return messages, nil
}

func writeToolHistory(ctx *workflow.NodeContext, messages []ai.Message) error {
	raw, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("workflow: cannot save the agent's tool history: %w", err)
	}
	ctx.State.Set(toolHistoryKey(ctx.Node.ID), string(raw))
	return nil
}

func workflowToolMessages(ctx *workflow.NodeContext) ([]ai.Message, error) {
	if previous := ctx.Graph.FindNode(ctx.State.GetString("_prev_node_id")); previous != nil && (previous.Type.AwaitsContact() || previous.Type.IsTrigger()) {
		ctx.State.Delete(toolHistoryKey(ctx.Node.ID))
		ctx.State.Delete("_pending_tool_call")
		return nil, nil
	}
	messages, err := readToolHistory(ctx)
	if err != nil {
		return nil, err
	}
	raw, exists := ctx.State.Get("_pending_tool_call")
	if !exists {
		return messages, nil
	}
	pending, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("workflow: invalid pending agent tool call")
	}
	name, _ := pending["name"].(string)
	source, _ := pending["source_ai_node"].(string)
	if source != ctx.Node.ID {
		return messages, nil
	}
	target, _ := pending["target_node"].(string)
	id, _ := pending["id"].(string)
	args, _ := pending["args"].(map[string]interface{})
	if name == "" || target == "" {
		return nil, fmt.Errorf("workflow: pending tool call has no name or target")
	}
	if len(messages) == 0 {
		calls := []ai.ToolCall{{ID: id, Name: name, Arguments: args}}
		ai.EnsureToolCallIDs(calls)
		messages = append(messages, ai.Message{Role: ai.RoleAssistant, ToolCalls: calls})
	}
	last := messages[len(messages)-1]
	if last.Role != ai.RoleAssistant || len(last.ToolCalls) != 1 || last.ToolCalls[0].Name != name || (id != "" && last.ToolCalls[0].ID != id) {
		return nil, fmt.Errorf("workflow: tool result does not match the pending assistant call")
	}
	result := workflow.NodeOutput(ctx.State, target)
	if previous := ctx.State.GetString("_prev_node_id"); previous != "" && previous != target {
		if continuation := workflow.NodeOutput(ctx.State, previous); len(continuation) > 0 {
			result = map[string]interface{}{"result": result, "continuation": continuation}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("workflow: the pending tool returned no recorded result")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("workflow: cannot encode the tool result: %w", err)
	}
	messages = append(messages, ai.Message{Role: ai.RoleTool, ToolCallID: last.ToolCalls[0].ID, Content: string(encoded)})
	if err := writeToolHistory(ctx, messages); err != nil {
		return nil, err
	}
	ctx.State.Delete("_pending_tool_call")
	return messages, nil
}

func rememberWorkflowToolCall(ctx *workflow.NodeContext, output *ai.GenerateOutput, call ai.ToolCall) (ai.ToolCall, error) {
	messages, err := readToolHistory(ctx)
	if err != nil {
		return call, err
	}
	encodedArgs, err := json.Marshal(call.Arguments)
	if err != nil {
		return call, fmt.Errorf("workflow: cannot encode tool arguments: %w", err)
	}
	turns, repeated := 0, 0
	consecutive := true
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != ai.RoleAssistant || len(message.ToolCalls) == 0 {
			continue
		}
		turns++
		previous := message.ToolCalls[0]
		args, err := json.Marshal(previous.Arguments)
		if err != nil {
			return call, fmt.Errorf("workflow: cannot read previous tool arguments: %w", err)
		}
		if consecutive && previous.Name == call.Name && string(args) == string(encodedArgs) {
			repeated++
		} else {
			consecutive = false
		}
	}
	if repeated >= maxIdenticalWorkflowToolCalls {
		return call, fmt.Errorf("workflow: tool loop detected: %s repeated identical arguments without a new customer message or another tool", call.Name)
	}
	if turns >= maxWorkflowToolTurns {
		return call, fmt.Errorf("workflow: agent exceeded %d tool calls without waiting for the customer", maxWorkflowToolTurns)
	}
	calls := []ai.ToolCall{call}
	ai.EnsureToolCallIDs(calls)
	call = calls[0]
	message := output.Message
	message.Role, message.ToolCalls = ai.RoleAssistant, calls
	messages = append(messages, message)
	return call, writeToolHistory(ctx, messages)
}
