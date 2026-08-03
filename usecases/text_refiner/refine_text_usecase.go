package text_refiner

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/ai"
	"vozko/domain/balance"
	"vozko/domain/text_refiner"
	"vozko/domain/workspace/workspace_pricing"
)

const (
	charsPerTokenEstimate = 3

	defaultMaxTokens = 2048

	hardMaxTokens = 8192

	maxOriginalTextChars = 60000
	maxInstructionChars  = 4000

	defaultModel = "openai/gpt-4o-mini"

	balanceSafetyFactorNum = 5
	balanceSafetyFactorDen = 4

	baseSystemPrompt = `You are a senior assistant specialised in editing AI ` +
		`agent prompts and customer-facing copy. You will receive a piece of ` +
		`TEXT and a USER INSTRUCTION describing the desired change. Apply the ` +
		`instruction faithfully and return ONLY the new version of the text.` +
		"\n\n" +
		`Universal output contract (NEVER violate):` + "\n" +
		`- Output ONLY the refined text. No explanations, no prefaces ` +
		`("Sure, here is..."), no suffixes, no markdown code fences, no ` +
		`quotation marks wrapping the answer, no meta commentary.` + "\n" +
		`- Always write in the SAME LANGUAGE as the original text. Do not ` +
		`switch languages unless the instruction explicitly requests another.` + "\n" +
		`- Preserve named entities, key facts and any template variables or ` +
		`placeholders ({{name}}, {first_name}, [PLACEHOLDER], $var, etc.).` + "\n" +
		`- Fix obvious capitalisation, spelling and punctuation issues even ` +
		`when not explicitly requested.` + "\n" +
		`- The user's instruction describes WHAT TO CHANGE. It is NOT itself ` +
		`content to insert into the text. Never paste the instruction verbatim ` +
		`into the output.` + "\n" +
		`- Fully apply the instruction. Add, remove or restructure content as ` +
		`needed. Do not return the original unchanged unless it is already ` +
		`perfect for the instruction.`

	messagingKindGuidance = "\n\n" +
		`CONTEXT: The TEXT is the SYSTEM PROMPT (instruction set) of an AI ` +
		`MESSAGING AGENT that replies in chat (WhatsApp, web). The user's ` +
		`instruction describes desired AGENT BEHAVIOUR; rewrite the ` +
		`INSTRUCTIONS accordingly, you are NOT writing the message body that ` +
		`will be sent.` + "\n" +
		`Keep the prompt concise and direct (LLM-readable, not human copy). ` +
		`Address the agent in the second person ("You answer messages ..."). ` +
		`Prefer bullet rules over long prose. If the instruction asks for a ` +
		`tone change (shorter, friendlier, more formal), update the relevant ` +
		`tone/style rule, do not append it to the persona description.`

	initialMessageKindGuidance = "\n\n" +
		`CONTEXT: The TEXT is the LITERAL FIRST MESSAGE the agent sends to a ` +
		`real customer (user-facing copy). Rewrite the message itself per the ` +
		`instruction. Do NOT add instructions for an AI; the output is shown ` +
		`verbatim to a human. Keep it short, natural, and in the same tone ` +
		`expected from the agent.`

	genericKindGuidance = "\n\n" +
		`CONTEXT: The TEXT is generic copy. Rewrite the literal text per the ` +
		`user's instruction.`
)

type refineTextUseCase struct {
	aiService            ai.Service
	pricer               workspace_pricing.Pricer
	cachedBalanceChecker balance.CachedBalanceChecker
}

func NewRefineTextUseCase(
	aiService ai.Service,
	pricer workspace_pricing.Pricer,
	cachedBalanceChecker balance.CachedBalanceChecker,
) text_refiner.RefineTextUseCase {
	return &refineTextUseCase{
		aiService:            aiService,
		pricer:               pricer,
		cachedBalanceChecker: cachedBalanceChecker,
	}
}

