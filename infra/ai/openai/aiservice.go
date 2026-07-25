package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	openai "github.com/sashabaranov/go-openai"

	"vozko/domain/ai"
	"vozko/domain/messaging"
	"vozko/domain/tools"
)

type Client interface {
	CreateChatCompletion(ctx context.Context, request openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}

type Service struct {
	client            Client
	defaultModel      string
	toolService       tools.Service
	defaultTemp       float32
	maxToolIterations int
	billingPub        messaging.MessageQueuePub
}

func NewService(client Client, defaultModel string, toolSvc tools.Service, billingPub messaging.MessageQueuePub) *Service {
	return &Service{
		client:            client,
		defaultModel:      strings.TrimSpace(defaultModel),
		toolService:       toolSvc,
		defaultTemp:       0.2,
		maxToolIterations: ai.DefaultMaxToolIterations,
		billingPub:        billingPub,
	}
}

func (s *Service) GenerateStream(ctx context.Context, input ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	result, err := s.Generate(ctx, input)
	if err != nil {
		ch := make(chan ai.StreamEvent, 1)
		go func() {
			defer close(ch)
			ch <- ai.StreamEvent{Type: ai.StreamEventError, Error: err}
		}()
		return ch, nil
	}

	ch := make(chan ai.StreamEvent, 2)
	go func() {
		defer close(ch)
		if result.Message.Content != "" {
			ch <- ai.StreamEvent{Type: ai.StreamEventToken, Token: result.Message.Content}
		}

		shouldEnd := false
		for _, tc := range result.ToolCalls {
			if tc.Result != nil && tc.Result.ShouldEndSession {
				shouldEnd = true
			}
		}

		ch <- ai.StreamEvent{
			Type:             ai.StreamEventDone,
			FullText:         result.Message.Content,
			AllToolCalls:     result.ToolCalls,
			ShouldEndSession: shouldEnd,
		}
	}()
	return ch, nil
}

func (s *Service) Generate(ctx context.Context, input ai.GenerateInput) (*ai.GenerateOutput, error) {
	if s.client == nil {
		return nil, ai.ErrProviderUnavailable
	}
	if len(input.Messages) == 0 {
		return nil, ai.ErrNoMessages
	}

	req := s.buildRequest(input)
	if req.Model == "" {
		req.Model = s.defaultModel
	}
	if req.Model == "" {
		return nil, ai.ErrProviderUnavailable
	}

	executionMode := input.ExecutionModeOrDefault()
	maxIterations := s.resolveMaxIterations(input.ToolIterationsOrDefault())
	hasTools := len(req.Tools) > 0
	usage := ai.Usage{}
	executedCalls := make([]ai.ToolCall, 0)

	for iteration := 0; ; iteration++ {
		resp, err := s.client.CreateChatCompletion(ctx, req)
		if err != nil {
			return nil, err
		}

		usage = addUsage(usage, resp.Usage)
		if resp.Usage.TotalTokens > 0 {
			if input.WorkspaceID == "" {
				log.Printf("[ai-billing] skipping: missing workspace_id for model=%s", req.Model)
			} else {
				s.publishBillingEvent(input.WorkspaceID, req.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
			}
		}
		choice := firstChoice(resp)
		if choice == nil {
			return nil, fmt.Errorf("openai: empty response")
		}

		message := choice.Message
		pendingCalls := convertToolCalls(message.ToolCalls)

		if len(message.ToolCalls) == 0 || !hasTools || executionMode == ai.ToolExecutionModeNone {
			return &ai.GenerateOutput{
				Message:   convertAssistantMessage(message),
				ToolCalls: append(executedCalls, pendingCalls...),
				Usage:     usage,
			}, nil
		}

		if executionMode != ai.ToolExecutionModeAuto || s.toolService == nil {
			return &ai.GenerateOutput{
				Message:   convertAssistantMessage(message),
				ToolCalls: append(executedCalls, pendingCalls...),
				Usage:     usage,
			}, nil
		}

		if iteration >= maxIterations {

			req.Messages = append(req.Messages, message)
			executed := s.executeToolCalls(ctx, message.ToolCalls, input.ToolConfigs)
			executedCalls = append(executedCalls, executed...)
			for _, call := range executed {
				req.Messages = append(req.Messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    serializeToolResult(call.Result),
					ToolCallID: call.ID,
				})
			}
			req.Tools = nil
			log.Printf("[openai] tool iteration limit reached (%d); forcing text response", maxIterations)
			continue
		}

		req.Messages = append(req.Messages, message)
		executed := s.executeToolCalls(ctx, message.ToolCalls, input.ToolConfigs)
		executedCalls = append(executedCalls, executed...)

		for _, call := range executed {
			req.Messages = append(req.Messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    serializeToolResult(call.Result),
				ToolCallID: call.ID,
			})
		}
	}
}

