package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

type fakeScripter struct {
	mu          sync.Mutex
	requests    []uw.ScriptRequest
	err         error
	reply       func(uw.ScriptRequest) *uw.ScriptResult
	sawDeadline bool
}

func (f *fakeScripter) Script(ctx context.Context, req uw.ScriptRequest) (*uw.ScriptResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if _, ok := ctx.Deadline(); ok {
		f.sawDeadline = true
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.reply != nil {
		return f.reply(req), nil
	}
	threads := make([]uw.ScriptedThread, 0, len(req.Subjects))
	for _, subject := range req.Subjects {
		threads = append(threads, uw.ScriptedThread{
			Ref: subject.Ref,
			Turns: []uw.ScriptTurn{
				{FromLead: true, Text: "oi! vi sim, queria saber sobre o valor"},
				{FromLead: false, Text: "Claro. Posso te passar as condicoes por aqui mesmo?"},
				{FromLead: true, Text: "pode sim, to olhando agora"},
			},
		})
	}
	return &uw.ScriptResult{Threads: threads, Model: "m", FinishReason: "stop"}, nil
}

func (f *fakeScripter) subjectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, req := range f.requests {
		n += len(req.Subjects)
	}
	return n
}

func (f *fakeScripter) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

type fakeBalance struct {
	micros int64
	err    error
}

func (f *fakeBalance) HasSufficientBalance(string, int64) (bool, error) { return true, nil }
func (f *fakeBalance) GetBalance(string) (int64, error)                 { return f.micros, f.err }
func (f *fakeBalance) Invalidate(string)                                {}
func (f *fakeBalance) InvalidateDebounced(string)                       {}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func newScriptedSeedUseCase(
	t *testing.T,
	writer *fakePlaceholderWriter,
	scripter uw.ConversationScripter,
	bal *fakeBalance,
) (*SeedInboxUseCase, *fakeConversationRepo) {
	t.Helper()
	instances := newFakeInstanceRepo(seedInstance("inst-1", uw.StatusConnected, time.Unix(100, 0)))
	contacts := newFakeContactRepo()
	conversations := newFakeConversationRepo()

	var checker = balanceCheckerOrNil(bal)
	uc := NewSeedInboxUseCase(
		instances, contacts, conversations, newFakeLeadLinker(), writer, scripter, checker)
	uc.clock = fixedClock{at: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)}
	return uc, conversations
}

func aScript() *uw.SeedScript {
	return &uw.SeedScript{
		Bodies:      []string{"Oi {{1}}, tudo bem? Vi que voce se interessou no curso."},
		MaxMessages: 4,
	}
}

func scriptedRequest(script *uw.SeedScript, numbers ...string) uw.SeedRequest {
	targets := make([]uw.SeedTarget, 0, len(numbers))
	for _, number := range numbers {
		targets = append(targets, uw.SeedTarget{Number: number, Name: "Marina"})
	}
	return uw.SeedRequest{WorkspaceID: "ws-1", Targets: targets, Script: script}
}

