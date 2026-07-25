package conversation_usecase

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type dedupMessageRepo struct {
	conversation.MessageRepository

	mu      sync.Mutex
	created []*conversation.Message

	preCreateHook func()

	createCalls int32
	getCalls    int32
}

func (m *dedupMessageRepo) Create(msg *conversation.Message) error {
	atomic.AddInt32(&m.createCalls, 1)
	if m.preCreateHook != nil {
		m.preCreateHook()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.created = append(m.created, msg)
	return nil
}

func (m *dedupMessageRepo) GetByWhatsAppMessageID(wamid string) (*conversation.Message, error) {
	atomic.AddInt32(&m.getCalls, 1)
	if wamid == "" {
		return nil, conversation.ErrMessageNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.created {
		if msg.WhatsAppMessageID != nil && *msg.WhatsAppMessageID == wamid {
			return msg, nil
		}
	}
	return nil, conversation.ErrMessageNotFound
}

func newInboundRecord(wamid string) conversation.MessageHistoryRecord {
	return conversation.MessageHistoryRecord{
		EntryID:     "entry-1",
		EntryType:   shared.EntryTypeWhatsApp,
		Channel:     conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeUserMessage,
		MessageID:   wamid,
		From:        "5511952166820",
		To:          "5511999990000",
		Text:        "Trabalho sim",
		Timestamp:   time.Unix(1777462587, 0).UTC(),
	}
}

func TestMessageHistoryManager_Record_DeduplicatesByWhatsAppMessageID(t *testing.T) {
	repo := &dedupMessageRepo{}
	mgr := NewMessageHistoryManager(repo)

	wamid := "wamid.HBgNNTUxMTk1MjE2NjgyMBUCABIYIEFDRjQyREJDQjVFN0NDNjg2QkYxRDUzREQ1MkU5NDZDAA=="
	rec := newInboundRecord(wamid)

	for i := 0; i < 5; i++ {
		if err := mgr.Record(context.Background(), conversation.MessageDirectionInbound, rec); err != nil {
			t.Fatalf("unexpected error on Record call #%d: %v", i+1, err)
		}
	}

	if len(repo.created) != 1 {
		t.Fatalf("expected exactly 1 message persisted for repeated wamid, got %d", len(repo.created))
	}

	persisted := repo.created[0]
	if persisted.WhatsAppMessageID == nil || *persisted.WhatsAppMessageID != wamid {
		t.Fatalf("expected persisted message to carry wamid %q, got %+v", wamid, persisted.WhatsAppMessageID)
	}
}

func TestMessageHistoryManager_Record_FirstMessageDoesNotLogError(t *testing.T) {
	var buf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	})

	repo := &dedupMessageRepo{}
	mgr := NewMessageHistoryManager(repo)

	rec := newInboundRecord("wamid.first-seen")
	if err := mgr.Record(context.Background(), conversation.MessageDirectionInbound, rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.created) != 1 {
		t.Fatalf("expected message to be persisted on first sighting, got %d creates", len(repo.created))
	}
	if got := buf.String(); strings.Contains(got, "failed to check existing message") {
		t.Fatalf("dedup check produced false-positive error log on first message: %q", got)
	}
}

func TestMessageHistoryManager_Record_ConcurrentCalls_NoDuplicates(t *testing.T) {
	repo := &dedupMessageRepo{

		preCreateHook: func() { time.Sleep(5 * time.Millisecond) },
	}
	mgr := NewMessageHistoryManager(repo)

	rec := newInboundRecord("wamid.concurrent")

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start
			_ = mgr.Record(context.Background(), conversation.MessageDirectionInbound, rec)
		}()
	}
	close(start)
	wg.Wait()

	if len(repo.created) != 1 {
		t.Fatalf("expected exactly 1 message persisted under concurrent Record, got %d", len(repo.created))
	}
}

func TestMessageHistoryManager_Record_EmptyWamidStillPersists(t *testing.T) {
	repo := &dedupMessageRepo{}
	mgr := NewMessageHistoryManager(repo)

	rec := conversation.MessageHistoryRecord{
		EntryID:     "entry-1",
		EntryType:   shared.EntryTypeWhatsApp,
		Channel:     conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeToolCall,
		MessageID:   "",
		From:        "system",
		To:          "system",
		Text:        "[Tool Call] foo",
		Timestamp:   time.Now().UTC(),
	}

	for i := 0; i < 3; i++ {
		if err := mgr.Record(context.Background(), conversation.MessageDirectionOutbound, rec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if len(repo.created) != 3 {
		t.Fatalf("records without a wamid must not be deduplicated; got %d creates", len(repo.created))
	}
	if got := atomic.LoadInt32(&repo.getCalls); got != 0 {
		t.Fatalf("dedup lookup should be skipped when wamid is empty; got %d GetByWhatsAppMessageID calls", got)
	}
}
