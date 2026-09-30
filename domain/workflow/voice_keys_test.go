package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestAKeySeenEarlyIsHandedToTheNextWait(t *testing.T) {
	keys := make(chan rune, 1)
	q := NewKeyQueue(keys, make(chan struct{}))
	if q.Pressed() {
		t.Fatal("nothing was pressed yet")
	}
	keys <- '4'
	if !q.Pressed() || !q.Pressed() {
		t.Fatal("a pressed key must stay pressed until it is read")
	}
	if key, pressed, err := q.Next(time.Second); !pressed || key != '4' || err != nil {
		t.Fatalf("Next = %q %v %v", key, pressed, err)
	}
}

func TestWaitingForAKeyEndsOnSilenceOrHangUp(t *testing.T) {
	ended := make(chan struct{})
	q := NewKeyQueue(make(chan rune), ended)
	if _, pressed, err := q.Next(10 * time.Millisecond); pressed || err != nil {
		t.Fatalf("silence: %v %v", pressed, err)
	}
	close(ended)
	if !q.Ended() {
		t.Fatal("hang up not seen")
	}
	if _, _, err := q.Next(time.Second); !errors.Is(err, ErrCallEnded) {
		t.Fatalf("err = %v", err)
	}
}
