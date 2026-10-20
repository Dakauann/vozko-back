package mercadolivre_test

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/webhook"
	"vozko/infra/mercadolivre/mlfake"
)

const (
	loadPacks            = 50
	loadMessagesPerPack  = 30
	loadQuestions        = 200
	loadPosters          = 32
	loadConsumerCeiling  = 40
	loadAcceptBudget     = 500 * time.Millisecond
	loadNicknameSlackPer = 2
)

func TestMercadoLivrePipelineUnderLoad(t *testing.T) {
	h := newHarness(t)
	account := h.connectNewSeller(mlfake.SellerSpec{ID: sellerID(50)})
	rule := h.call(http.MethodPost, "/mercadolivre/accounts/"+account.ID+"/comment-rules", h.workspaceID, map[string]any{
		"name": "Frete", "enabled": true, "match": "contains", "keywords": []string{"frete"},
		"actions": []string{"public_reply"}, "publicReplyText": "Oi {{username}}, o frete é grátis!",
	})
	if rule.status >= 300 {
		t.Fatalf("rule answered %d: %s", rule.status, rule.body)
	}

	var bodies [][]byte
	packs := make([]pack, 0, loadPacks)
	for i := 0; i < loadPacks; i++ {
		p := h.newPack(account.MLUserID, false)
		packs = append(packs, p)
		for m := 0; m < loadMessagesPerPack; m++ {
			id := h.fake.BuyerWrites(p.id, fmt.Sprintf("Mensagem %d do pacote %d", m, i))
			bodies = append(bodies, h.fake.Notification("messages", account.MLUserID, id, uuid.NewString()))
		}
	}
	matching := map[string]bool{}
	for i := 0; i < loadQuestions; i++ {
		text := "Tem em outra cor?"
		if i%2 == 0 {
			text = "Quanto fica o frete?"
		}
		qid := h.fake.Ask(account.MLUserID, "MLB4000"+fmt.Sprint(i%10), "70"+h.fake.NextID(), text)
		matching[qid] = i%2 == 0
		copies := 2
		if i < loadQuestions/2 {
			copies = 3
		}
		for c := 0; c < copies; c++ {
			bodies = append(bodies, h.fake.Notification("questions", account.MLUserID, "/questions/"+qid, uuid.NewString()))
		}
	}
	if len(bodies) != 2000 {
		t.Fatalf("built %d notifications, want 2000", len(bodies))
	}
	rand.Shuffle(len(bodies), func(i, j int) { bodies[i], bodies[j] = bodies[j], bodies[i] })
	h.fake.ResetCalls()

	latencies := make([]time.Duration, len(bodies))
	refused := 0
	var mu sync.Mutex
	work := make(chan int)
	var wg sync.WaitGroup
	started := time.Now()
	for w := 0; w < loadPosters; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				at := time.Now()
				code := h.notify(bodies[i])
				latencies[i] = time.Since(at)
				if code != http.StatusOK {
					mu.Lock()
					refused++
					mu.Unlock()
				}
			}
		}()
	}
	for i := range bodies {
		work <- i
	}
	close(work)
	wg.Wait()
	posted := time.Since(started)
	h.queue.waitIdle(t, 10*time.Minute)
	drained := time.Since(started)

	slices.Sort(latencies)
	handled := h.handled.snapshot()
	slices.Sort(handled)
	p99 := handled[len(handled)*99/100]
	t.Logf("2000 notifications: posted in %s, all processed in %s (%.0f notifications/s); handler time p50=%s p99=%s max=%s; seen by the poster p50=%s p99=%s max=%s; peak concurrent Mercado Livre calls %d",
		posted, drained, float64(len(bodies))/drained.Seconds(),
		handled[len(handled)/2], p99, handled[len(handled)-1],
		latencies[len(latencies)/2], latencies[len(latencies)*99/100], latencies[len(latencies)-1], h.fake.PeakInFlight())
	t.Logf("fake Mercado Livre calls: %v (total %d)", h.fake.Calls(), h.fake.TotalCalls())
	t.Logf("pipeline metrics: %v", h.metrics.events)
	t.Logf("retries scheduled %d, dead letters: message %d question %d", h.queue.delayed.Load(),
		h.queue.deadLetters(webhook.TopicMercadoLivreMessage), h.queue.deadLetters(webhook.TopicMercadoLivreQuestion))

	if refused != 0 {
		t.Fatalf("%d notifications were refused", refused)
	}
	if p99 > loadAcceptBudget {
		t.Errorf("p99 notification handler time %s is over the 500 ms Mercado Livre budget", p99)
	}
	if n := h.rows("mercadolivre_conversations", "account_id = ?", account.ID); n != loadPacks {
		t.Fatalf("conversations = %d, want %d", n, loadPacks)
	}
	if n := h.rows("conversation_messages", "entry_type = ? AND sender_kind = ?", "mercadolivre", "contact"); n != loadPacks*loadMessagesPerPack {
		t.Fatalf("inbound transcript rows = %d, want %d", n, loadPacks*loadMessagesPerPack)
	}
	var duplicated int64
	if err := h.db.Raw(`SELECT COUNT(*) FROM (SELECT entry_id, external_message_id FROM conversation_messages
		WHERE entry_type = 'mercadolivre' GROUP BY entry_id, external_message_id HAVING COUNT(*) > 1) d`).Scan(&duplicated).Error; err != nil {
		t.Fatal(err)
	}
	if duplicated != 0 {
		t.Fatalf("%d messages were stored twice", duplicated)
	}
	if n := h.rows("mercadolivre_questions", "account_id = ?", account.ID); n != loadQuestions {
		t.Fatalf("mirrored questions = %d", n)
	}
	for qid, ruled := range matching {
		want := 0
		if ruled {
			want = 1
		}
		if got := h.fake.AnswersPosted(qid); got != want {
			t.Fatalf("question %s answered %d times, want %d", qid, got, want)
		}
	}
	contacts := h.rows("mercadolivre_contacts", "account_id = ?", account.ID)
	if contacts != loadPacks+loadQuestions {
		t.Fatalf("contacts = %d, want %d", contacts, loadPacks+loadQuestions)
	}
	for _, p := range packs {
		if conv := h.conversationOf(account.ID, p.id); conv.PackSummary == nil || len(conv.PackSummary.Items) != 1 {
			t.Fatalf("pack %s has no summary", p.id)
		}
	}

	calls := h.fake.Calls()
	messageNotifications, questionNotifications, answers := loadPacks*loadMessagesPerPack, len(bodies)-loadPacks*loadMessagesPerPack, loadQuestions/2
	bounds := map[string]int{
		"GET /messages/{id}":                          messageNotifications,
		"GET /messages/packs/{pack}/sellers/{seller}": messageNotifications,
		"GET /packs/{id}":                             loadPacks,
		"GET /orders/{id}":                            loadPacks,
		"GET /shipments/{id}":                         loadPacks,
		"GET /items/bulk":                             loadPacks,
		"GET /questions/{id}":                         questionNotifications + answers,
		"POST /answers":                               answers,
		"GET /users/{id}":                             int(contacts) * loadNicknameSlackPer,
	}
	for route, limit := range bounds {
		if calls[route] > limit {
			t.Errorf("%s was called %d times, bound %d", route, calls[route], limit)
		}
	}
	expected := 0
	for _, limit := range bounds {
		expected += limit
	}
	expected += loadPacks
	if total := h.fake.TotalCalls(); total > expected {
		t.Errorf("%d Mercado Livre calls for 2000 notifications, bound %d", total, expected)
	}
	if peak := h.fake.PeakInFlight(); peak > loadConsumerCeiling {
		t.Errorf("peak concurrent Mercado Livre calls %d is over the consumer ceiling %d", peak, loadConsumerCeiling)
	}
	for route := range calls {
		if strings.HasPrefix(route, "POST /messages/packs") {
			t.Errorf("a pipeline without an agent sent a message: %v", calls)
		}
	}
}
