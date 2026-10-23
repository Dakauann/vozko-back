package cache

import (
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type SlotState interface {
	SetNX(key, value string, ttl time.Duration) (bool, error)
	DelIfValue(key, value string) (bool, error)
}

type Slots struct {
	state  SlotState
	prefix string
	max    int
	ttl    time.Duration
}

func NewSlots(state SlotState, prefix string, max int, ttl time.Duration) *Slots {
	return &Slots{state: state, prefix: prefix, max: max, ttl: ttl}
}

func (s *Slots) Acquire(id string) (func(), bool, error) {
	token := uuid.NewString()
	for i := 0; i < s.max; i++ {
		key := s.prefix + id + ":" + strconv.Itoa(i)
		taken, err := s.state.SetNX(key, token, s.ttl)
		if err != nil {
			return nil, false, err
		}
		if taken {
			return func() {
				if _, err := s.state.DelIfValue(key, token); err != nil {
					log.Printf("[slots] %s was not released and expires in %s: %v", key, s.ttl, err)
				}
			}, true, nil
		}
	}
	return nil, false, nil
}
