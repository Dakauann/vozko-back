package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
	"vozko/domain/messaging"
	"vozko/domain/tools"
)

type Service struct {
	client            *openrouter.Client
	defaultModel      string
	toolService       tools.Service
	defaultTemp       float32
	maxToolIterations int
	billingPub        messaging.MessageQueuePub
	// usageFetcher recovers token usage when a stream is cut before the inline
	// usage chunk arrives (cancel/timeout/abort), so the turn is still billed.
	// Optional: nil disables recovery.
	usageFetcher generationUsageFetcher
	// catalogFetcher loads the priced, popularity-sorted model catalog for the UI
	// (GetModelsWithPricing). Optional: nil falls back to the library ListModels.
	catalogFetcher *modelCatalogFetcher
}

type Config struct {
	APIKey       string
	DefaultModel string
	HTTPReferer  string
	XTitle       string
}

func NewService(cfg Config, toolSvc tools.Service, billingPub messaging.MessageQueuePub) *Service {
	var opts []openrouter.Option
	if cfg.HTTPReferer != "" {
		opts = append(opts, openrouter.WithHTTPReferer(cfg.HTTPReferer))
	}
	if cfg.XTitle != "" {
		opts = append(opts, openrouter.WithXTitle(cfg.XTitle))
	}

	return &Service{
		client:            openrouter.NewClient(cfg.APIKey, opts...),
		defaultModel:      strings.TrimSpace(cfg.DefaultModel),
		toolService:       toolSvc,
		defaultTemp:       0.2,
		maxToolIterations: ai.DefaultMaxToolIterations,
		billingPub:        billingPub,
		usageFetcher:      newHTTPGenerationFetcher(cfg.APIKey, openRouterDefaultBaseURL),
		catalogFetcher:    newModelCatalogFetcher(cfg.APIKey, openRouterDefaultBaseURL),
	}
}

