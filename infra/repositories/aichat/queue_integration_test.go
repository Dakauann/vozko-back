package aichat_repository

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/aichat"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func queueDB(t *testing.T) *gorm.DB {
	return repotest.IsolatedDB(t, "chat_queue_test", &schema.AIChatMessage{}, &schema.AIChatQueuedMessage{})
}

func enqueue(t *testing.T, repo aichat.QueueRepository, threadID, clientID, content string) *aichat.QueuedMessage {
	t.Helper()
	q := &aichat.QueuedMessage{ThreadID: threadID, ClientID: clientID, Content: content}
	if err := repo.Enqueue(q); err != nil {
		t.Fatalf("enqueue %s: %v", clientID, err)
	}
	return q
}

func TestEnqueueIsIdempotentPerClientID(t *testing.T) {
	repo := NewQueueRepository(queueDB(t))
	thread := uuid.New().String()
	first := enqueue(t, repo, thread, "c1", "primeira")
	again := enqueue(t, repo, thread, "c1", "repetida")
	if again.ID != first.ID || again.Content != "primeira" {
		t.Fatalf("a retried send must return the stored message, got %+v want id %s", again, first.ID)
	}
	other := enqueue(t, repo, uuid.New().String(), "c1", "outra conversa")
	if other.ID == first.ID {
		t.Fatal("the client id is scoped to its thread")
	}
	pending, err := repo.Pending(thread)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %+v err %v, want one message", pending, err)
	}
}

func TestDeliverMovesEveryPendingMessageIntoTheThreadInOrderOnce(t *testing.T) {
	db := queueDB(t)
	repo := NewQueueRepository(db)
	messages := NewMessageRepository(db)
	thread := uuid.New().String()
	enqueue(t, repo, thread, "c1", "um")
	enqueue(t, repo, thread, "c2", "dois")
	enqueue(t, repo, thread, "c3", "três")

	delivered, err := repo.Deliver(thread)
	if err != nil || len(delivered) != 3 {
		t.Fatalf("delivered %+v err %v", delivered, err)
	}
	for i, want := range []string{"c1", "c2", "c3"} {
		if delivered[i].ClientID != want || delivered[i].MessageID == "" || !delivered[i].Delivered() {
			t.Fatalf("delivery %d = %+v", i, delivered[i])
		}
	}
	again, err := repo.Deliver(thread)
	if err != nil || len(again) != 0 {
		t.Fatalf("a second drain must find nothing, got %+v err %v", again, err)
	}
	items, _, err := messages.ListByThread(aichat.ListMessagesInput{ThreadID: thread, Limit: 10})
	if err != nil || len(items) != 3 {
		t.Fatalf("thread messages %+v err %v", items, err)
	}
	for i, want := range []string{"um", "dois", "três"} {
		if items[i].Role != aichat.RoleUser || items[i].Content != want || items[i].ID != delivered[i].MessageID {
			t.Fatalf("message %d = %+v", i, items[i])
		}
	}
	retried := enqueue(t, repo, thread, "c2", "dois de novo")
	if !retried.Delivered() || retried.MessageID != delivered[1].MessageID {
		t.Fatalf("a retry after delivery must not queue a copy, got %+v", retried)
	}
	if pending, _ := repo.Pending(thread); len(pending) != 0 {
		t.Fatalf("nothing should be pending, got %+v", pending)
	}
}

func TestConcurrentDrainsDeliverEachMessageExactlyOnce(t *testing.T) {
	db := queueDB(t)
	repo := NewQueueRepository(db)
	thread := uuid.New().String()
	for _, id := range []string{"a", "b", "c", "d"} {
		enqueue(t, repo, thread, id, id)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := repo.Deliver(thread)
			if err != nil {
				t.Errorf("deliver: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, q := range out {
				seen[q.ClientID]++
			}
		}()
	}
	wg.Wait()
	if len(seen) != 4 {
		t.Fatalf("delivered %v, want all four", seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("%s delivered %d times", id, n)
		}
	}
	var count int64
	db.Model(&schema.AIChatMessage{}).Where("thread_id = ?", thread).Count(&count)
	if count != 4 {
		t.Fatalf("thread has %d messages, want 4", count)
	}
}

func TestRemoveOnlyTakesBackAMessageThatWasNotDelivered(t *testing.T) {
	repo := NewQueueRepository(queueDB(t))
	thread := uuid.New().String()
	enqueue(t, repo, thread, "c1", "fica")
	enqueue(t, repo, thread, "c2", "sai")
	if err := repo.Remove(thread, "c2"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := repo.Remove(thread, "c2"); !errors.Is(err, aichat.ErrQueuedMessageGone) {
		t.Fatalf("a second remove must say it is gone, got %v", err)
	}
	if err := repo.Remove(uuid.New().String(), "c1"); !errors.Is(err, aichat.ErrQueuedMessageGone) {
		t.Fatalf("another thread removed it: %v", err)
	}
	delivered, err := repo.Deliver(thread)
	if err != nil || len(delivered) != 1 || delivered[0].ClientID != "c1" {
		t.Fatalf("delivered %+v err %v", delivered, err)
	}
	if err := repo.Remove(thread, "c1"); !errors.Is(err, aichat.ErrQueuedMessageGone) {
		t.Fatalf("a delivered message cannot be removed, got %v", err)
	}
}

func TestPendingProposalIsSeenUntilClaimed(t *testing.T) {
	repo := NewMessageRepository(integrationDB(t))
	thread := uuid.New().String()
	if pending, err := repo.HasPendingProposal(thread); err != nil || pending {
		t.Fatalf("empty thread: %v %v", pending, err)
	}
	proposed(t, repo, thread, "act-1")
	if pending, err := repo.HasPendingProposal(thread); err != nil || !pending {
		t.Fatalf("proposal not seen: %v %v", pending, err)
	}
	if _, err := repo.ClaimProposal(thread, "act-1", aichat.ProposalRejected); err != nil {
		t.Fatal(err)
	}
	if pending, err := repo.HasPendingProposal(thread); err != nil || pending {
		t.Fatalf("claimed proposal still pending: %v %v", pending, err)
	}
}
