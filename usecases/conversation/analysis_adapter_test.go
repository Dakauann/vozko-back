package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type recordingIngestor struct {
	inputs []ca.IngestInput
	err    error
}

func (r *recordingIngestor) Enqueue(_ context.Context, in ca.IngestInput) error {
	if r.err != nil {
		return r.err
	}
	r.inputs = append(r.inputs, in)
	return nil
}

// stubMessages answers ListByEntry from a fixed history.
type stubMessages struct {
	conversation.MessageRepository
	history []*conversation.Message
	err     error
}

func (s *stubMessages) ListByEntry(string, shared.EntryType) ([]*conversation.Message, error) {
	return s.history, s.err
}

func msgs(n int, base time.Time) []*conversation.Message {
	out := make([]*conversation.Message, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, &conversation.Message{
			Text:        "mensagem " + itoa(i),
			From:        "5511999",
			MessageType: conversation.MessageTypeUserMessage,
			CreatedAt:   base.Add(time.Duration(i) * time.Minute),
		})
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func adapterWith(t *testing.T, subject *AnalysisSubject, history []*conversation.Message) (*AnalysisAdapter, *recordingIngestor) {
	t.Helper()
	ing := &recordingIngestor{}
	a := NewAnalysisAdapter(ing, &stubMessages{history: history})
	a.RegisterResolver(shared.EntryTypeWhatsApp, func(context.Context, string) (*AnalysisSubject, error) {
		return subject, nil
	})
	return a, ing
}

func enabledSubject() *AnalysisSubject {
	return &AnalysisSubject{
		EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp,
		WorkspaceID: "ws-1", ContainerID: "camp-1", ContainerName: "Campanha A",
		ContactLabel: "5511999", EnableAnalysis: true,
	}
}

func TestAdapterEnqueuesAConversation(t *testing.T) {
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	a, ing := adapterWith(t, enabledSubject(), msgs(4, base))

	if err := a.Enqueue(context.Background(), "entry-1", shared.EntryTypeWhatsApp); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(ing.inputs) != 1 {
		t.Fatalf("enqueued %d inputs, want 1", len(ing.inputs))
	}

	in := ing.inputs[0]
	if in.Container.Kind != ca.SubjectKindConversation {
		t.Errorf("kind = %q", in.Container.Kind)
	}
	if in.Container.Source != ca.SourceWhatsApp {
		t.Errorf("source = %q", in.Container.Source)
	}
	if in.Container.ContainerID != "camp-1" {
		t.Errorf("container = %q, want the campaign", in.Container.ContainerID)
	}
	if in.SubjectID != "entry-1" {
		t.Errorf("subject id = %q, want the entry id", in.SubjectID)
	}
	if in.WorkspaceID != "ws-1" {
		t.Errorf("workspace = %q", in.WorkspaceID)
	}
	// The ref must be one the engine will accept.
	if err := in.Container.Validate(); err != nil {
		t.Errorf("enqueued an invalid container ref: %v", err)
	}
	// Bucketed on the last message, not on now.
	if want := base.Add(3 * time.Minute); !in.OccurredAt.Equal(want) {
		t.Errorf("commentedAt = %v, want the last message at %v", in.OccurredAt, want)
	}
}

// Switching analysis off on the container means nothing is queued and nothing
// is billed. This is the cheapest guard in the system and the easiest to lose.
func TestAdapterRespectsTheAnalysisSwitch(t *testing.T) {
	subject := enabledSubject()
	subject.EnableAnalysis = false
	a, ing := adapterWith(t, subject, msgs(4, time.Now()))

	if err := a.Enqueue(context.Background(), "entry-1", shared.EntryTypeWhatsApp); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(ing.inputs) != 0 {
		t.Fatalf("queued %d conversations with analysis switched off", len(ing.inputs))
	}
}

// An empty conversation must not be queued: it would spend a model call to
// analyse nothing and store the result.
func TestAdapterSkipsEmptyConversations(t *testing.T) {
	a, ing := adapterWith(t, enabledSubject(), nil)
	if err := a.Enqueue(context.Background(), "entry-1", shared.EntryTypeWhatsApp); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(ing.inputs) != 0 {
		t.Fatal("queued an empty conversation")
	}
}

// A channel with no resolver registered is not analysed, and that is a quiet
// no-op rather than an error: it is how a channel is switched off.
func TestAdapterIgnoresUnregisteredChannels(t *testing.T) {
	a, ing := adapterWith(t, enabledSubject(), msgs(2, time.Now()))
	if err := a.Enqueue(context.Background(), "entry-1", shared.EntryTypeTelegram); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(ing.inputs) != 0 {
		t.Fatal("queued a conversation on an unregistered channel")
	}
}

// The stored message count is the conversation's REAL length, while the text
// the model reads is windowed. Reporting the window as the length would
// understate every long conversation in the dashboard.
func TestAdapterCountsTheWholeConversationButWindowsTheText(t *testing.T) {
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	total := transcriptMessageLimit + 37
	a, _ := adapterWith(t, enabledSubject(), msgs(total, base))

	got, err := a.ReadTranscripts(context.Background(), ca.ContainerRef{
		Kind: ca.SubjectKindConversation, Source: ca.SourceWhatsApp,
		AccountID: "ws-1", ContainerID: "camp-1",
	}, []string{"entry-1"})
	if err != nil {
		t.Fatalf("ReadTranscripts: %v", err)
	}

	tr, ok := got["entry-1"]
	if !ok {
		t.Fatal("no transcript returned")
	}
	if tr.MessageCount != total {
		t.Errorf("message count = %d, want the full %d", tr.MessageCount, total)
	}
	// The oldest message is outside the window and must not be in the text.
	if strings.Contains(tr.Text, "mensagem 0\n") {
		t.Error("the transcript was not windowed to the most recent messages")
	}
	if !strings.Contains(tr.Text, "mensagem "+itoa(total-1)) {
		t.Error("the newest message is missing from the transcript")
	}
	if want := base.Add(time.Duration(total-1) * time.Minute); !tr.LastMessageAt.Equal(want) {
		t.Errorf("lastMessageAt = %v, want %v", tr.LastMessageAt, want)
	}
}

// One unreadable conversation must not fail the batch its peers are in.
func TestReadTranscriptsSkipsUnreadableConversations(t *testing.T) {
	ing := &recordingIngestor{}
	a := NewAnalysisAdapter(ing, &stubMessages{history: msgs(3, time.Now())})
	a.RegisterResolver(shared.EntryTypeWhatsApp, func(_ context.Context, entryID string) (*AnalysisSubject, error) {
		if entryID == "bad" {
			return nil, errors.New("boom")
		}
		return enabledSubject(), nil
	})

	got, err := a.ReadTranscripts(context.Background(), ca.ContainerRef{
		Kind: ca.SubjectKindConversation, Source: ca.SourceWhatsApp,
		AccountID: "ws-1", ContainerID: "camp-1",
	}, []string{"good", "bad"})
	if err != nil {
		t.Fatalf("one bad conversation failed the whole batch: %v", err)
	}
	if _, ok := got["good"]; !ok {
		t.Error("the readable conversation was dropped")
	}
	if _, ok := got["bad"]; ok {
		t.Error("the unreadable conversation should be absent, so the engine skips it")
	}
}

// The campaign objective reaches the prompt when a channel provides one, and
// its absence is not an error.
func TestReadContainerContextResolvesTheObjective(t *testing.T) {
	a, _ := adapterWith(t, enabledSubject(), msgs(2, time.Now()))
	ref := ca.ContainerRef{
		Kind: ca.SubjectKindConversation, Source: ca.SourceWhatsApp,
		AccountID: "ws-1", ContainerID: "camp-1",
	}

	ctx, err := a.ReadContainerContext(context.Background(), ref)
	if err != nil || ctx.Caption != "" {
		t.Fatalf("with no resolver: ctx=%+v err=%v", ctx, err)
	}

	a.RegisterObjective(shared.EntryTypeWhatsApp, func(_ context.Context, containerID string) (string, error) {
		if containerID != "camp-1" {
			t.Errorf("objective resolved for %q", containerID)
		}
		return "  Agendar avaliação  ", nil
	})
	ctx, err = a.ReadContainerContext(context.Background(), ref)
	if err != nil {
		t.Fatalf("ReadContainerContext: %v", err)
	}
	if ctx.Caption != "Agendar avaliação" {
		t.Errorf("objective = %q", ctx.Caption)
	}

	a.RegisterObjective(shared.EntryTypeWhatsApp, func(context.Context, string) (string, error) {
		return "", errors.New("boom")
	})
	if _, err := a.ReadContainerContext(context.Background(), ref); err != nil {
		t.Errorf("a failed objective lookup must not stop the analysis: %v", err)
	}
}

// The adapter satisfies the engine's port. Compile-time, because a signature
// drift here would otherwise only show up at container wiring.
var _ ca.ConversationAdapter = (*AnalysisAdapter)(nil)