func TestSeedInboxWritesTheScriptedThread(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, conversations := newScriptedSeedUseCase(t, writer, &fakeScripter{}, &fakeBalance{micros: 5_000_000})

	out, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 1 || out.Scripted != 1 || out.ScriptFailed != 0 {
		t.Fatalf("outcome = %+v, want 1 seeded and 1 scripted", out)
	}

	writes := writer.writes()
	if len(writes) != 4 {
		t.Fatalf("wrote %d messages, want 4", len(writes))
	}

	conversationID := conversations.created[0].ID
	for i, msg := range writes {
		if msg.EntryID != conversationID {
			t.Errorf("message %d is on conversation %q, want %q", i, msg.EntryID, conversationID)
		}
		if msg.EntryType != shared.EntryTypeUnofficialWhatsApp {
			t.Errorf("message %d EntryType = %q", i, msg.EntryType)
		}
		if msg.Channel != conversation.MessageChannelUnofficialWhatsApp {
			t.Errorf("message %d Channel = %q", i, msg.Channel)
		}
		if strings.TrimSpace(msg.Text) == "" {
			t.Errorf("message %d is empty", i)
		}
	}

	if !strings.Contains(writes[0].Text, "Marina") {
		t.Errorf("the opening is %q, want the operator's message with the name", writes[0].Text)
	}
	if writes[0].ResolvedDirection() != conversation.MessageDirectionOutbound {
		t.Errorf("the opening is %q, want OUTBOUND", writes[0].ResolvedDirection())
	}
	wantDirections := []conversation.MessageHistoryDirection{
		conversation.MessageDirectionOutbound,
		conversation.MessageDirectionInbound,
		conversation.MessageDirectionOutbound,
		conversation.MessageDirectionInbound,
	}
	for i, want := range wantDirections {
		if writes[i].ResolvedDirection() != want {
			t.Errorf("message %d direction = %q, want %q", i, writes[i].ResolvedDirection(), want)
		}
	}
	if !writes[1].MessageType.IsInbound() {
		t.Errorf("the lead's message type %q is not inbound", writes[1].MessageType)
	}
	if writes[0].MessageType.IsInbound() {
		t.Errorf("our message type %q counts as inbound", writes[0].MessageType)
	}

	for i := 1; i < len(writes); i++ {
		if !writes[i].CreatedAt.After(writes[i-1].CreatedAt) {
			t.Errorf("message %d is not after message %d", i, i-1)
		}
	}
	newest := writes[len(writes)-1].CreatedAt
	if newest.After(uc.clock.Now()) {
		t.Errorf("the newest message is at %v, in the future of %v", newest, uc.clock.Now())
	}
	if uc.clock.Now().Sub(newest) > time.Hour {
		t.Errorf("the newest message is %v old; a seeded thread has to read as recent",
			uc.clock.Now().Sub(newest))
	}

	var meta map[string]any
	if err := json.Unmarshal(writes[0].Metadata, &meta); err != nil {
		t.Fatalf("metadata is not JSON: %v (%s)", err, writes[0].Metadata)
	}
	if meta[conversation.SeedMetadataKey] != conversation.SeedSourceLeadImport {
		t.Errorf("metadata seed = %v, want %q", meta[conversation.SeedMetadataKey], conversation.SeedSourceLeadImport)
	}
	if _, ok := meta["seededAt"]; !ok {
		t.Error("metadata carries no seededAt")
	}
}

func TestSeedInboxLeavesOnlyTheTrailingInboundMessageUnread(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _ := newScriptedSeedUseCase(t, writer, &fakeScripter{}, &fakeBalance{micros: 5_000_000})

	if _, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	writes := writer.writes()
	last := writes[len(writes)-1]
	if last.Read {
		t.Error("the trailing inbound message is marked read; the conversation reads as answered")
	}
	if last.ReadAt != nil {
		t.Error("the trailing unread message carries a ReadAt")
	}
	for i, msg := range writes[:len(writes)-1] {
		if !msg.Read {
			t.Errorf("message %d is unread; only the trailing inbound one may be", i)
		}
	}
}

func TestSeedInboxMarksEverythingReadWhenTheThreadEndsOnOurTurn(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{reply: func(req uw.ScriptRequest) *uw.ScriptResult {
		threads := make([]uw.ScriptedThread, 0, len(req.Subjects))
		for _, s := range req.Subjects {
			threads = append(threads, uw.ScriptedThread{Ref: s.Ref, Turns: []uw.ScriptTurn{
				{FromLead: true, Text: "oi"},
				{FromLead: false, Text: "Bom dia! Como posso ajudar?"},
			}})
		}
		return &uw.ScriptResult{Threads: threads}
	}}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	if _, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for i, msg := range writer.writes() {
		if !msg.Read {
			t.Errorf("message %d is unread in a thread that ends on our turn", i)
		}
	}
}

func TestSeedInboxNeverScriptsAConversationThatHasHistory(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{}
	uc, conversations := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	live := &uw.Conversation{
		ID: "conv-live", WorkspaceID: "ws-1", InstanceID: "inst-1",
		ContactID: "contact-5511888888888@s.whatsapp.net",
		ChatID:    "5511888888888@s.whatsapp.net",
	}
	conversations.convs[live.ID] = live
	writer.counts[live.ID] = 12

	out, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511888888888", "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.AlreadyActive != 1 || out.Seeded != 1 {
		t.Fatalf("outcome = %+v, want 1 already active and 1 seeded", out)
	}
	if got := scripter.subjectCount(); got != 1 {
		t.Fatalf("the model was asked about %d subjects, want only the 1 empty conversation", got)
	}
	for _, msg := range writer.writes() {
		if msg.EntryID == live.ID {
			t.Fatal("a message was written into a live conversation")
		}
	}
}