func (u *refineTextUseCase) Execute(ctx context.Context, input text_refiner.RefineInput) (*text_refiner.RefineOutput, error) {
	if err := validate(&input); err != nil {
		return nil, err
	}

	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = defaultModel
	}

	maxTokens := input.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	if maxTokens > hardMaxTokens {
		maxTokens = hardMaxTokens
	}

	temperature := input.Temperature
	if temperature <= 0 {

		temperature = 0.5
	}

	userMessage := buildUserMessage(input.Kind.Normalize(), input.Instruction, input.OriginalText)
	systemPrompt := systemPromptForKind(input.Kind.Normalize())
	estimatedPromptTokens := estimateTokens(systemPrompt) + estimateTokens(userMessage)

	estimate, err := u.pricer.PriceLLM(input.WorkspaceID, model, estimatedPromptTokens, maxTokens)
	if err != nil {
		return nil, fmt.Errorf("text_refiner: estimate price: %w", err)
	}
	worstCase := estimate.PriceMicros
	if worstCase < 0 {
		worstCase = 0
	}

	required := (worstCase*balanceSafetyFactorNum + balanceSafetyFactorDen - 1) / balanceSafetyFactorDen
	ok, err := u.cachedBalanceChecker.HasSufficientBalance(input.WorkspaceID, required)
	if err != nil {
		return nil, fmt.Errorf("text_refiner: check balance: %w", err)
	}
	if !ok {
		return nil, text_refiner.ErrInsufficientBalance
	}

	out, err := u.aiService.Generate(ctx, ai.GenerateInput{
		Model:        model,
		SystemPrompt: systemPrompt,
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: userMessage},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		WorkspaceID: input.WorkspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("text_refiner: ai generate: %w", err)
	}
	if out == nil {
		return nil, text_refiner.ErrAIUnavailable
	}

	refined := strings.TrimSpace(out.Message.Content)
	refined = stripWrappingFences(refined)
	refined = stripTextMarkers(refined)
	if refined == "" {
		return nil, text_refiner.ErrEmptyResponse
	}

	segments := computeSegments(input.OriginalText, refined)
	unified := buildUnifiedDiff(input.OriginalText, refined)

	actualPriceMicros := int64(0)
	if out.Usage.PromptTokens > 0 || out.Usage.CompletionTokens > 0 {
		if actual, perr := u.pricer.PriceLLM(input.WorkspaceID, model, out.Usage.PromptTokens, out.Usage.CompletionTokens); perr == nil {
			actualPriceMicros = actual.PriceMicros
		}
	}

	return &text_refiner.RefineOutput{
		RequestID:            uuid.NewString(),
		Model:                model,
		OriginalText:         input.OriginalText,
		RefinedText:          refined,
		Segments:             segments,
		UnifiedDiff:          unified,
		PromptTokens:         out.Usage.PromptTokens,
		CompletionTokens:     out.Usage.CompletionTokens,
		EstimatedPriceMicros: estimate.PriceMicros,
		ActualPriceMicros:    actualPriceMicros,
		Usage:                out.Usage,
	}, nil
}

func validate(in *text_refiner.RefineInput) error {
	in.OriginalText = strings.TrimRight(in.OriginalText, "\r\n\t ")
	in.Instruction = strings.TrimSpace(in.Instruction)
	if in.OriginalText == "" {
		return text_refiner.ErrTextRequired
	}
	if in.Instruction == "" {
		return text_refiner.ErrInstructionRequired
	}
	if len(in.OriginalText) > maxOriginalTextChars {
		return text_refiner.ErrTextTooLong
	}
	if len(in.Instruction) > maxInstructionChars {
		return text_refiner.ErrInstructionTooLong
	}
	return nil
}

func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	n := len(s) / charsPerTokenEstimate
	if n < 1 {
		return 1
	}
	return n
}

func buildUserMessage(kind text_refiner.TextKind, instruction, text string) string {
	var b strings.Builder
	b.Grow(len(instruction) + len(text) + 256)

	switch kind {
	case text_refiner.TextKindMessagingPrompt:
		b.WriteString("USER REQUEST (describes how the AI agent should behave):\n")
		b.WriteString(instruction)
		b.WriteString("\n\nCURRENT AGENT SYSTEM PROMPT (you must rewrite THIS so the agent behaves as requested, do NOT inject the user request as content):\n")
	case text_refiner.TextKindInitialMessage:
		b.WriteString("USER INSTRUCTION (how to rewrite the greeting):\n")
		b.WriteString(instruction)
		b.WriteString("\n\nCURRENT GREETING (literal user-facing message; rewrite this exact greeting):\n")
	default:
		b.WriteString("USER INSTRUCTION (apply faithfully, rewriting as much as needed):\n")
		b.WriteString(instruction)
		b.WriteString("\n\nORIGINAL TEXT (rewrite the whole thing in the same language):\n")
	}

	b.WriteString("<<<TEXT\n")
	b.WriteString(text)
	b.WriteString("\nTEXT>>>")
	b.WriteString("\n\nReturn ONLY the new text, with no markers, no quotes, no explanations.")
	return b.String()
}

func systemPromptForKind(kind text_refiner.TextKind) string {
	switch kind {
	case text_refiner.TextKindMessagingPrompt:
		return baseSystemPrompt + messagingKindGuidance
	case text_refiner.TextKindInitialMessage:
		return baseSystemPrompt + initialMessageKindGuidance
	default:
		return baseSystemPrompt + genericKindGuidance
	}
}

func stripWrappingFences(s string) string {
	if !strings.HasPrefix(s, "```") || !strings.HasSuffix(s, "```") {
		return s
	}
	inner := strings.TrimPrefix(s, "```")

	if nl := strings.IndexByte(inner, '\n'); nl >= 0 {
		first := strings.TrimSpace(inner[:nl])
		if !strings.ContainsAny(first, " \t") && len(first) <= 24 {
			inner = inner[nl+1:]
		}
	}
	inner = strings.TrimSuffix(inner, "```")
	return strings.TrimSpace(inner)
}

func stripTextMarkers(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<<<TEXT")
	s = strings.TrimSuffix(s, "TEXT>>>")
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}