func (s *Service) buildRequest(input ai.GenerateInput) openai.ChatCompletionRequest {
	messages := convertInputMessages(input.SystemPrompt, input.Messages)
	temp := input.Temperature
	if temp <= 0 {
		temp = s.defaultTemp
	}

	defs := input.Tools
	// Only fall back to the full default tool registry when the caller actually
	// allows tool execution. Callers that disable execution (ToolExecutionModeNone,
	// e.g. workflow AI-agent nodes in prompt mode with no custom tools) and pass no
	// tools want *none*. Injecting the default set here let the model emit tool
	// calls it could never run, which suppressed its text reply (the node would
	// finish with response=0 chars and deliver nothing).
	if defs == nil && s.toolService != nil && input.ToolExecutionMode != ai.ToolExecutionModeNone {
		defs = s.toolService.Definitions()
	}

	req := openai.ChatCompletionRequest{
		Model:       strings.TrimSpace(input.Model),
		Messages:    messages,
		Temperature: temp,
		MaxTokens:   input.MaxTokens,
		Tools:       convertTools(defs),
	}
	// go-openai v1.21 supports json_object / text; json_schema is OpenRouter-only here.
	if rf := input.ResponseFormat; rf != nil {
		switch rf.Type {
		case ai.ResponseFormatJSONObject:
			req.ResponseFormat = &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONObject,
			}
		case ai.ResponseFormatText:
			req.ResponseFormat = &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeText,
			}
		}
	}
	return req
}

func (s *Service) GetAvaibleModels(ctx context.Context) ([]string, error) {
	if s.client == nil {
		return nil, ai.ErrProviderUnavailable
	}

	openaiClient, ok := s.client.(*openai.Client)
	if !ok {
		return nil, ai.ErrProviderUnavailable
	}

	models, err := openaiClient.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(models.Models))
	for _, model := range models.Models {
		result = append(result, model.ID)
	}

	return result, nil
}

func (s *Service) GetModelsWithPricing(ctx context.Context) ([]ai.ModelInfo, error) {
	ids, err := s.GetAvaibleModels(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ai.ModelInfo, 0, len(ids))
	for _, id := range ids {
		result = append(result, ai.ModelInfo{ID: id, Name: id})
	}
	return result, nil
}

func (s *Service) resolveMaxIterations(requested int) int {
	limit := s.maxToolIterations
	if limit <= 0 {
		limit = ai.DefaultMaxToolIterations
	}
	if requested > 0 && requested < limit {
		limit = requested
	}
	return limit
}

func (s *Service) executeToolCalls(ctx context.Context, calls []openai.ToolCall, toolConfigs map[string]map[string]interface{}) []ai.ToolCall {
	if len(calls) == 0 {
		return nil
	}

	results := make([]ai.ToolCall, 0, len(calls))
	for _, call := range calls {
		args := parseToolArguments(call.Function.Arguments)
		var execResult tools.ExecutionResult
		if s.toolService != nil {
			var res tools.ExecutionResult
			var err error
			toolConfig := toolConfigs[strings.ToLower(call.Function.Name)]
			if len(toolConfig) > 0 {
				res, err = s.toolService.ExecuteWithConfig(ctx, call.Function.Name, toolConfig, args)
			} else {
				res, err = s.toolService.Execute(ctx, call.Function.Name, args)
			}
			if err != nil {
				execResult = tools.ExecutionResult{Result: fmt.Sprintf("tool %s failed: %v", call.Function.Name, err), IsError: true, ContextUpdateText: fmt.Sprintf("tool %s failed: %v", call.Function.Name, err)}
			} else {
				execResult = res
			}
		} else {
			execResult = tools.ExecutionResult{Result: fmt.Sprintf("tool service unavailable for %s", call.Function.Name), IsError: true}
		}

		execCopy := execResult
		results = append(results, ai.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: args,
			Result:    &execCopy,
		})
	}

	return results
}