func (s *Service) GenerateStream(ctx context.Context, input ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	if s.client == nil {
		return nil, ai.ErrProviderUnavailable
	}
	if len(input.Messages) == 0 {
		return nil, ai.ErrNoMessages
	}

	req := s.buildRequest(input)
	if req.Model == "" {
		return nil, ai.ErrProviderUnavailable
	}

	req.StreamOptions = &openrouter.StreamOptions{IncludeUsage: true}

	eventCh := make(chan ai.StreamEvent, 64)
	execMode := input.ExecutionModeOrDefault()
	maxIter := s.resolveMaxIterations(input.ToolIterationsOrDefault())
	hasTools := len(req.Tools) > 0

	go func() {
		defer close(eventCh)

		var allToolCalls []ai.ToolCall
		var fullText strings.Builder
		shouldEnd := false

		for iter := 0; ; iter++ {
			stream, err := s.client.CreateChatCompletionStream(ctx, req)
			if err != nil {
				eventCh <- ai.StreamEvent{Type: ai.StreamEventError, Error: fmt.Errorf("stream: %w", err)}
				return
			}

			toolAcc := make(map[int]*openrouter.ToolCall)
			var iterText strings.Builder
			finishReason := ""
			// Per-iteration: each stream reports its own usage in a final chunk and
			// carries the generation id on every chunk. Scoping these to the iteration
			// prevents a cut stream from re-billing the previous iteration's usage and
			// lets us recover usage by id when the final chunk never arrives.
			var totalUsage *openrouter.Usage
			var genID string

			for {
				select {
				case <-ctx.Done():
					stream.Close()
					eventCh <- ai.StreamEvent{Type: ai.StreamEventError, Error: ctx.Err()}
					s.billStreamUsage(input.WorkspaceID, req.Model, genID, totalUsage)
					return
				default:
				}

				resp, err := stream.Recv()
				if err != nil {
					if err == io.EOF {
						break
					}
					if isStreamClosedError(err) {
						break
					}
					stream.Close()
					eventCh <- ai.StreamEvent{Type: ai.StreamEventError, Error: err}
					s.billStreamUsage(input.WorkspaceID, req.Model, genID, totalUsage)
					return
				}

				if resp.ID != "" {
					genID = resp.ID
				}
				if resp.Usage != nil {
					totalUsage = resp.Usage
				}

				if len(resp.Choices) == 0 {
					if totalUsage != nil && finishReason != "" {
						break
					}
					continue
				}

				choice := resp.Choices[0]

				if reasoning := streamReasoningDelta(choice.Delta); reasoning != "" {
					select {
					case eventCh <- ai.StreamEvent{Type: ai.StreamEventReasoning, Token: reasoning}:
					case <-ctx.Done():
						stream.Close()
						s.billStreamUsage(input.WorkspaceID, req.Model, genID, totalUsage)
						return
					}
				}

				if choice.Delta.Content != "" {
					iterText.WriteString(choice.Delta.Content)
					fullText.WriteString(choice.Delta.Content)
					select {
					case eventCh <- ai.StreamEvent{Type: ai.StreamEventToken, Token: choice.Delta.Content}:
					case <-ctx.Done():
						stream.Close()
						s.billStreamUsage(input.WorkspaceID, req.Model, genID, totalUsage)
						return
					}
				}

				for _, call := range choice.Delta.ToolCalls {
					idx := 0
					if call.Index != nil {
						idx = *call.Index
					}
					if existing, ok := toolAcc[idx]; ok {
						if call.ID != "" {
							existing.ID = call.ID
						}
						if call.Function.Name != "" {
							existing.Function.Name = call.Function.Name
						}
						existing.Function.Arguments += call.Function.Arguments
					} else {
						toolAcc[idx] = &openrouter.ToolCall{
							ID:       call.ID,
							Type:     call.Type,
							Function: openrouter.FunctionCall{Name: call.Function.Name, Arguments: call.Function.Arguments},
						}
					}
				}

				if choice.FinishReason != "" {
					finishReason = string(choice.FinishReason)
				}
			}
			stream.Close()

			s.billStreamUsage(input.WorkspaceID, req.Model, genID, totalUsage)

			pending := make([]openrouter.ToolCall, 0, len(toolAcc))
			for _, tc := range toolAcc {
				pending = append(pending, *tc)
			}

			shouldReturn := len(pending) == 0 || !hasTools ||
				execMode == ai.ToolExecutionModeNone ||
				execMode != ai.ToolExecutionModeAuto ||
				s.toolService == nil ||
				iter >= maxIter

			if shouldReturn {
				if len(pending) > 0 {
					allToolCalls = append(allToolCalls, convertToolCalls(pending)...)
				}
				doneEvt := ai.StreamEvent{
					Type:             ai.StreamEventDone,
					FullText:         fullText.String(),
					AllToolCalls:     allToolCalls,
					ShouldEndSession: shouldEnd,
					FinishReason:     finishReason,
				}
				if totalUsage != nil {
					doneEvt.Usage = &ai.Usage{
						PromptTokens:     totalUsage.PromptTokens,
						CompletionTokens: totalUsage.CompletionTokens,
						TotalTokens:      totalUsage.TotalTokens,
					}
				}
				eventCh <- doneEvt
				return
			}

			for _, tc := range pending {
				toolCall := ai.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: parseToolArguments(tc.Function.Arguments),
				}
				eventCh <- ai.StreamEvent{Type: ai.StreamEventToolCall, ToolCall: &toolCall}

				var result tools.ExecutionResult
				var err error
				toolConfig := input.ToolConfigs[strings.ToLower(tc.Function.Name)]
				if len(toolConfig) > 0 {
					result, err = s.toolService.ExecuteWithConfig(ctx, tc.Function.Name, toolConfig, toolCall.Arguments)
				} else {
					result, err = s.toolService.Execute(ctx, tc.Function.Name, toolCall.Arguments)
				}
				if err != nil {
					result = tools.ExecutionResult{Result: fmt.Sprintf("tool %s failed: %v", tc.Function.Name, err), IsError: true, ContextUpdateText: fmt.Sprintf("tool %s failed: %v", tc.Function.Name, err)}
				}
				toolCall.Result = &result
				eventCh <- ai.StreamEvent{Type: ai.StreamEventToolResult, ToolCall: &toolCall, ToolResult: &result}

				if result.ShouldEndSession {
					shouldEnd = true
				}
				allToolCalls = append(allToolCalls, toolCall)
			}

			log.Printf("[openrouter] iteration %d completed, finishReason=%s, pendingTools=%d, textLen=%d", iter, finishReason, len(pending), iterText.Len())

			req.Messages = append(req.Messages, openrouter.ChatCompletionMessage{
				Role:      openrouter.ChatMessageRoleAssistant,
				Content:   openrouter.Content{Text: iterText.String()},
				ToolCalls: pending,
			})
			for _, tc := range allToolCalls[len(allToolCalls)-len(pending):] {
				req.Messages = append(req.Messages, openrouter.ToolMessage(tc.ID, serializeToolResult(tc.Result)))
			}

			if finishReason != "tool_calls" && finishReason != "function_call" {
				log.Printf("[openrouter] exiting tool loop: finishReason=%s (not tool_calls), totalToolCalls=%d", finishReason, len(allToolCalls))
				exitEvt := ai.StreamEvent{
					Type:             ai.StreamEventDone,
					FullText:         fullText.String(),
					AllToolCalls:     allToolCalls,
					ShouldEndSession: shouldEnd,
					FinishReason:     finishReason,
				}
				if totalUsage != nil {
					exitEvt.Usage = &ai.Usage{
						PromptTokens:     totalUsage.PromptTokens,
						CompletionTokens: totalUsage.CompletionTokens,
						TotalTokens:      totalUsage.TotalTokens,
					}
				}
				eventCh <- exitEvt
				return
			}
		}
	}()

	return eventCh, nil
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
		return nil, ai.ErrProviderUnavailable
	}

	execMode := input.ExecutionModeOrDefault()
	maxIter := s.resolveMaxIterations(input.ToolIterationsOrDefault())
	hasTools := len(req.Tools) > 0

	var totalUsage ai.Usage
	var executedCalls []ai.ToolCall

	for iter := 0; ; iter++ {
		resp, err := s.client.CreateChatCompletion(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("openrouter: %w", err)
		}

		if resp.Usage != nil {
			totalUsage.PromptTokens += resp.Usage.PromptTokens
			totalUsage.CompletionTokens += resp.Usage.CompletionTokens
			totalUsage.TotalTokens += resp.Usage.TotalTokens

			if input.WorkspaceID == "" {
				log.Printf("CRITICAL: [ai-billing] missing workspace_id for model=%s, NOT billing (REVENUE LEAK)", req.Model)
			} else {
				s.publishBillingEvent(input.WorkspaceID, req.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
			}
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("openrouter: empty response from %s", req.Model)
		}

		msg := resp.Choices[0].Message
		pending := convertToolCalls(msg.ToolCalls)

		noToolCalls := len(msg.ToolCalls) == 0
		cannotExecute := !hasTools ||
			execMode == ai.ToolExecutionModeNone ||
			execMode != ai.ToolExecutionModeAuto ||
			s.toolService == nil
		hitLimit := iter >= maxIter

		if noToolCalls || cannotExecute {
			content := strings.TrimSpace(msg.Content.Text)

			out := &ai.GenerateOutput{
				Message:      ai.Message{Role: ai.RoleAssistant, Content: content},
				ToolCalls:    append(executedCalls, pending...),
				Usage:        totalUsage,
				FinishReason: string(resp.Choices[0].FinishReason),
			}

			return out, nil
		}

		if hitLimit {

			req.Messages = append(req.Messages, msg)
			for _, call := range msg.ToolCalls {
				args := parseToolArguments(call.Function.Arguments)
				var result tools.ExecutionResult
				var execErr error
				toolConfig := input.ToolConfigs[strings.ToLower(call.Function.Name)]
				if len(toolConfig) > 0 {
					result, execErr = s.toolService.ExecuteWithConfig(ctx, call.Function.Name, toolConfig, args)
				} else {
					result, execErr = s.toolService.Execute(ctx, call.Function.Name, args)
				}
				if execErr != nil {
					result = tools.ExecutionResult{Result: fmt.Sprintf("tool %s failed: %v", call.Function.Name, execErr), IsError: true, ContextUpdateText: fmt.Sprintf("tool %s failed: %v", call.Function.Name, execErr)}
				}
				resultCopy := result
				executedCalls = append(executedCalls, ai.ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: args, Result: &resultCopy})
				req.Messages = append(req.Messages, openrouter.ToolMessage(call.ID, serializeToolResult(&result)))
			}
			req.Tools = nil
			log.Printf("[openrouter] tool iteration limit reached (%d); forcing text response", maxIter)
			continue
		}

		req.Messages = append(req.Messages, msg)
		for _, call := range msg.ToolCalls {
			args := parseToolArguments(call.Function.Arguments)

			var result tools.ExecutionResult
			var err error
			toolConfig := input.ToolConfigs[strings.ToLower(call.Function.Name)]
			if len(toolConfig) > 0 {
				result, err = s.toolService.ExecuteWithConfig(ctx, call.Function.Name, toolConfig, args)
			} else {
				result, err = s.toolService.Execute(ctx, call.Function.Name, args)
			}
			if err != nil {
				result = tools.ExecutionResult{Result: fmt.Sprintf("tool %s failed: %v", call.Function.Name, err), IsError: true, ContextUpdateText: fmt.Sprintf("tool %s failed: %v", call.Function.Name, err)}
			}
			resultCopy := result
			executedCalls = append(executedCalls, ai.ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: args, Result: &resultCopy})
			req.Messages = append(req.Messages, openrouter.ToolMessage(call.ID, serializeToolResult(&result)))
		}
	}
}