func TestSeedInboxFallsBackToThePlaceholderWhenTheModelFails(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{err: errors.New("upstream 503")}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	out, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 1 || out.Scripted != 0 || out.ScriptFailed != 1 {
		t.Fatalf("outcome = %+v, want 1 seeded, 0 scripted, 1 script failed", out)
	}
	writes := writer.writes()
	if len(writes) != 1 {
		t.Fatalf("wrote %d messages, want the 1 placeholder", len(writes))
	}
	if writes[0].MessageType != conversation.MessageTypeSystem || writes[0].Text != "" || !writes[0].Read {
		t.Fatalf("the fallback is %+v, want today's placeholder", writes[0])
	}
}

func TestSeedInboxFallsBackWhenTheModelReturnsNoUsableTurns(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{reply: func(req uw.ScriptRequest) *uw.ScriptResult {
		threads := make([]uw.ScriptedThread, 0, len(req.Subjects))
		for _, s := range req.Subjects {
			threads = append(threads, uw.ScriptedThread{Ref: s.Ref, Turns: []uw.ScriptTurn{
				{FromLead: false, Text: "nossa mensagem de novo"},
			}})
		}
		return &uw.ScriptResult{Threads: threads}
	}}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	out, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 1 || out.Scripted != 0 || out.ScriptFailed != 1 {
		t.Fatalf("outcome = %+v, want the placeholder fallback", out)
	}
	if len(writer.writes()) != 1 {
		t.Fatalf("wrote %d messages, want the 1 placeholder", len(writer.writes()))
	}
}

func TestSeedInboxIgnoresUnknownRefs(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{reply: func(uw.ScriptRequest) *uw.ScriptResult {
		return &uw.ScriptResult{Threads: []uw.ScriptedThread{
			{Ref: 9999, Turns: []uw.ScriptTurn{{FromLead: true, Text: "oi"}}},
		}}
	}}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	out, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 1 || out.Scripted != 0 || out.ScriptFailed != 1 {
		t.Fatalf("outcome = %+v, want the placeholder fallback", out)
	}
}

func TestSeedInboxRefusesToScriptBelowTheBalanceFloor(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 9_999})

	out, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if scripter.calls() != 0 {
		t.Fatalf("the model was called %d times below the balance floor", scripter.calls())
	}
	if out.Seeded != 1 || out.ScriptFailed != 1 {
		t.Fatalf("outcome = %+v, want 1 seeded plain and 1 script failed", out)
	}
	if len(writer.writes()) != 1 {
		t.Fatalf("wrote %d messages, want the 1 placeholder", len(writer.writes()))
	}
}

func TestSeedInboxRefusesToScriptWhenTheBalanceCannotBeRead(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter,
		&fakeBalance{micros: 5_000_000, err: errors.New("connection reset")})

	if _, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if scripter.calls() != 0 {
		t.Fatal("the model was called on an unreadable balance")
	}
}

