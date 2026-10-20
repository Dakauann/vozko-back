package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/channel"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
)

type textRulesAgents struct{ agent.Repository }

func (textRulesAgents) FindByID(id string) (*agent.Agent, error) {
	return &agent.Agent{ID: id, WorkspaceID: "ws-1", Name: "Bia", MessagingPrompt: "Você é a Bia.", MessagingModel: "gpt-x"}, nil
}

type scriptedAI struct {
	ai.Service
	replies []string
	errs    []error
	inputs  []ai.GenerateInput
}

func (s *scriptedAI) Generate(_ context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	s.inputs = append(s.inputs, in)
	i := len(s.inputs) - 1
	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	if i >= len(s.replies) {
		return &ai.GenerateOutput{}, nil
	}
	return &ai.GenerateOutput{Message: ai.Message{Role: ai.RoleAssistant, Content: s.replies[i]}}, nil
}

type recordingAgentSender struct{ sent []string }

func (r *recordingAgentSender) SendAgentTextMessage(_, _, text, _ string) (*conversation.Message, error) {
	r.sent = append(r.sent, text)
	return &conversation.Message{ID: "msg-1", Text: text}, nil
}

type recordingHandOff struct {
	calls []ia.RouletteHandOff
	err   error
}

func (r *recordingHandOff) HandOffToRoulette(in ia.RouletteHandOff) (string, error) {
	r.calls = append(r.calls, in)
	return "user-7", r.err
}

type recordingEvents struct{ events []*ce.ConversationEvent }

func (r *recordingEvents) Log(e *ce.ConversationEvent) { r.events = append(r.events, e) }

type describedWindowAdapter struct {
	fakeWindowAdapter
	caps channel.Capabilities
}

func (a *describedWindowAdapter) Capabilities() channel.Capabilities { return a.caps }

type textRulesHarness struct {
	svc     *ChannelAIReplyService
	ai      *scriptedAI
	sender  *recordingAgentSender
	handOff *recordingHandOff
	events  *recordingEvents
}

func newTextRulesHarness(t *testing.T, caps channel.Capabilities, replies ...string) *textRulesHarness {
	t.Helper()
	h := &textRulesHarness{
		ai:      &scriptedAI{replies: replies},
		sender:  &recordingAgentSender{},
		handOff: &recordingHandOff{},
		events:  &recordingEvents{},
	}
	adapters := conversation.NewAdapterRegistry(&describedWindowAdapter{
		fakeWindowAdapter: fakeWindowAdapter{entryType: shared.EntryTypeInstagram, open: true},
		caps:              caps,
	})
	h.svc = NewChannelAIReplyService(textRulesAgents{}, h.ai, stubMessageRepo{}, h.sender, adapters, h.handOff, h.events)
	return h
}

func textRulesRequest() conversation.AIReplyRequest {
	return conversation.AIReplyRequest{
		WorkspaceID:           "ws-1",
		EntryID:               "conv-1",
		EntryType:             shared.EntryTypeInstagram,
		AgentID:               "agent-1",
		AgentResponsesEnabled: true,
		Text:                  "qual o prazo de entrega?",
	}
}

func TestAgentReplyStatesTheChannelTextRulesInTheSystemPrompt(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 350}, "Entregamos em 3 dias.")

	if _, err := h.svc.Reply(context.Background(), textRulesRequest()); err != nil {
		t.Fatal(err)
	}

	prompt := h.ai.inputs[0].SystemPrompt
	if !strings.HasPrefix(prompt, "Você é a Bia.") || !strings.Contains(prompt, "350") {
		t.Fatalf("system prompt = %q, want the agent prompt followed by the channel's limit", prompt)
	}
}

func TestAgentReplyOnAChannelWithoutLimitsAddsNothing(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{}, "ok")

	if _, err := h.svc.Reply(context.Background(), textRulesRequest()); err != nil {
		t.Fatal(err)
	}
	if got := h.ai.inputs[0].SystemPrompt; got != "Você é a Bia." {
		t.Fatalf("system prompt = %q", got)
	}
}

func TestAgentReplyWithinTheLimitIsSentAsIs(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 20}, "Entregamos em 3 dias.")
	h.ai.replies = []string{"Em 3 dias."}

	msg, err := h.svc.Reply(context.Background(), textRulesRequest())
	if err != nil || msg == nil {
		t.Fatalf("msg = %v err = %v", msg, err)
	}
	if len(h.ai.inputs) != 1 || len(h.sender.sent) != 1 || h.sender.sent[0] != "Em 3 dias." {
		t.Fatalf("generations = %d sent = %v", len(h.ai.inputs), h.sender.sent)
	}
	if len(h.handOff.calls) != 0 || len(h.events.events) != 0 {
		t.Fatal("a valid reply hands nothing off")
	}
}