func (s *Service) GetAvaibleModels(ctx context.Context) ([]string, error) {
	if s.client == nil {
		return nil, ai.ErrProviderUnavailable
	}
	models, err := s.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("openrouter: %w", err)
	}
	result := make([]string, 0, len(models))
	for _, m := range models {
		result = append(result, m.ID)
	}
	return result, nil
}

// GetModelsWithPricing returns the priced model catalog for the picker UI. It
// prefers the direct /models?sort=most-popular fetch (popularity order + created +
// context length, TTL-cached) and falls back to the library's unsorted ListModels
// when that's unavailable.
func (s *Service) GetModelsWithPricing(ctx context.Context) ([]ai.ModelInfo, error) {
	if models, ok := s.catalogFetcher.FetchModelsWithPricing(ctx); ok {
		return models, nil
	}

	if s.client == nil {
		return nil, ai.ErrProviderUnavailable
	}
	models, err := s.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("openrouter: %w", err)
	}
	result := make([]ai.ModelInfo, 0, len(models))
	for _, m := range models {
		info := ai.ModelInfo{
			ID:   m.ID,
			Name: m.Name,
		}

		if v := parseFloat64(m.Pricing.Prompt); v > 0 {
			info.PromptPrice = v * 1_000_000
		}
		if v := parseFloat64(m.Pricing.Completion); v > 0 {
			info.CompletionPrice = v * 1_000_000
		}
		if m.ContextLength != nil && *m.ContextLength > 0 {
			info.ContextLength = *m.ContextLength
		} else if m.TopProvider.ContextLength != nil && *m.TopProvider.ContextLength > 0 {
			info.ContextLength = *m.TopProvider.ContextLength
		}
		info.Created = m.Created
		result = append(result, info)
	}
	return result, nil
}

