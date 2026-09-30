package voipinfra

import "testing"

func TestAFullKeyBufferKeepsTheNewestPress(t *testing.T) {
	call := &trackedCall{keys: make(chan rune, 2)}
	for _, key := range "123" {
		call.pressKey(key)
	}
	got := []rune{<-call.keys, <-call.keys}
	if string(got) != "23" {
		t.Fatalf("buffered %q, want the two newest presses", string(got))
	}
}