func TestAnOverLongReplyIsRegeneratedOnceWithTheViolationStated(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 10}, strings.Repeat("a", 25), "Em 3 dias.")

	msg, err := h.svc.Reply(context.Background(), textRulesRequest())
	if err != nil || msg == nil {
		t.Fatalf("msg = %v err = %v", msg, err)
	}
	if len(h.ai.inputs) != 2 {
		t.Fatalf("generations = %d, want exactly one regeneration", len(h.ai.inputs))
	}
	retry := h.ai.inputs[1]
	tail := retry.Messages[len(retry.Messages)-2:]
	if tail[0].Role != ai.RoleAssistant || tail[0].Content != strings.Repeat("a", 25) {
		t.Errorf("the regeneration must show the model its own draft, got %+v", tail[0])
	}
	if tail[1].Role != ai.RoleUser || !strings.Contains(tail[1].Content, "25 caracteres") || !strings.Contains(tail[1].Content, "10") {
		t.Errorf("the regeneration must state the violation, got %q", tail[1].Content)
	}
	if retry.ToolExecutionMode != ai.ToolExecutionModeNone || retry.ToolChoice != "none" {
		t.Errorf("a rewrite must not run tools again: mode %q choice %q", retry.ToolExecutionMode, retry.ToolChoice)
	}
	if len(h.sender.sent) != 1 || h.sender.sent[0] != "Em 3 dias." {
		t.Fatalf("sent = %v, want the rewritten reply", h.sender.sent)
	}
	if len(h.handOff.calls) != 0 {
		t.Fatal("a rewrite that fits is not handed off")
	}
}

func TestAReplyStillOverTheLimitIsWithheldAndHandedToAPerson(t *testing.T) {
	long := strings.Repeat("a", 25)
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 10}, long, long+"b")

	msg, err := h.svc.Reply(context.Background(), textRulesRequest())
	if err != nil || msg != nil {
		t.Fatalf("msg = %v err = %v, want nothing sent and no error", msg, err)
	}
	if len(h.ai.inputs) != 2 {
		t.Fatalf("generations = %d, want the first and one regeneration only", len(h.ai.inputs))
	}
	if len(h.sender.sent) != 0 {
		t.Fatalf("sent = %v, an invalid reply is never sent nor truncated", h.sender.sent)
	}
	if len(h.handOff.calls) != 1 {
		t.Fatalf("hand-offs = %d, want 1", len(h.handOff.calls))
	}
	call := h.handOff.calls[0]
	if call.WorkspaceID != "ws-1" || call.EntryID != "conv-1" || call.EntryType != "instagram" || call.ByActorID != "ai:agent-1" {
		t.Errorf("hand-off = %+v", call)
	}
	if len(h.events.events) != 1 {
		t.Fatalf("events = %d, want one note saying why", len(h.events.events))
	}
	event := h.events.events[0]
	details := event.DetailsMap()
	if event.EventType != ce.EventAgentReplyWithheld || event.WorkspaceID != "ws-1" || event.EntryID != "conv-1" || details["reason"] != "channel_text_rules" || details["violations"] != channel.TextViolationMaxRunes || details["agent_id"] != "agent-1" {
		t.Errorf("event = %+v details = %v", event, details)
	}
}

func TestAFailedRewriteIsWithheldAndHandedToAPerson(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 10}, strings.Repeat("a", 25))
	h.ai.errs = []error{nil, errors.New("provider down")}

	if msg, _ := h.svc.Reply(context.Background(), textRulesRequest()); msg != nil {
		t.Fatalf("msg = %v, want nothing sent", msg)
	}
	if len(h.sender.sent) != 0 || len(h.handOff.calls) != 1 || len(h.events.events) != 1 {
		t.Fatalf("sent = %v hand-offs = %d events = %d", h.sender.sent, len(h.handOff.calls), len(h.events.events))
	}
}

func TestAnEmptyRewriteIsWithheldAndHandedToAPerson(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 10}, strings.Repeat("a", 25), "   ")

	if msg, _ := h.svc.Reply(context.Background(), textRulesRequest()); msg != nil {
		t.Fatalf("msg = %v", msg)
	}
	if len(h.sender.sent) != 0 || len(h.handOff.calls) != 1 {
		t.Fatalf("sent = %v hand-offs = %d", h.sender.sent, len(h.handOff.calls))
	}
}

func TestAFailedHandOffIsReported(t *testing.T) {
	long := strings.Repeat("a", 25)
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 10}, long, long)
	h.handOff.err = errors.New("ring unavailable")

	msg, err := h.svc.Reply(context.Background(), textRulesRequest())
	if msg != nil || err == nil {
		t.Fatalf("msg = %v err = %v, want the hand-off failure surfaced", msg, err)
	}
	if len(h.sender.sent) != 0 {
		t.Fatal("nothing is sent when the hand-off fails")
	}
}