func (s *Service) buildRequest(input ai.GenerateInput) openrouter.ChatCompletionRequest {
	messages := make([]openrouter.ChatCompletionMessage, 0, len(input.Messages)+1)

	if sys := strings.TrimSpace(input.SystemPrompt); sys != "" {
		messages = append(messages, openrouter.SystemMessage(getBrazilTimePrefix()+sys))
	}
	for _, m := range input.Messages {
		switch m.Role {
		case ai.RoleSystem:
			messages = append(messages, openrouter.SystemMessage(m.Content))
		case ai.RoleAssistant:
			// An assistant message that made tool calls must replay them (the chat
			// API requires the matching RoleTool results to follow), so serialize
			// them onto the message instead of dropping them.
			if len(m.ToolCalls) > 0 {
				messages = append(messages, openrouter.ChatCompletionMessage{
					Role:      openrouter.ChatMessageRoleAssistant,
					Content:   openrouter.Content{Text: m.Content},
					ToolCalls: toOpenRouterToolCalls(m.ToolCalls),
				})
			} else {
				messages = append(messages, openrouter.AssistantMessage(m.Content))
			}
		case ai.RoleUser:
			messages = append(messages, openrouter.UserMessage(m.Content))
		case ai.RoleTool:
			messages = append(messages, openrouter.ToolMessage(m.ToolCallID, m.Content))
		}
	}

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

	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = s.defaultModel
	}

	req := openrouter.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Temperature: temp,
		Tools:       convertTools(defs),
	}

	if input.MaxTokens > 0 {
		req.MaxTokens = input.MaxTokens
	}
	if input.ReasoningMaxTokens > 0 {
		// Cap chain-of-thought so it can't consume the whole output budget (which on
		// reasoning models like Gemini 3 leaves no room for the actual answer/tool call).
		mt := input.ReasoningMaxTokens
		req.Reasoning = &openrouter.ChatCompletionReasoning{MaxTokens: &mt}
	}
	if input.ToolChoice != "" && len(req.Tools) > 0 {
		req.ToolChoice = input.ToolChoice
	}
	if rf := input.ResponseFormat; rf != nil {
		switch rf.Type {
		case ai.ResponseFormatJSONObject:
			req.ResponseFormat = &openrouter.ChatCompletionResponseFormat{
				Type: openrouter.ChatCompletionResponseFormatTypeJSONObject,
			}
		case ai.ResponseFormatJSONSchema:
			name := strings.TrimSpace(rf.JSONSchemaName)
			if name == "" {
				name = "response"
			}
			schema := openrouterJSONSchema(rf.JSONSchema)
			req.ResponseFormat = &openrouter.ChatCompletionResponseFormat{
				Type: openrouter.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &openrouter.ChatCompletionResponseFormatJSONSchema{
					Name:        name,
					Description: rf.JSONSchemaDescription,
					Schema:      schema,
					Strict:      rf.JSONSchemaStrict,
				},
			}
		case ai.ResponseFormatText:
			req.ResponseFormat = &openrouter.ChatCompletionResponseFormat{
				Type: openrouter.ChatCompletionResponseFormatTypeText,
			}
		}
	}
	return req
}

