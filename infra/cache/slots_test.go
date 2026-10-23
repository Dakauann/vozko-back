package cache

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type slotStateFake struct {
	mu   sync.Mutex
	keys map[string]string
	err  error
}

func (f *slotStateFake) SetNX(key, value string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if _, ok := f.keys[key]; ok {
		return false, nil
	}
	f.keys[key] = value
	return true, nil
}

func (f *slotStateFake) DelIfValue(key, value string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.keys[key] != value {
		return false, nil
	}
	delete(f.keys, key)
	return true, nil
}

func TestSlotsBoundTheWorkInFlightPerID(t *testing.T) {
	state := &slotStateFake{keys: map[string]string{}}
	slots := NewSlots(state, "s:", 2, time.Second)
	first, ok1, _ := slots.Acquire("a")
	_, ok2, _ := slots.Acquire("a")
	_, ok3, _ := slots.Acquire("a")
	_, other, _ := slots.Acquire("b")
	if !ok1 || !ok2 || ok3 || !other {
		t.Fatalf("acquired %v %v %v %v", ok1, ok2, ok3, other)
	}
	first()
	if _, ok, _ := slots.Acquire("a"); !ok {
		t.Fatal("a released slot is free again")
	}
	state.err = errors.New("down")
	if _, ok, err := slots.Acquire("c"); ok || err == nil {
		t.Fatal("an unreadable store grants nothing")
	}
}
