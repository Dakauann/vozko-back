package conversation_usecase

import (
	"fmt"
	"testing"
	"time"

	"vozko/domain/leadexit"
	"vozko/domain/messaging"
)

func TestAnExitRunInProgressIsRetriedWithoutSpendingTheRetryBudget(t *testing.T) {
	sub := &mockWhatsAppQueueSub{}
	pub := &mockWhatsAppQueuePub{}
	handler := &mockWhatsAppHandler{execErr: fmt.Errorf("lead exit of wamid.busy: %w", leadexit.ErrRunInProgress)}
	state := newMockWhatsAppSharedState()
	state.counters["wa:retry:msg:wamid.busy"] = int64(messaging.MaxRetries) - 1

	uc := NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(sub, pub, handler, state)
	_ = uc.Start()

	ack := &mockMessageAck{}
	sub.handler(makeTextMessagePayload("wamid.busy", "5511999999999", "sair"), ack)
	time.Sleep(100 * time.Millisecond)

	if !ack.wasAcked() || ack.wasNacked() {
		t.Fatalf("acked %v, nacked %v; want the delivery acked after a delayed republish", ack.wasAcked(), ack.wasNacked())
	}
	if pub.callCount() != 1 {
		t.Fatalf("an exit still running must be tried again, republishes = %d", pub.callCount())
	}
	state.mu.Lock()
	attempts := state.counters["wa:retry:msg:wamid.busy"]
	state.mu.Unlock()
	if attempts != int64(messaging.MaxRetries)-1 {
		t.Fatalf("the retry budget moved to %d", attempts)
	}
}