// openrouterJSONSchema adapts a plain map schema to json.Marshaler for the SDK.
type openrouterJSONSchema map[string]any

func (s openrouterJSONSchema) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]any(s))
}

// toOpenRouterToolCalls converts domain tool calls back into the provider's
// wire format so a caller-supplied assistant turn can replay its tool calls.
func toOpenRouterToolCalls(calls []ai.ToolCall) []openrouter.ToolCall {
	out := make([]openrouter.ToolCall, 0, len(calls))
	for _, c := range calls {
		args := "{}"
		if len(c.Arguments) > 0 {
			if b, err := json.Marshal(c.Arguments); err == nil {
				args = string(b)
			}
		}
		out = append(out, openrouter.ToolCall{
			ID:       c.ID,
			Type:     openrouter.ToolTypeFunction,
			Function: openrouter.FunctionCall{Name: c.Name, Arguments: args},
		})
	}
	return out
}

func (s *Service) resolveMaxIterations(requested int) int {
	limit := s.maxToolIterations
	if limit <= 0 {
		limit = ai.DefaultMaxToolIterations
	}
	if requested > 0 && requested < limit {
		return requested
	}
	return limit
}

// streamReasoningDelta extracts the reasoning/thinking text from a streamed delta.
// Providers expose it either as `reasoning` (most models) or `reasoning_content`
// (deepseek-style); we forward whichever is present so callers can render a live
// thinking view. Returns "" when the delta carries no reasoning.
func streamReasoningDelta(delta openrouter.ChatCompletionStreamChoiceDelta) string {
	if delta.Reasoning != nil && *delta.Reasoning != "" {
		return *delta.Reasoning
	}
	return delta.ReasoningContent
}