func TestSeedInboxChecksTheBalanceOncePerBatch(t *testing.T) {
	writer := newFakePlaceholderWriter()
	counting := &countingBalance{fakeBalance: fakeBalance{micros: 5_000_000}}
	instances := newFakeInstanceRepo(seedInstance("inst-1", uw.StatusConnected, time.Unix(100, 0)))
	uc := NewSeedInboxUseCase(instances, newFakeContactRepo(), newFakeConversationRepo(),
		newFakeLeadLinker(), writer, &fakeScripter{}, counting)

	if _, err := uc.Execute(context.Background(),
		scriptedRequest(aScript(), "5511900000001", "5511900000002", "5511900000003")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if counting.reads != 1 {
		t.Fatalf("the balance was read %d times for one batch, want 1", counting.reads)
	}
}

func TestSeedInboxSkipsScriptingARowWithNoNameWhenTheBodyUsesIt(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	out, err := uc.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: "ws-1",
		Script:      aScript(),
		Targets: []uw.SeedTarget{
			{Number: "5511900000001", Name: "   "},
			{Number: "5511900000002", Name: "Marina"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.ScriptSkippedNoName != 1 {
		t.Errorf("ScriptSkippedNoName = %d, want 1", out.ScriptSkippedNoName)
	}
	if out.Scripted != 1 {
		t.Errorf("Scripted = %d, want 1", out.Scripted)
	}
	if out.Seeded != 2 {
		t.Errorf("Seeded = %d, want 2; nothing is dropped", out.Seeded)
	}
	if got := scripter.subjectCount(); got != 1 {
		t.Fatalf("the model was asked about %d subjects, want 1", got)
	}
}

func TestSeedInboxScriptsAnUnnamedRowWhenTheBodyDoesNotUseTheName(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	out, err := uc.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: "ws-1",
		Script:      &uw.SeedScript{Bodies: []string{"Oi, tudo bem? Vi seu interesse."}, MaxMessages: 4},
		Targets:     []uw.SeedTarget{{Number: "5511900000001"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.ScriptSkippedNoName != 0 || out.Scripted != 1 {
		t.Fatalf("outcome = %+v, want it scripted", out)
	}
}

func TestSeedInboxWritesOnlyWhatAcceptTurnsAllows(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{reply: func(req uw.ScriptRequest) *uw.ScriptResult {
		threads := make([]uw.ScriptedThread, 0, len(req.Subjects))
		for _, s := range req.Subjects {
			threads = append(threads, uw.ScriptedThread{Ref: s.Ref, Turns: []uw.ScriptTurn{
				{FromLead: true, Text: "1"},
				{FromLead: true, Text: "quebra a alternancia"},
				{FromLead: false, Text: "  "},
				{FromLead: false, Text: "2"},
				{FromLead: true, Text: "3"},
				{FromLead: false, Text: "4"},
				{FromLead: true, Text: "5"},
				{FromLead: false, Text: "6"},
			}})
		}
		return &uw.ScriptResult{Threads: threads}
	}}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	if _, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	writes := writer.writes()
	if len(writes) != 4 {
		t.Fatalf("wrote %d messages for a cap of 4", len(writes))
	}
	texts := []string{writes[1].Text, writes[2].Text, writes[3].Text}
	want := []string{"1", "2", "3"}
	for i := range want {
		if texts[i] != want[i] {
			t.Fatalf("turns written = %v, want %v", texts, want)
		}
	}
}

func TestSeedInboxChunksSubjectsAndSurvivesOneFailedChunk(t *testing.T) {
	writer := newFakePlaceholderWriter()
	var chunk int
	scripter := &fakeScripter{reply: func(req uw.ScriptRequest) *uw.ScriptResult {
		chunk++
		if chunk == 1 {
			return &uw.ScriptResult{}
		}
		threads := make([]uw.ScriptedThread, 0, len(req.Subjects))
		for _, s := range req.Subjects {
			threads = append(threads, uw.ScriptedThread{Ref: s.Ref, Turns: []uw.ScriptTurn{
				{FromLead: true, Text: "oi"},
			}})
		}
		return &uw.ScriptResult{Threads: threads}
	}}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	numbers := distinctNumbers(uw.ScriptSubjectsPerCall * 2)

	out, err := uc.Execute(context.Background(), scriptedRequest(aScript(), numbers...))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if scripter.calls() != 2 {
		t.Fatalf("made %d calls for %d subjects, want 2 chunks of %d",
			scripter.calls(), len(numbers), uw.ScriptSubjectsPerCall)
	}
	if out.Scripted != uw.ScriptSubjectsPerCall {
		t.Errorf("Scripted = %d, want the %d from the surviving chunk", out.Scripted, uw.ScriptSubjectsPerCall)
	}
	if out.ScriptFailed != uw.ScriptSubjectsPerCall {
		t.Errorf("ScriptFailed = %d, want the %d from the failed chunk", out.ScriptFailed, uw.ScriptSubjectsPerCall)
	}
	if out.Seeded != len(numbers) {
		t.Errorf("Seeded = %d, want all %d; a failed chunk still gets chats", out.Seeded, len(numbers))
	}
}

func TestSeedInboxSendsTheRenderedOpeningToTheModel(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	script := aScript()
	if _, err := uc.Execute(context.Background(), scriptedRequest(script, "5511999999999")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	scripter.mu.Lock()
	defer scripter.mu.Unlock()
	req := scripter.requests[0]
	if req.WorkspaceID != "ws-1" {
		t.Errorf("the model call carries workspace %q; without it nobody pays", req.WorkspaceID)
	}
	if req.MaxMessages != 4 {
		t.Errorf("MaxMessages = %d, want the operator's 4", req.MaxMessages)
	}
	if len(req.Subjects) != 1 {
		t.Fatalf("subjects = %d", len(req.Subjects))
	}
	if !strings.Contains(req.Subjects[0].FirstMessage, "Marina") {
		t.Errorf("FirstMessage = %q, want the rendered opening", req.Subjects[0].FirstMessage)
	}
	if strings.Contains(req.Subjects[0].FirstMessage, "{{") {
		t.Errorf("FirstMessage = %q, still carries a live placeholder", req.Subjects[0].FirstMessage)
	}
}

func TestSeedInboxScriptingIsIdempotent(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	req := scriptedRequest(aScript(), "5511999999999")
	if _, err := uc.Execute(context.Background(), req); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	before := len(writer.writes())

	out, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if out.AlreadyActive != 1 || out.Seeded != 0 {
		t.Fatalf("second run outcome = %+v, want 1 already active", out)
	}
	if scripter.calls() != 1 {
		t.Fatalf("the model was called %d times across two runs, want 1", scripter.calls())
	}
	if len(writer.writes()) != before {
		t.Fatalf("the second run wrote %d more messages", len(writer.writes())-before)
	}
}

func TestSeedInboxWithoutAScripterBehavesExactlyAsBefore(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _ := newScriptedSeedUseCase(t, writer, nil, &fakeBalance{micros: 5_000_000})

	out, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 1 || out.Scripted != 0 {
		t.Fatalf("outcome = %+v, want 1 plain seed", out)
	}
	writes := writer.writes()
	if len(writes) != 1 || writes[0].MessageType != conversation.MessageTypeSystem || writes[0].Text != "" {
		t.Fatalf("wrote %+v, want today's placeholder", writes)
	}
}

func TestSeedInboxWithoutAScriptNeverCallsTheModel(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	out, err := uc.Execute(context.Background(), scriptedRequest(nil, "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if scripter.calls() != 0 {
		t.Fatal("the model was called for an import that asked for no script")
	}
	if out.Seeded != 1 || out.Scripted != 0 || out.ScriptFailed != 0 {
		t.Fatalf("outcome = %+v, want a plain seed and no script counters", out)
	}
}

func TestSeedInboxBoundsTheModelCall(t *testing.T) {
	writer := newFakePlaceholderWriter()
	scripter := &fakeScripter{}
	uc, _ := newScriptedSeedUseCase(t, writer, scripter, &fakeBalance{micros: 5_000_000})

	if _, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	scripter.mu.Lock()
	defer scripter.mu.Unlock()
	if !scripter.sawDeadline {
		t.Fatal("the model was called on an unbounded context")
	}
}

func TestSeedInboxKeepsGoingWhenOneScriptedWriteFails(t *testing.T) {
	writer := newFakePlaceholderWriter()
	writer.failOnText = "Oi Marina, tudo bem? Vi que voce se interessou no curso."
	uc, _ := newScriptedSeedUseCase(t, writer, &fakeScripter{}, &fakeBalance{micros: 5_000_000})

	out, err := uc.Execute(context.Background(), scriptedRequest(aScript(), "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Failed != 1 || out.Seeded != 0 {
		t.Fatalf("outcome = %+v, want 1 failed", out)
	}
}

type countingBalance struct {
	fakeBalance
	reads int
}

func (c *countingBalance) GetBalance(id string) (int64, error) {
	c.reads++
	return c.fakeBalance.GetBalance(id)
}

func distinctNumbers(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("55119%08d", i))
	}
	return out
}

func balanceCheckerOrNil(b *fakeBalance) balance.CachedBalanceChecker {
	if b == nil {
		return nil
	}
	return b
}

func TestSeedInboxStaggersWhereThreadsEndButStaysDeterministic(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _ := newScriptedSeedUseCase(t, writer, &fakeScripter{}, &fakeBalance{micros: 5_000_000})

	numbers := distinctNumbers(8)
	if _, err := uc.Execute(context.Background(), scriptedRequest(aScript(), numbers...)); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	newest := map[string]time.Time{}
	for _, msg := range writer.writes() {
		if at, seen := newest[msg.EntryID]; !seen || msg.CreatedAt.After(at) {
			newest[msg.EntryID] = msg.CreatedAt
		}
	}
	if len(newest) != len(numbers) {
		t.Fatalf("wrote into %d conversations, want %d", len(newest), len(numbers))
	}
	distinct := map[time.Time]struct{}{}
	for _, at := range newest {
		distinct[at] = struct{}{}
	}
	if len(distinct) < 2 {
		t.Fatalf("%d conversations all end on the same instant", len(newest))
	}

	const number = "5511999999999"
	first := threadTimestamps(number, 4, time.Unix(1_700_000_000, 0).UTC())
	second := threadTimestamps(number, 4, time.Unix(1_700_000_000, 0).UTC())
	for i := range first {
		if !first[i].Equal(second[i]) {
			t.Fatalf("timestamp %d differs between runs: %v then %v", i, first[i], second[i])
		}
	}
	for i := 1; i < len(first); i++ {
		gap := first[i].Sub(first[i-1])
		if gap < 90*time.Second || gap > 240*time.Second {
			t.Errorf("gap %d is %v, want 90s..240s", i, gap)
		}
	}
}