func convertInputMessages(system string, messages []ai.Message) []openai.ChatCompletionMessage {
	converted := make([]openai.ChatCompletionMessage, 0, len(messages)+1)
	if trimmed := strings.TrimSpace(system); trimmed != "" {
		converted = append(converted, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: trimmed,
		})
	}

	for _, msg := range messages {
		role := toOpenAIRole(msg.Role)
		if role == "" {
			continue
		}

		converted = append(converted, openai.ChatCompletionMessage{
			Role:       role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
			// Replay any tool calls the assistant made so a caller-supplied agentic
			// history round-trips (the matching RoleTool results must follow).
			ToolCalls: toOpenAIToolCalls(msg.ToolCalls),
		})
	}

	return converted
}

// toOpenAIToolCalls converts domain tool calls into the SDK wire format so a
// caller-supplied assistant turn can replay its tool calls.
func toOpenAIToolCalls(calls []ai.ToolCall) []openai.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]openai.ToolCall, 0, len(calls))
	for _, c := range calls {
		args := "{}"
		if len(c.Arguments) > 0 {
			if b, err := json.Marshal(c.Arguments); err == nil {
				args = string(b)
			}
		}
		out = append(out, openai.ToolCall{
			ID:       c.ID,
			Type:     openai.ToolTypeFunction,
			Function: openai.FunctionCall{Name: c.Name, Arguments: args},
		})
	}
	return out
}

func toOpenAIRole(role ai.Role) string {
	switch role {
	case ai.RoleSystem:
		return openai.ChatMessageRoleSystem
	case ai.RoleAssistant:
		return openai.ChatMessageRoleAssistant
	case ai.RoleUser:
		return openai.ChatMessageRoleUser
	case ai.RoleTool:
		return openai.ChatMessageRoleTool
	default:
		return ""
	}
}

func fromOpenAIRole(role string) ai.Role {
	switch role {
	case openai.ChatMessageRoleSystem:
		return ai.RoleSystem
	case openai.ChatMessageRoleAssistant:
		return ai.RoleAssistant
	case openai.ChatMessageRoleTool:
		return ai.RoleTool
	default:
		return ai.RoleUser
	}
}

func convertAssistantMessage(msg openai.ChatCompletionMessage) ai.Message {
	return ai.Message{
		Role:    fromOpenAIRole(msg.Role),
		Content: strings.TrimSpace(msg.Content),
	}
}

func convertTools(defs []tools.Definition) []openai.Tool {
	if len(defs) == 0 {
		return nil
	}

	result := make([]openai.Tool, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}

		result = append(result, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        name,
				Description: def.Description,
				Parameters:  buildToolSchema(def),
			},
		})
	}

	return result
}

