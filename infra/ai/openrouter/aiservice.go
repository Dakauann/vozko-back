package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
	"vozko/domain/messaging"
	"vozko/domain/metrics"
	"vozko/domain/tools"
	"vozko/infra/ai/aibilling"
)

type Service struct {
	client             *openrouter.Client
	defaultModel       string
	toolService        tools.Service
	defaultTemp        float32
	maxToolIterations  int
	billingPub         messaging.MessageQueuePub
	usageFetcher       generationUsageFetcher
	catalogFetcher     *modelCatalogFetcher
	usageRetryDelays   []time.Duration
	recoveries         sync.WaitGroup
	streamDrainTimeout time.Duration
	completionWait     time.Duration
	billingMetrics     metrics.BillingMetricsRecorder
}

type Config struct {
	APIKey       string
	DefaultModel string
	HTTPReferer  string
	XTitle       string
}

func NewService(cfg Config, toolSvc tools.Service, billingPub messaging.MessageQueuePub, billingMetrics metrics.BillingMetricsRecorder) (*Service, error) {
	if billingPub == nil || billingMetrics == nil {
		return nil, fmt.Errorf("%w: openrouter ai service", ai.ErrBillingNotConfigured)
	}

	var opts []openrouter.Option
	if cfg.HTTPReferer != "" {
		opts = append(opts, openrouter.WithHTTPReferer(cfg.HTTPReferer))
	}
	if cfg.XTitle != "" {
		opts = append(opts, openrouter.WithXTitle(cfg.XTitle))
	}
	opts = append(opts, recordingGenerationIDs)

	return &Service{
		client:            openrouter.NewClient(cfg.APIKey, opts...),
		defaultModel:      strings.TrimSpace(cfg.DefaultModel),
		toolService:       toolSvc,
		defaultTemp:       0.2,
		maxToolIterations: ai.DefaultMaxToolIterations,
		billingPub:        billingPub,
		billingMetrics:    billingMetrics,
		usageFetcher:      newHTTPGenerationFetcher(cfg.APIKey, openRouterDefaultBaseURL),
		catalogFetcher:    newModelCatalogFetcher(cfg.APIKey, openRouterDefaultBaseURL),
		usageRetryDelays:  generationRetryDelays,
	}, nil
}