type latin1Rule struct{}

func (latin1Rule) Instruction() string { return "Use apenas caracteres latinos." }

func (latin1Rule) Check(body string) (channel.TextViolation, bool) {
	for _, r := range body {
		if r > 0xFF {
			return channel.TextViolation{Code: "charset", Message: "a mensagem tem caracteres fora do latim"}, true
		}
	}
	return channel.TextViolation{}, false
}

func TestAProviderTextRuleIsStatedAndEnforced(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 350, ProviderTextRules: []channel.TextRule{latin1Rule{}}}, "Olá 中", "Olá!")

	msg, err := h.svc.Reply(context.Background(), textRulesRequest())
	if err != nil || msg == nil {
		t.Fatalf("msg = %v err = %v", msg, err)
	}
	if !strings.Contains(h.ai.inputs[0].SystemPrompt, "Use apenas caracteres latinos.") {
		t.Errorf("the provider rule is missing from the prompt: %q", h.ai.inputs[0].SystemPrompt)
	}
	if !strings.Contains(h.ai.inputs[1].Messages[len(h.ai.inputs[1].Messages)-1].Content, "fora do latim") {
		t.Error("the regeneration must state the provider rule's violation")
	}
	if len(h.sender.sent) != 1 || h.sender.sent[0] != "Olá!" {
		t.Fatalf("sent = %v", h.sender.sent)
	}
}

func TestTheDisclosureCountsAgainstTheLimit(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 20}, strings.Repeat("a", 15), "Em 3.")
	req := textRulesRequest()
	req.Disclosure = "Sou um robô."

	if _, err := h.svc.Reply(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ai.inputs[0].SystemPrompt, "6 caracteres") {
		t.Errorf("the prompt must state the room left after the disclosure, got %q", h.ai.inputs[0].SystemPrompt)
	}
	if len(h.ai.inputs) != 2 {
		t.Fatalf("generations = %d, a reply that only fits without the disclosure is regenerated", len(h.ai.inputs))
	}
	if len(h.sender.sent) != 1 || h.sender.sent[0] != "Sou um robô.\n\nEm 3." {
		t.Fatalf("sent = %q", h.sender.sent)
	}
}

func TestADisclosureThatFillsTheLimitHandsOffWithoutGenerating(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 10}, "ok")
	req := textRulesRequest()
	req.Disclosure = "Sou um assistente automático."

	if msg, err := h.svc.Reply(context.Background(), req); msg != nil || err != nil {
		t.Fatalf("msg = %v err = %v", msg, err)
	}
	if len(h.ai.inputs) != 0 || len(h.sender.sent) != 0 || len(h.handOff.calls) != 1 || len(h.events.events) != 1 {
		t.Fatalf("generations = %d sent = %v hand-offs = %d events = %d", len(h.ai.inputs), h.sender.sent, len(h.handOff.calls), len(h.events.events))
	}
}

func TestUnknownChannelTextRulesRefuseBeforeGenerating(t *testing.T) {
	h := newTextRulesHarness(t, channel.Capabilities{MaxTextRunes: 10}, "ok")
	h.svc.adapters = conversation.NewAdapterRegistry(&fakeWindowAdapter{entryType: shared.EntryTypeInstagram, open: true})

	msg, err := h.svc.Reply(context.Background(), textRulesRequest())
	if msg != nil || !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Fatalf("msg = %v err = %v, want ErrCapabilityUnsupported", msg, err)
	}
	if len(h.ai.inputs) != 0 || len(h.sender.sent) != 0 {
		t.Fatal("nothing is generated or sent without the channel's text rules")
	}
}

func TestNewChannelAIReplyServiceRequiresItsDependencies(t *testing.T) {
	adapters := conversation.NewAdapterRegistry()
	build := map[string]func(){
		"no adapters": func() {
			NewChannelAIReplyService(textRulesAgents{}, &scriptedAI{}, stubMessageRepo{}, &recordingAgentSender{}, nil, &recordingHandOff{}, &recordingEvents{})
		},
		"no hand-off": func() {
			NewChannelAIReplyService(textRulesAgents{}, &scriptedAI{}, stubMessageRepo{}, &recordingAgentSender{}, adapters, nil, &recordingEvents{})
		},
		"no events": func() {
			NewChannelAIReplyService(textRulesAgents{}, &scriptedAI{}, stubMessageRepo{}, &recordingAgentSender{}, adapters, &recordingHandOff{}, nil)
		},
	}
	for name, fn := range build {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("a missing dependency must fail boot")
				}
			}()
			fn()
		})
	}
}