func buildToolSchema(def tools.Definition) map[string]interface{} {
	properties := make(map[string]interface{})
	for key, param := range def.Parameters {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			schemaType, formatHint := resolveParamType(param.Type)
			desc := param.Description
			if formatHint != "" {
				if desc != "" {
					desc += " "
				}
				desc += formatHint
			}
			prop := map[string]interface{}{
				"type":        schemaType,
				"description": desc,
			}
			if len(param.Enum) > 0 {
				prop["enum"] = param.Enum
			}
			if schemaType == "object" && param.Items != nil && len(param.Items.Properties) > 0 {
				objProps := make(map[string]interface{})
				for ik, ip := range param.Items.Properties {
					subType, subFormat := resolveParamType(ip.Type)
					subDesc := ip.Description
					if subFormat != "" {
						if subDesc != "" {
							subDesc += " "
						}
						subDesc += subFormat
					}
					objProp := map[string]interface{}{"type": subType, "description": subDesc}
					if len(ip.Enum) > 0 {
						objProp["enum"] = ip.Enum
					}
					objProps[ik] = objProp
				}
				prop["properties"] = objProps
				if len(param.Items.Required) > 0 {
					prop["required"] = param.Items.Required
				}
			}
			if schemaType == "array" && param.Items != nil {
				itemsSchema := map[string]interface{}{"type": param.Items.Type}
				if param.Items.Description != "" {
					itemsSchema["description"] = param.Items.Description
				}
				if param.Items.Type == "object" && len(param.Items.Properties) > 0 {
					itemProps := make(map[string]interface{})
					for ik, ip := range param.Items.Properties {
						subType, subFormat := resolveParamType(ip.Type)
						subDesc := ip.Description
						if subFormat != "" {
							if subDesc != "" {
								subDesc += " "
							}
							subDesc += subFormat
						}
						itemProp := map[string]interface{}{"type": subType, "description": subDesc}
						if len(ip.Enum) > 0 {
							itemProp["enum"] = ip.Enum
						}
						itemProps[ik] = itemProp
					}
					itemsSchema["properties"] = itemProps
					if len(param.Items.Required) > 0 {
						itemsSchema["required"] = param.Items.Required
					}
				}
				prop["items"] = itemsSchema
			}
			properties[trimmed] = prop
		}
	}

	schema := map[string]interface{}{
		"type": "object",
	}
	if len(properties) > 0 {
		schema["properties"] = properties
	}
	if len(def.Required) > 0 {
		schema["required"] = def.Required
	}
	return schema
}

// resolveParamType delegates to the shared tools.ResolveParamType so the LLM
// tool-schema mapping and the workflow builder validation stay on one list.
func resolveParamType(raw string) (schemaType, formatHint string) {
	return tools.ResolveParamType(raw)
}

func convertToolCalls(calls []openai.ToolCall) []ai.ToolCall {
	if len(calls) == 0 {
		return nil
	}

	converted := make([]ai.ToolCall, 0, len(calls))
	for _, call := range calls {
		converted = append(converted, ai.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: parseToolArguments(call.Function.Arguments),
		})
	}
	return converted
}

func parseToolArguments(raw string) map[string]interface{} {
	result := map[string]interface{}{}
	if strings.TrimSpace(raw) == "" {
		return result
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return map[string]interface{}{"_raw": raw}
	}
	return result
}

func serializeToolResult(res *tools.ExecutionResult) string {
	if res == nil {
		return "{}"
	}

	payload := map[string]interface{}{
		"result":   res.Result,
		"is_error": res.IsError,
	}
	if strings.TrimSpace(res.ContextUpdateText) != "" {
		payload["context_update"] = res.ContextUpdateText
	}
	if res.ShouldEndSession {
		payload["should_end_session"] = true
	}

	bytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("{\"result\":\"%v\",\"is_error\":%t}", res.Result, res.IsError)
	}
	return string(bytes)
}

func firstChoice(resp openai.ChatCompletionResponse) *openai.ChatCompletionChoice {
	if len(resp.Choices) == 0 {
		return nil
	}
	return &resp.Choices[0]
}

func addUsage(current ai.Usage, usage openai.Usage) ai.Usage {
	current.PromptTokens += usage.PromptTokens
	current.CompletionTokens += usage.CompletionTokens
	current.TotalTokens += usage.TotalTokens
	return current
}

func (s *Service) publishBillingEvent(workspaceID, model string, promptTokens, completionTokens int) {
	if s.billingPub == nil {
		return
	}
	event := ai.AICompletedEvent{
		RequestID:        uuid.New().String(),
		WorkspaceID:      workspaceID,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[ai-billing] failed to marshal event: %v", err)
		return
	}
	delays := [3]time.Duration{100 * time.Millisecond, 500 * time.Millisecond, 2 * time.Second}
	for i := range delays {
		if err := s.billingPub.Publish(ai.TopicAIBillingCompleted, data); err == nil {
			return
		} else if i < len(delays)-1 {
			log.Printf("[ai-billing] publish attempt %d failed, retrying in %v: %v", i+1, delays[i], err)
			time.Sleep(delays[i])
		} else {
			log.Printf("[ai-billing] CRITICAL publish failed after %d attempts workspace=%s model=%s prompt=%d completion=%d: %v",
				len(delays), workspaceID, model, promptTokens, completionTokens, err)
		}
	}
}

var _ ai.Service = (*Service)(nil)