func (s *Service) GenerateStream(ctx context.Context, input ai.GenerateInput) (<-chan ai.StreamEvent, error) {
	if s.client == nil {
		return nil, ai.ErrProviderUnavailable
	}
	if len(input.Messages) == 0 {
		return nil, ai.ErrNoMessages
	}
	if err := s.checkReasoning(ctx, input); err != nil {
		return nil, err
	}

	req := s.buildRequest(s.visibleInput(ctx, input))
	if req.Model == "" {
		return nil, ai.ErrProviderUnavailable
	}

	req.StreamOptions = &openrouter.StreamOptions{IncludeUsage: true}

	eventCh := make(chan ai.StreamEvent, 64)
	execMode := input.ExecutionModeOrDefault()
	maxIter := s.resolveMaxIterations(input.ToolIterationsOrDefault())
	hasTools := len(req.Tools) > 0
	target := billingTarget{workspaceID: input.WorkspaceID, reference: input.BillingReference, model: req.Model}

	go func() {
		defer close(eventCh)

		var allToolCalls []ai.ToolCall
		var fullText strings.Builder
		shouldEnd := false

		for iter := 0; ; iter++ {
			if err := ctx.Err(); err != nil {
				s.abandonStream(nil, eventCh, err)
				return
			}
			attempt, items := s.startStream(ctx, req, target)

			toolAcc := make(map[int]*openrouter.ToolCall)
			var thinking reasoningAccumulator
			var iterText strings.Builder
			finishReason := ""

			for {
				var item streamItem
				var open bool
				select {
				case <-ctx.Done():
					s.abandonStream(attempt, eventCh, ctx.Err())
					return
				case item, open = <-items:
				}
				if !open {
					break
				}
				if item.err != nil {
					s.settle(attempt, exitAfter(item.err))
					eventCh <- ai.StreamEvent{Type: ai.StreamEventError, Error: classified(item.err)}
					return
				}

				resp := item.chunk
				if len(resp.Choices) == 0 {
					if _, usage := attempt.ledger.snapshot(); usage != nil && finishReason != "" {
						break
					}
					continue
				}

				choice := resp.Choices[0]
				thinking.add(choice.Delta.ReasoningDetails)

				if reasoning := streamReasoningDelta(choice.Delta); reasoning != "" {
					select {
					case eventCh <- ai.StreamEvent{Type: ai.StreamEventReasoning, Token: reasoning}:
					case <-ctx.Done():
						s.abandonStream(attempt, eventCh, ctx.Err())
						return
					}
				}

				if choice.Delta.Content != "" {
					iterText.WriteString(choice.Delta.Content)
					fullText.WriteString(choice.Delta.Content)
					select {
					case eventCh <- ai.StreamEvent{Type: ai.StreamEventToken, Token: choice.Delta.Content}:
					case <-ctx.Done():
						s.abandonStream(attempt, eventCh, ctx.Err())
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
			s.settle(attempt, callEnded)
			genID, totalUsage := attempt.ledger.snapshot()

			if streamUnfinished(finishReason, totalUsage) {
				eventCh <- ai.StreamEvent{Type: ai.StreamEventError, Error: classified(fmt.Errorf("%w: model=%s generation=%s finish=%q", ai.ErrStreamIncomplete, req.Model, genID, finishReason))}
				return
			}

			pending := orderedToolCalls(toolAcc)

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
					Reasoning:        thinking.blocks(),
				}
				if totalUsage != nil {
					doneEvt.Usage = usageOf(totalUsage)
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

				startedAt := time.Now()
				result := s.executeTool(tools.WithCallID(ctx, tc.ID), input.ToolConfigs, tc.Function.Name, toolCall.Arguments)
				toolCall.Result = &result
				toolCall.DurationMs = time.Since(startedAt).Milliseconds()
				eventCh <- ai.StreamEvent{Type: ai.StreamEventToolResult, ToolCall: &toolCall, ToolResult: &result}

				if result.ShouldEndSession {
					shouldEnd = true
				}
				allToolCalls = append(allToolCalls, toolCall)
			}

			log.Printf("[openrouter] iteration %d completed, finishReason=%s, pendingTools=%d, textLen=%d", iter, finishReason, len(pending), iterText.Len())

			req.Messages = append(req.Messages, openrouter.ChatCompletionMessage{
				Role:             openrouter.ChatMessageRoleAssistant,
				Content:          openrouter.Content{Text: iterText.String()},
				ToolCalls:        pending,
				ReasoningDetails: reasoningDetailsOf(thinking.blocks()),
			})
			for _, tc := range allToolCalls[len(allToolCalls)-len(pending):] {
				req.Messages = append(req.Messages, openrouter.ToolMessage(tc.ID, serializeToolResult(tc.Result)))
			}
			if cachesExplicitly(req.Model) {
				req.Messages = withMovedTailBreakpoints(req.Messages)
			}

			if finishReason != "tool_calls" && finishReason != "function_call" {
				log.Printf("[openrouter] exiting tool loop: finishReason=%s (not tool_calls), totalToolCalls=%d", finishReason, len(allToolCalls))
				exitEvt := ai.StreamEvent{
					Type:             ai.StreamEventDone,
					FullText:         fullText.String(),
					AllToolCalls:     allToolCalls,
					ShouldEndSession: shouldEnd,
					FinishReason:     finishReason,
					Reasoning:        thinking.blocks(),
				}
				if totalUsage != nil {
					exitEvt.Usage = usageOf(totalUsage)
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
	if err := s.checkReasoning(ctx, input); err != nil {
		return nil, err
	}

	req := s.buildRequest(s.visibleInput(ctx, input))
	if req.Model == "" {
		return nil, ai.ErrProviderUnavailable
	}

	execMode := input.ExecutionModeOrDefault()
	maxIter := s.resolveMaxIterations(input.ToolIterationsOrDefault())
	hasTools := len(req.Tools) > 0
	target := billingTarget{workspaceID: input.WorkspaceID, reference: input.BillingReference, model: req.Model}

	var totalUsage ai.Usage
	var executedCalls []ai.ToolCall
	forced := false

	for iter := 0; ; iter++ {
		resp, err := s.complete(ctx, req, target)
		if err != nil {
			return nil, fmt.Errorf("openrouter: %w", classified(err))
		}

		if resp.Usage != nil {
			totalUsage.PromptTokens += resp.Usage.PromptTokens
			totalUsage.CompletionTokens += resp.Usage.CompletionTokens
			totalUsage.TotalTokens += resp.Usage.TotalTokens
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

		if noToolCalls || cannotExecute || forced {
			content := strings.TrimSpace(msg.Content.Text)

			out := &ai.GenerateOutput{
				Message:      ai.Message{Role: ai.RoleAssistant, Content: content},
				ToolCalls:    append(executedCalls, pending...),
				Usage:        totalUsage,
				FinishReason: string(resp.Choices[0].FinishReason),
			}

			return out, nil
		}

		req.Messages = append(req.Messages, msg)
		for _, call := range msg.ToolCalls {
			args := parseToolArguments(call.Function.Arguments)
			startedAt := time.Now()
			result := s.executeTool(tools.WithCallID(ctx, call.ID), input.ToolConfigs, call.Function.Name, args)
			executedCalls = append(executedCalls, ai.ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: args, Result: &result, DurationMs: time.Since(startedAt).Milliseconds()})
			req.Messages = append(req.Messages, openrouter.ToolMessage(call.ID, serializeToolResult(&result)))
		}
		if cachesExplicitly(req.Model) {
			req.Messages = withMovedTailBreakpoints(req.Messages)
		}
		if hitLimit {
			req.ToolChoice = finalAnswerToolChoice
			forced = true
			log.Printf("[openrouter] tool iteration limit reached (%d); forcing text response", maxIter)
		}
	}
}

func (s *Service) executeTool(ctx context.Context, configs map[string]map[string]interface{}, name string, args map[string]interface{}) tools.ExecutionResult {
	var result tools.ExecutionResult
	var err error
	if config := configs[strings.ToLower(name)]; len(config) > 0 {
		result, err = s.toolService.ExecuteWithConfig(ctx, name, config, args)
	} else {
		result, err = s.toolService.Execute(ctx, name, args)
	}
	if err != nil {
		failure := fmt.Sprintf("tool %s failed: %v", name, err)
		return tools.ExecutionResult{Result: failure, IsError: true, ContextUpdateText: failure}
	}
	return result
}

func orderedToolCalls(accumulated map[int]*openrouter.ToolCall) []openrouter.ToolCall {
	indexes := make([]int, 0, len(accumulated))
	for index := range accumulated {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	calls := make([]openrouter.ToolCall, 0, len(indexes))
	for _, index := range indexes {
		calls = append(calls, *accumulated[index])
	}
	return calls
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
		result = append(result, ai.DescribeModel(sdkModelFacts(m)))
	}
	return result, nil
}

func (s *Service) buildRequest(input ai.GenerateInput) openrouter.ChatCompletionRequest {
	messages := make([]openrouter.ChatCompletionMessage, 0, len(input.Messages)+1)
	explicit := cachesExplicitly(s.modelFor(input))

	clock := brazilClock(input.AsOf)
	history := input.Messages
	if input.VolatileTail > 0 && len(history) > 0 {
		history, clock = withClockInTail(history, clock), ""
	}
	if sys := strings.TrimSpace(input.SystemPrompt); sys != "" {
		messages = append(messages, systemMessage(sys, clock, explicit))
	}
	for _, m := range history {
		switch m.Role {
		case ai.RoleSystem:
			messages = append(messages, openrouter.SystemMessage(m.Content))
		case ai.RoleAssistant:
			if len(m.ToolCalls) > 0 {
				messages = append(messages, openrouter.ChatCompletionMessage{
					Role:             openrouter.ChatMessageRoleAssistant,
					Content:          openrouter.Content{Text: m.Content},
					ToolCalls:        toOpenRouterToolCalls(m.ToolCalls),
					ReasoningDetails: reasoningDetailsOf(m.Reasoning),
				})
			} else {
				messages = append(messages, openrouter.AssistantMessage(m.Content))
			}
		case ai.RoleUser:
			messages = append(messages, userMessage(m))
		case ai.RoleTool:
			messages = append(messages, openrouter.ToolMessage(m.ToolCallID, m.Content))
		}
	}

	temp := input.Temperature
	if temp <= 0 {
		temp = s.defaultTemp
	}

	if explicit {
		messages = withTailBreakpoints(messages, input.VolatileTail)
	}

	req := openrouter.ChatCompletionRequest{
		Model:       s.modelFor(input),
		Messages:    messages,
		Temperature: temp,
		Tools:       convertTools(input.Tools),
		SessionId:   input.SessionID,
	}

	if input.MaxTokens > 0 {
		req.MaxTokens = input.MaxTokens
	}
	req.Reasoning = reasoningField(input)
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
	req.Usage = &openrouter.IncludeUsage{Include: true}
	return req
}

func (s *Service) modelFor(input ai.GenerateInput) string {
	if model := strings.TrimSpace(input.Model); model != "" {
		return model
	}
	return s.defaultModel
}

func (s *Service) visibleInput(ctx context.Context, input ai.GenerateInput) ai.GenerateInput {
	if !carriesImages(input.Messages) || s.seesImages(ctx, s.modelFor(input)) {
		return input
	}
	messages := make([]ai.Message, len(input.Messages))
	for i, m := range input.Messages {
		m.Images = nil
		messages[i] = m
	}
	input.Messages = messages
	return input
}

func carriesImages(messages []ai.Message) bool {
	for _, m := range messages {
		if len(m.Images) > 0 {
			return true
		}
	}
	return false
}

func reasoningField(input ai.GenerateInput) *openrouter.ChatCompletionReasoning {
	if input.ReasoningEffort != "" {
		effort := string(input.ReasoningEffort)
		return &openrouter.ChatCompletionReasoning{Effort: &effort}
	}
	if input.ReasoningMaxTokens > 0 {
		budget := input.ReasoningMaxTokens
		return &openrouter.ChatCompletionReasoning{MaxTokens: &budget}
	}
	return nil
}

func (s *Service) checkReasoning(ctx context.Context, input ai.GenerateInput) error {
	if input.ReasoningEffort == "" {
		return nil
	}
	models, _ := s.catalogFetcher.FetchModelsWithPricing(ctx)
	return ai.CheckReasoning(models, s.modelFor(input), input.ReasoningEffort)
}

func (s *Service) seesImages(ctx context.Context, model string) bool {
	models, ok := s.catalogFetcher.FetchModelsWithPricing(ctx)
	if !ok {
		return false
	}
	m, found := ai.FindModel(models, model)
	return found && m.SeesImages()
}

func userMessage(m ai.Message) openrouter.ChatCompletionMessage {
	if len(m.Images) == 0 {
		return openrouter.UserMessage(m.Content)
	}
	parts := make([]openrouter.ChatMessagePart, 0, len(m.Images)+1)
	parts = append(parts, openrouter.ChatMessagePart{Type: openrouter.ChatMessagePartTypeText, Text: m.Content})
	for _, url := range m.Images {
		parts = append(parts, openrouter.ChatMessagePart{
			Type:     openrouter.ChatMessagePartTypeImageURL,
			ImageURL: &openrouter.ChatMessageImageURL{URL: url, Detail: openrouter.ImageURLDetailAuto},
		})
	}
	return openrouter.ChatCompletionMessage{Role: openrouter.ChatMessageRoleUser, Content: openrouter.Content{Multi: parts}}
}

type openrouterJSONSchema map[string]any

func (s openrouterJSONSchema) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]any(s))
}

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

func streamReasoningDelta(delta openrouter.ChatCompletionStreamChoiceDelta) string {
	if delta.Reasoning != nil && *delta.Reasoning != "" {
		return *delta.Reasoning
	}
	return delta.ReasoningContent
}

func streamUnfinished(finishReason string, usage *openrouter.Usage) bool {
	if finishReason == "error" {
		return true
	}
	return finishReason == "" && usage == nil
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
		result = append(result, openrouter.Tool{
			Type:     openrouter.ToolTypeFunction,
			Function: &openrouter.FunctionDefinition{Name: name, Description: def.Description, Parameters: def.JSONSchema()},
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

func costToMicros(cost float64) int64 {
	return aibilling.CostToMicros(cost)
}

func usageOf(usage *openrouter.Usage) *ai.Usage {
	return &ai.Usage{
		PromptTokens:       usage.PromptTokens,
		CompletionTokens:   usage.CompletionTokens,
		TotalTokens:        usage.TotalTokens,
		CachedTokens:       usage.PromptTokenDetails.CachedTokens,
		CacheWriteTokens:   usage.PromptTokenDetails.CacheWriteTokens,
		ProviderCostMicros: costToMicros(usage.Cost),
	}
}

func callUsageOf(usage *openrouter.Usage) ai.CallUsage {
	return ai.CallUsage{
		PromptTokens:       usage.PromptTokens,
		CompletionTokens:   usage.CompletionTokens,
		CachedTokens:       usage.PromptTokenDetails.CachedTokens,
		CacheWriteTokens:   usage.PromptTokenDetails.CacheWriteTokens,
		ReasoningTokens:    usage.CompletionTokenDetails.ReasoningTokens,
		ProviderCostMicros: costToMicros(usage.Cost),
	}
}

func (s *Service) publishBillingEvent(workspaceID, reference, model string, usage ai.CallUsage) {
	if usage.PromptTokens > 0 {
		log.Printf("[ai-cache] ref=%s model=%s in=%d cached=%d written=%d hit=%d%%", reference, model, usage.PromptTokens, usage.CachedTokens, usage.CacheWriteTokens, usage.CachedTokens*100/usage.PromptTokens)
	}
	aibilling.NewPublisher(s.billingPub).PublishUsage(reference, workspaceID, model, usage)
}