func isStreamClosedError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "response body closed") ||
		strings.Contains(s, "client disconnected") ||
		strings.Contains(s, "stream closed")
}

func getBrazilTimePrefix() string {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return fmt.Sprintf("[Current Date/Time in Brazil: %s]\n\n", time.Now().In(loc).Format("02/01/2006 15:04:05"))
}

// resolveParamType delegates to the shared tools.ResolveParamType so the LLM
// tool-schema mapping and the workflow builder validation stay on one list.
func resolveParamType(raw string) (schemaType, formatHint string) {
	return tools.ResolveParamType(raw)
}

func convertTools(defs []tools.Definition) []openrouter.Tool {
	if len(defs) == 0 {
		return nil
	}
	result := make([]openrouter.Tool, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		props := make(map[string]interface{})
		for k, p := range def.Parameters {
			if k = strings.TrimSpace(k); k != "" {
				schemaType, formatHint := resolveParamType(p.Type)
				desc := p.Description
				if formatHint != "" {
					if desc != "" {
						desc += " "
					}
					desc += formatHint
				}
				prop := map[string]interface{}{"type": schemaType, "description": desc}
				if len(p.Enum) > 0 {
					prop["enum"] = p.Enum
				}
				if schemaType == "array" && p.Items != nil {
					itemsSchema := map[string]interface{}{"type": p.Items.Type}
					if p.Items.Description != "" {
						itemsSchema["description"] = p.Items.Description
					}
					if p.Items.Type == "object" && len(p.Items.Properties) > 0 {
						itemProps := make(map[string]interface{})
						for ik, ip := range p.Items.Properties {
							itemProp := map[string]interface{}{"type": ip.Type, "description": ip.Description}
							if len(ip.Enum) > 0 {
								itemProp["enum"] = ip.Enum
							}
							itemProps[ik] = itemProp
						}
						itemsSchema["properties"] = itemProps
						if len(p.Items.Required) > 0 {
							itemsSchema["required"] = p.Items.Required
						}
					}
					prop["items"] = itemsSchema
				}
				if schemaType == "object" && p.Items != nil && len(p.Items.Properties) > 0 {
					objProps := make(map[string]interface{})
					for ik, ip := range p.Items.Properties {
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
					if len(p.Items.Required) > 0 {
						prop["required"] = p.Items.Required
					}
				}
				props[k] = prop
			}
		}
		if len(props) == 0 {
			schema := map[string]interface{}{"type": "object"}
			if len(def.Required) > 0 {
				schema["required"] = def.Required
			}
			result = append(result, openrouter.Tool{
				Type:     openrouter.ToolTypeFunction,
				Function: &openrouter.FunctionDefinition{Name: name, Description: def.Description, Parameters: schema},
			})
			continue
		}
		schema := map[string]interface{}{"type": "object", "properties": props}
		if len(def.Required) > 0 {
			schema["required"] = def.Required
		}
		result = append(result, openrouter.Tool{
			Type:     openrouter.ToolTypeFunction,
			Function: &openrouter.FunctionDefinition{Name: name, Description: def.Description, Parameters: schema},
		})
	}
	return result
}

func convertToolCalls(calls []openrouter.ToolCall) []ai.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	result := make([]ai.ToolCall, 0, len(calls))
	for _, c := range calls {
		result = append(result, ai.ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: parseToolArguments(c.Function.Arguments)})
	}
	return result
}

func parseToolArguments(raw string) map[string]interface{} {
	if raw = strings.TrimSpace(raw); raw == "" {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return map[string]interface{}{"_raw": raw}
	}
	return result
}

func serializeToolResult(res *tools.ExecutionResult) string {
	if res == nil {
		return "{}"
	}
	payload := map[string]interface{}{"result": res.Result, "is_error": res.IsError}
	if ctx := strings.TrimSpace(res.ContextUpdateText); ctx != "" {
		payload["context_update"] = ctx
	}
	if res.ShouldEndSession {
		payload["should_end_session"] = true
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"result":"%v","is_error":%t}`, res.Result, res.IsError)
	}
	return string(b)
}

var _ ai.Service = (*Service)(nil)

func parseFloat64(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
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
