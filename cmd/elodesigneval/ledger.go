package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"vozko/domain/ai"
)

type referenceLedger struct {
	mu       sync.Mutex
	total    *usageLedger
	byRef    map[string]int64
	unbilled int
}

func newReferenceLedger(total *usageLedger) *referenceLedger {
	return &referenceLedger{total: total, byRef: map[string]int64{}}
}

func (l *referenceLedger) Publish(topic string, message []byte) error {
	var event ai.AICompletedEvent
	if err := json.Unmarshal(message, &event); err != nil {
		return err
	}
	if err := l.total.Publish(topic, message); err != nil {
		return err
	}
	l.mu.Lock()
	l.byRef[event.RequestID] += event.ProviderCostMicros
	l.mu.Unlock()
	return nil
}

func (l *referenceLedger) PublishWithDelay(topic string, message []byte, _ time.Duration) error {
	return l.Publish(topic, message)
}

func (l *referenceLedger) ValidateConnection() error { return nil }

func (l *referenceLedger) IncBillingSkipped(reason string) {
	l.mu.Lock()
	l.unbilled++
	l.mu.Unlock()
	fmt.Fprintf(os.Stderr, "warning: a provider call could not be billed (%s); the spending cap undercounts\n", reason)
}

func (l *referenceLedger) Unbilled() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.unbilled
}

func (l *referenceLedger) takeFor(reference string) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	var spent int64
	for id, micros := range l.byRef {
		if id == reference || strings.HasPrefix(id, reference+":") {
			spent += micros
			delete(l.byRef, id)
		}
	}
	return spent
}
